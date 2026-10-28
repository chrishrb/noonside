package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Minute data lives in measurement_days: one row per inverter and UTC day,
// samples as parallel arrays sorted by secs (seconds since 00:00 UTC).

// sampleDay and sampleSecs map a timestamptz column to its measurement_days
// row and array position.
const (
	sampleDay  = `(time AT TIME ZONE 'UTC')::date`
	sampleSecs = `(floor(extract(epoch FROM time))::bigint % 86400)::int`
)

// samplesFrom expands the measurement_days rows of the inverters $1 into samples
// (inverter_id, time, pac_w, pdc_w, udc_v, eday_wh, temperature_c) within
// [$2, $3).
const samplesFrom = `
	FROM measurement_days d
	CROSS JOIN LATERAL (
		SELECT d.day::timestamp AT TIME ZONE 'UTC' + u.s * interval '1 second' AS time,
			u.pac_w, u.pdc_w, u.udc_v, u.eday_wh, u.temperature_c
		FROM unnest(d.secs, d.pac_w, d.pdc_w, d.udc_v, d.eday_wh, d.temperature_c)
			AS u(s, pac_w, pdc_w, udc_v, eday_wh, temperature_c)
	) m
	WHERE d.inverter_id = ANY($1)
		AND d.day BETWEEN ($2::timestamptz AT TIME ZONE 'UTC')::date AND ($3::timestamptz AT TIME ZONE 'UTC')::date
		AND m.time >= $2 AND m.time < $3`

// UpsertMeasurements merges ms into the day rows; new samples replace
// existing ones with the same time.
func (m *Module) UpsertMeasurements(ctx context.Context, ms []Measurement) error {
	if len(ms) == 0 {
		return nil
	}
	return m.db.InTx(ctx, func(ctx context.Context) error {
		// Dropped at the end of the transaction: with the transaction pooler
		// the next transaction may run on another server connection.
		if _, err := m.db.Q(ctx).Exec(ctx, `CREATE TEMP TABLE tmp_samples (
			time timestamptz NOT NULL, inverter_id uuid NOT NULL, pac_w real, pdc_w real,
			udc_v real, eday_wh real, temperature_c real) ON COMMIT DROP`); err != nil {
			return err
		}
		_, err := m.db.Q(ctx).CopyFrom(ctx, pgx.Identifier{"tmp_samples"},
			[]string{"time", "inverter_id", "pac_w", "pdc_w", "udc_v", "eday_wh", "temperature_c"},
			pgx.CopyFromSlice(len(ms), func(i int) ([]any, error) {
				m := ms[i]
				return []any{m.Time, m.InverterID, m.PacW, m.PdcW, m.UdcV, m.EdayWh, m.TemperatureC}, nil
			}))
		if err != nil {
			return db.MapErr(err)
		}
		// Serialize writers per day row; the merge below reads the row and
		// would otherwise drop samples of a concurrent writer.
		if _, err := m.db.Q(ctx).Exec(ctx, `
			SELECT pg_advisory_xact_lock(hashtext(inverter_id::text), day - DATE '2000-01-01')
			FROM (SELECT DISTINCT inverter_id, `+sampleDay+` AS day FROM tmp_samples ORDER BY 1, 2) k`); err != nil {
			return err
		}
		_, err = m.db.Q(ctx).Exec(ctx, `
			WITH new AS (
				SELECT inverter_id, `+sampleDay+` AS day, `+sampleSecs+` AS s,
					pac_w, pdc_w, udc_v, eday_wh, temperature_c, 0 AS prio
				FROM tmp_samples
			), old AS (
				SELECT d.inverter_id, d.day, u.s, u.pac_w, u.pdc_w, u.udc_v, u.eday_wh, u.temperature_c, 1 AS prio
				FROM measurement_days d
				CROSS JOIN LATERAL unnest(d.secs, d.pac_w, d.pdc_w, d.udc_v, d.eday_wh, d.temperature_c)
					AS u(s, pac_w, pdc_w, udc_v, eday_wh, temperature_c)
				WHERE (d.inverter_id, d.day) IN (SELECT DISTINCT inverter_id, day FROM new)
			), merged AS (
				SELECT DISTINCT ON (inverter_id, day, s) * FROM (SELECT * FROM new UNION ALL SELECT * FROM old) x
				ORDER BY inverter_id, day, s, prio
			)
			INSERT INTO measurement_days (inverter_id, day, secs, pac_w, pdc_w, udc_v, eday_wh, temperature_c)
			SELECT inverter_id, day, array_agg(s ORDER BY s), array_agg(pac_w ORDER BY s),
				array_agg(pdc_w ORDER BY s), array_agg(udc_v ORDER BY s),
				array_agg(eday_wh ORDER BY s), array_agg(temperature_c ORDER BY s)
			FROM merged GROUP BY inverter_id, day
			ON CONFLICT (inverter_id, day) DO UPDATE SET
				secs = EXCLUDED.secs, pac_w = EXCLUDED.pac_w, pdc_w = EXCLUDED.pdc_w, udc_v = EXCLUDED.udc_v,
				eday_wh = EXCLUDED.eday_wh, temperature_c = EXCLUDED.temperature_c`)
		return db.MapErr(err)
	})
}

// DeleteMeasurementsBefore drops up to maxRows whole day rows before the
// cutoff's UTC day. Once none are left it trims the samples before the
// cutoff from the row of that day and reports done. Each call is one short
// transaction, so callers can delete large histories in batches.
func (m *Module) DeleteMeasurementsBefore(ctx context.Context, systemID uuid.UUID, before time.Time, maxRows int) (int64, bool, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return 0, false, err
	}
	const cut = `($2::timestamptz AT TIME ZONE 'UTC')::date`
	const cutSecs = `(floor(extract(epoch FROM $2::timestamptz))::bigint % 86400)::int`
	var rows, samples int64
	if err := m.db.Q(ctx).QueryRow(ctx, `
		WITH old AS (
			SELECT d.inverter_id, d.day FROM measurement_days d
			WHERE d.inverter_id = ANY($1)
				AND (d.day < `+cut+` OR (d.day = `+cut+` AND d.secs[cardinality(d.secs)] < `+cutSecs+`))
			ORDER BY d.day LIMIT $3
		), del AS (
			DELETE FROM measurement_days d USING old
			WHERE d.inverter_id = old.inverter_id AND d.day = old.day
			RETURNING cardinality(d.secs) AS n
		) SELECT count(*), coalesce(sum(n), 0) FROM del`, ids, before, maxRows).Scan(&rows, &samples); err != nil {
		return 0, false, err
	}
	if rows >= int64(maxRows) {
		return samples, false, nil
	}
	var trimmed int64
	if err := m.db.Q(ctx).QueryRow(ctx, `
		WITH kept AS (
			SELECT d.inverter_id, d.day, cardinality(d.secs) AS before_n,
				array_agg(u.s ORDER BY u.s) AS secs, array_agg(u.pac_w ORDER BY u.s) AS pac_w,
				array_agg(u.pdc_w ORDER BY u.s) AS pdc_w, array_agg(u.udc_v ORDER BY u.s) AS udc_v,
				array_agg(u.eday_wh ORDER BY u.s) AS eday_wh, array_agg(u.temperature_c ORDER BY u.s) AS temperature_c
			FROM measurement_days d
			CROSS JOIN LATERAL unnest(d.secs, d.pac_w, d.pdc_w, d.udc_v, d.eday_wh, d.temperature_c)
				AS u(s, pac_w, pdc_w, udc_v, eday_wh, temperature_c)
			WHERE d.inverter_id = ANY($1) AND d.day = `+cut+` AND d.secs[1] < `+cutSecs+` AND u.s >= `+cutSecs+`
			GROUP BY d.inverter_id, d.day, d.secs
		), upd AS (
			UPDATE measurement_days d SET secs = k.secs, pac_w = k.pac_w, pdc_w = k.pdc_w,
				udc_v = k.udc_v, eday_wh = k.eday_wh, temperature_c = k.temperature_c
			FROM kept k WHERE d.inverter_id = k.inverter_id AND d.day = k.day
			RETURNING k.before_n - cardinality(k.secs) AS n
		) SELECT coalesce(sum(n), 0) FROM upd`, ids, before).Scan(&trimmed); err != nil {
		return samples, false, err
	}
	return samples + trimmed, true, nil
}

func (m *Module) UpsertDailyEnergy(ctx context.Context, ds []DailyEnergy) error {
	if len(ds) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, d := range ds {
		b.Queue(`
			INSERT INTO inverter_daily (inverter_id, day, energy_wh, pac_max_w) VALUES ($1, $2, $3, $4)
			ON CONFLICT (inverter_id, day) DO UPDATE SET
				energy_wh = GREATEST(inverter_daily.energy_wh, EXCLUDED.energy_wh),
				pac_max_w = GREATEST(inverter_daily.pac_max_w, EXCLUDED.pac_max_w)`,
			d.InverterID, pgDate(d.Day), d.EnergyWh, d.PacMaxW)
	}
	return db.MapErr(m.db.Q(ctx).SendBatch(ctx, b).Close())
}

func (m *Module) LastMeasurementTime(ctx context.Context, systemID uuid.UUID) (*time.Time, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var t *time.Time
	err = m.db.Q(ctx).QueryRow(ctx, `
		SELECT max(d.day::timestamp AT TIME ZONE 'UTC' + d.secs[array_upper(d.secs, 1)] * interval '1 second')
		FROM measurement_days d
		WHERE d.inverter_id = ANY($1) AND d.day = (
			SELECT max(d2.day) FROM measurement_days d2 WHERE d2.inverter_id = ANY($1))`, ids).Scan(&t)
	return t, err
}

func (m *Module) FirstDataDay(ctx context.Context, systemID uuid.UUID) (*time.Time, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var t *time.Time
	err = m.db.Q(ctx).QueryRow(ctx, `
		SELECT min(d.day) FROM inverter_daily d WHERE d.inverter_id = ANY($1)`, ids).Scan(&t)
	return t, err
}

// UpsertInverterState keeps the newest power values. Status fields are
// replaced whenever the snapshot carries a status, since status snapshots
// (min_cur.js) may be older than the latest minute record.
func (m *Module) UpsertInverterState(ctx context.Context, st InverterState) error {
	hasStatus := st.StatusCode != nil || st.StatusText != "" || st.ErrorCode != nil || st.ErrorText != "" || st.PowerLimitPct != nil
	_, err := m.db.Q(ctx).Exec(ctx, `
		INSERT INTO inverter_state AS cur (inverter_id, time, pac_w, pdc_w, status_code, status_text, error_code, error_text, power_limit_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $10)
		ON CONFLICT (inverter_id) DO UPDATE SET
			time  = GREATEST(cur.time, EXCLUDED.time),
			pac_w = CASE WHEN EXCLUDED.time >= cur.time THEN EXCLUDED.pac_w ELSE cur.pac_w END,
			pdc_w = CASE WHEN EXCLUDED.time >= cur.time THEN EXCLUDED.pdc_w ELSE cur.pdc_w END,
			status_code = CASE WHEN $9 THEN EXCLUDED.status_code ELSE cur.status_code END,
			status_text = CASE WHEN $9 THEN EXCLUDED.status_text ELSE cur.status_text END,
			error_code  = CASE WHEN $9 THEN EXCLUDED.error_code  ELSE cur.error_code END,
			error_text  = CASE WHEN $9 THEN EXCLUDED.error_text  ELSE cur.error_text END,
			power_limit_pct = CASE WHEN $9 THEN EXCLUDED.power_limit_pct ELSE cur.power_limit_pct END`,
		st.InverterID, st.Time, st.PacW, st.PdcW, st.StatusCode, st.StatusText, st.ErrorCode, st.ErrorText, hasStatus, st.PowerLimitPct)
	return db.MapErr(err)
}

func (m *Module) ListInverterStates(ctx context.Context, systemID uuid.UUID) ([]InverterState, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT st.inverter_id, st.time, st.pac_w, st.pdc_w, st.status_code, st.status_text, st.error_code, st.error_text, st.power_limit_pct
		FROM inverter_state st WHERE st.inverter_id = ANY($1)`, ids)
	return db.Collect(rows, err, func(r pgx.Row) (InverterState, error) {
		var st InverterState
		return st, r.Scan(&st.InverterID, &st.Time, &st.PacW, &st.PdcW, &st.StatusCode, &st.StatusText, &st.ErrorCode, &st.ErrorText, &st.PowerLimitPct)
	})
}

const eventCols = `e.id, e.inverter_id, e.started_at, e.ended_at, e.status_code, e.status_text, e.error_code, e.error_text, e.power_limit_pct`

func scanEvent(r pgx.Row) (InverterEvent, error) {
	var e InverterEvent
	err := r.Scan(&e.ID, &e.InverterID, &e.StartedAt, &e.EndedAt, &e.StatusCode, &e.StatusText, &e.ErrorCode, &e.ErrorText, &e.PowerLimitPct)
	return e, db.MapErr(err)
}

func (m *Module) OpenEvent(ctx context.Context, inverterID uuid.UUID) (*InverterEvent, error) {
	e, err := scanEvent(m.db.Q(ctx).QueryRow(ctx, `SELECT `+eventCols+` FROM inverter_events e
		WHERE e.inverter_id = $1 AND e.ended_at IS NULL ORDER BY e.started_at DESC LIMIT 1`, inverterID))
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Module) CloseEvent(ctx context.Context, id int64, at time.Time) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE inverter_events SET ended_at = $2 WHERE id = $1`, id, at)
	return err
}

func (m *Module) InsertEvent(ctx context.Context, e InverterEvent) (int64, error) {
	var id int64
	err := m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO inverter_events (inverter_id, started_at, ended_at, status_code, status_text, error_code, error_text, power_limit_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (inverter_id, started_at) DO UPDATE SET ended_at = COALESCE(EXCLUDED.ended_at, inverter_events.ended_at)
		RETURNING id`,
		e.InverterID, e.StartedAt, e.EndedAt, e.StatusCode, e.StatusText, e.ErrorCode, e.ErrorText, e.PowerLimitPct).Scan(&id)
	return id, db.MapErr(err)
}

func (m *Module) ListEvents(ctx context.Context, systemID uuid.UUID, f EventFilter) ([]InverterEvent, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	where := []string{"e.inverter_id = ANY($1)"}
	args := []any{ids}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.InverterID != nil {
		add("e.inverter_id = $%d", *f.InverterID)
	}
	if f.From != nil {
		add("COALESCE(e.ended_at, 'infinity') >= $%d", *f.From)
	}
	if f.To != nil {
		add("e.started_at < $%d", *f.To)
	}
	if f.StatusCode != nil {
		add("e.status_code = $%d", *f.StatusCode)
	}
	if f.OnlyErrors {
		where = append(where, "COALESCE(e.error_code, 0) <> 0")
	}
	if f.BeforeID != nil {
		add("e.id < $%d", *f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+eventCols+` FROM inverter_events e
		WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY e.id DESC LIMIT %d`, limit), args...)
	return db.Collect(rows, err, scanEvent)
}

func (m *Module) PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]PowerPoint, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT date_bin($4::interval, m.time, TIMESTAMPTZ 'epoch') AS t, d.inverter_id,
			COALESCE(avg(m.pac_w), 0), COALESCE(avg(m.pdc_w), 0), avg(m.udc_v), avg(m.temperature_c)`+samplesFrom+`
		GROUP BY t, d.inverter_id ORDER BY t`, ids, from, to, bucket)
	return db.Collect(rows, err, func(r pgx.Row) (PowerPoint, error) {
		var p PowerPoint
		return p, r.Scan(&p.Time, &p.InverterID, &p.PacW, &p.PdcW, &p.UdcV, &p.TemperatureC)
	})
}

func (m *Module) Samples(ctx context.Context, systemID uuid.UUID, from, to time.Time) ([]Measurement, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT m.time, d.inverter_id, m.pac_w, m.pdc_w, m.udc_v, m.eday_wh, m.temperature_c`+samplesFrom+`
		ORDER BY m.time, array_position($1, d.inverter_id)`, ids, from, to)
	return db.Collect(rows, err, func(r pgx.Row) (Measurement, error) {
		var m Measurement
		return m, r.Scan(&m.Time, &m.InverterID, &m.PacW, &m.PdcW, &m.UdcV, &m.EdayWh, &m.TemperatureC)
	})
}

func (m *Module) Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]EnergyBucket, error) {
	switch granularity {
	case "day", "month", "year":
	default:
		return nil, fmt.Errorf("invalid granularity %q", granularity)
	}
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT date_trunc('`+granularity+`', d.day)::date AS b, d.inverter_id,
			sum(COALESCE(d.manual_energy_wh, d.energy_wh)), max(d.pac_max_w), bool_or(d.manual_energy_wh IS NOT NULL)
		FROM inverter_daily d
		WHERE d.inverter_id = ANY($1) AND d.day >= $2 AND d.day < $3
		GROUP BY b, d.inverter_id ORDER BY b, array_position($1, d.inverter_id)`, ids, pgDate(from), pgDate(to))
	return db.Collect(rows, err, func(r pgx.Row) (EnergyBucket, error) {
		var b EnergyBucket
		return b, r.Scan(&b.Start, &b.InverterID, &b.EnergyWh, &b.PacMaxW, &b.Corrected)
	})
}

func (m *Module) PeakPower(ctx context.Context, systemID uuid.UUID, from, to time.Time) (map[uuid.UUID]float64, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT d.inverter_id, max(m.pac_w)::float8`+samplesFrom+` AND m.pac_w IS NOT NULL
		GROUP BY d.inverter_id`, ids, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]float64{}
	for rows.Next() {
		var id uuid.UUID
		var v float64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

func (m *Module) EnergyDuring(ctx context.Context, systemID uuid.UUID, intervals []Interval) (map[uuid.UUID]float64, error) {
	out := map[uuid.UUID]float64{}
	if len(intervals) == 0 {
		return out, nil
	}
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	starts, ends := make([]time.Time, len(intervals)), make([]time.Time, len(intervals))
	for i, iv := range intervals {
		starts[i], ends[i] = iv.Start, iv.End
	}
	// Only the day rows the intervals touch are expanded.
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT inverter_id, sum(w * hours) FROM (
			SELECT d.inverter_id, avg(u.pac_w) AS w, extract(epoch FROM iv.e - iv.s) / 3600 AS hours
			FROM unnest($2::timestamptz[], $3::timestamptz[]) AS iv(s, e)
			JOIN measurement_days d ON d.day BETWEEN (iv.s AT TIME ZONE 'UTC')::date AND (iv.e AT TIME ZONE 'UTC')::date
				AND d.inverter_id = ANY($1)
			CROSS JOIN LATERAL unnest(d.secs, d.pac_w) AS u(s, pac_w)
			WHERE u.pac_w IS NOT NULL
				AND d.day::timestamp AT TIME ZONE 'UTC' + u.s * interval '1 second' >= iv.s
				AND d.day::timestamp AT TIME ZONE 'UTC' + u.s * interval '1 second' < iv.e
			GROUP BY d.inverter_id, iv.s, iv.e
		) x GROUP BY inverter_id`, ids, starts, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var wh float64
		if err := rows.Scan(&id, &wh); err != nil {
			return nil, err
		}
		out[id] = wh
	}
	return out, rows.Err()
}

// pgDate strips the time zone so pgx encodes the calendar date as-is.
func pgDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// inverterIDs returns the system's inverters in display order (sort order,
// name); the queries above filter and order by them.
func (m *Module) inverterIDs(ctx context.Context, systemID uuid.UUID) ([]uuid.UUID, error) {
	invs, err := m.systems.ListInverters(ctx, systemID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(invs))
	for i, inv := range invs {
		ids[i] = inv.ID
	}
	return ids, nil
}
