package systems

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const systemCols = `s.id, s.name, s.location, s.latitude, s.longitude, s.timezone, s.peak_power_kwp,
	s.commissioned_at, s.orientation, s.module_info, s.operator, s.tariff_per_kwh, s.currency,
	s.co2_kg_per_kwh, s.minute_retention_years, s.price_zone, s.unpaid_at_negative_prices, s.image_key, s.created_at, s.updated_at,
	(SELECT coalesce(json_agg(json_build_object('id', p.id, 'name', p.name, 'kwp', p.kwp, 'tilt_deg', p.tilt_deg,
		'azimuth_deg', p.azimuth_deg) ORDER BY p.sort_order), '[]') FROM system_planes p WHERE p.system_id = s.id)`

type planeJSON struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	KWp        float64   `json:"kwp"`
	TiltDeg    float64   `json:"tilt_deg"`
	AzimuthDeg float64   `json:"azimuth_deg"`
}

func scanSystemInto(r pgx.Row, extra ...any) (*System, error) {
	var s System
	var planes []byte
	dest := []any{&s.ID, &s.Name, &s.Location, &s.Latitude, &s.Longitude, &s.Timezone, &s.PeakPowerKWp,
		&s.CommissionedAt, &s.Orientation, &s.ModuleInfo, &s.Operator, &s.TariffPerKWh, &s.Currency,
		&s.CO2KgPerKWh, &s.MinuteRetentionYears, &s.PriceZone, &s.UnpaidAtNegativePrices, &s.ImageKey, &s.CreatedAt, &s.UpdatedAt, &planes}
	if err := r.Scan(append(dest, extra...)...); err != nil {
		return nil, db.MapErr(err)
	}
	var ps []planeJSON
	if err := json.Unmarshal(planes, &ps); err != nil {
		return nil, err
	}
	for _, p := range ps {
		s.Planes = append(s.Planes, Plane(p))
	}
	return &s, nil
}

// setPlanes replaces the system's planes. Planes with a known ID are
// updated in place, so their inverters stay assigned; planes that are no
// longer listed are deleted, which unassigns their inverters. An ID that is
// not one of the system's planes returns ErrNotFound.
func (m *Module) setPlanes(ctx context.Context, systemID uuid.UUID, planes []Plane) error {
	keep := []uuid.UUID{}
	for _, p := range planes {
		if p.ID != uuid.Nil {
			keep = append(keep, p.ID)
		}
	}
	var known int
	if err := m.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM system_planes WHERE system_id = $1 AND id = ANY($2)`,
		systemID, keep).Scan(&known); err != nil {
		return err
	}
	if known != len(keep) { // also catches duplicate IDs
		return db.ErrNotFound
	}
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM system_planes WHERE system_id = $1 AND NOT (id = ANY($2))`, systemID, keep)
	for i, p := range planes {
		if p.ID != uuid.Nil {
			b.Queue(`UPDATE system_planes SET name = $3, kwp = $4, tilt_deg = $5, azimuth_deg = $6, sort_order = $7
				WHERE system_id = $1 AND id = $2`, systemID, p.ID, p.Name, p.KWp, p.TiltDeg, p.AzimuthDeg, i)
			continue
		}
		b.Queue(`INSERT INTO system_planes (system_id, name, kwp, tilt_deg, azimuth_deg, sort_order) VALUES ($1, $2, $3, $4, $5, $6)`,
			systemID, p.Name, p.KWp, p.TiltDeg, p.AzimuthDeg, i)
	}
	return db.MapErr(m.db.Q(ctx).SendBatch(ctx, b).Close())
}

func (m *Module) CreateSystem(ctx context.Context, sys *System) (*System, error) {
	var out *System
	err := m.db.InTx(ctx, func(ctx context.Context) error {
		var id uuid.UUID
		err := m.db.Q(ctx).QueryRow(ctx, `
			INSERT INTO systems (name, location, latitude, longitude, timezone, peak_power_kwp,
				commissioned_at, orientation, module_info, operator, tariff_per_kwh, currency, co2_kg_per_kwh,
				minute_retention_years, price_zone, unpaid_at_negative_prices)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING id`,
			sys.Name, sys.Location, sys.Latitude, sys.Longitude, sys.Timezone, sys.PeakPowerKWp,
			sys.CommissionedAt, sys.Orientation, sys.ModuleInfo, sys.Operator, sys.TariffPerKWh,
			sys.Currency, sys.CO2KgPerKWh, sys.MinuteRetentionYears, sys.PriceZone, sys.UnpaidAtNegativePrices).Scan(&id)
		if err != nil {
			return db.MapErr(err)
		}
		if err := m.setPlanes(ctx, id, sys.Planes); err != nil {
			return err
		}
		out, err = m.GetSystem(ctx, id)
		return err
	})
	return out, err
}

func (m *Module) GetSystem(ctx context.Context, id uuid.UUID) (*System, error) {
	return scanSystemInto(m.db.Q(ctx).QueryRow(ctx, `SELECT `+systemCols+` FROM systems s WHERE s.id = $1`, id))
}

func (m *Module) UpdateSystem(ctx context.Context, sys *System) (*System, error) {
	var out *System
	err := m.db.InTx(ctx, func(ctx context.Context) error {
		tag, err := m.db.Q(ctx).Exec(ctx, `
			UPDATE systems SET name = $2, location = $3, latitude = $4, longitude = $5, timezone = $6,
				peak_power_kwp = $7, commissioned_at = $8, orientation = $9, module_info = $10,
				operator = $11, tariff_per_kwh = $12, currency = $13, co2_kg_per_kwh = $14,
				minute_retention_years = $15, price_zone = $16, unpaid_at_negative_prices = $17, updated_at = now()
			WHERE id = $1`,
			sys.ID, sys.Name, sys.Location, sys.Latitude, sys.Longitude, sys.Timezone, sys.PeakPowerKWp,
			sys.CommissionedAt, sys.Orientation, sys.ModuleInfo, sys.Operator, sys.TariffPerKWh,
			sys.Currency, sys.CO2KgPerKWh, sys.MinuteRetentionYears, sys.PriceZone, sys.UnpaidAtNegativePrices)
		if err != nil {
			return db.MapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return db.ErrNotFound
		}
		if err := m.setPlanes(ctx, sys.ID, sys.Planes); err != nil {
			return err
		}
		out, err = m.GetSystem(ctx, sys.ID)
		return err
	})
	return out, err
}

func (m *Module) SetSystemCoordinates(ctx context.Context, id uuid.UUID, location string, lat, lon float64) error {
	_, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE systems SET latitude = $3, longitude = $4, updated_at = now()
		WHERE id = $1 AND location = $2 AND latitude IS NULL AND longitude IS NULL`, id, location, lat, lon)
	return err
}

// SetImageKey sets the picture's key (nil: none) and returns the previous
// one.
func (m *Module) SetImageKey(ctx context.Context, id uuid.UUID, key *string) (old *string, err error) {
	err = m.db.Q(ctx).QueryRow(ctx, `
		UPDATE systems s SET image_key = $2, updated_at = now()
		FROM (SELECT image_key FROM systems WHERE id = $1 FOR UPDATE) o
		WHERE s.id = $1 RETURNING o.image_key`, id, key).Scan(&old)
	return old, db.MapErr(err)
}

func (m *Module) deleteSystem(ctx context.Context, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM systems WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) ListSystems(ctx context.Context, userID *uuid.UUID) ([]SystemWithRole, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if userID == nil {
		rows, err = m.db.Q(ctx).Query(ctx, `SELECT `+systemCols+`, 'owner' FROM systems s ORDER BY s.name`)
	} else {
		rows, err = m.db.Q(ctx).Query(ctx, `
			SELECT `+systemCols+`, m.role FROM systems s
			JOIN system_members m ON m.system_id = s.id AND m.user_id = $1
			ORDER BY s.name`, *userID)
	}
	return db.Collect(rows, err, func(r pgx.Row) (SystemWithRole, error) {
		var role auth.Role
		sys, err := scanSystemInto(r, &role)
		if err != nil {
			return SystemWithRole{}, err
		}
		return SystemWithRole{System: *sys, Role: role}, nil
	})
}

// AdminSystems lists every system with its inverter and member counts and
// owners, in one query plus one for the owners' profiles.
func (m *Module) AdminSystems(ctx context.Context) ([]AdminSystem, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+systemCols+`,
		(SELECT count(*) FROM inverters i WHERE i.system_id = s.id),
		(SELECT count(*) FROM system_members sm WHERE sm.system_id = s.id),
		coalesce((SELECT array_agg(sm.user_id) FROM system_members sm WHERE sm.system_id = s.id AND sm.role = 'owner'), '{}')
		FROM systems s ORDER BY s.name`)
	out, err := db.Collect(rows, err, func(r pgx.Row) (AdminSystem, error) {
		var a AdminSystem
		var owners []uuid.UUID
		sys, err := scanSystemInto(r, &a.InverterCount, &a.MemberCount, &owners)
		if err != nil {
			return a, err
		}
		a.System = *sys
		for _, id := range owners {
			a.Owners = append(a.Owners, Member{SystemID: sys.ID, UserID: id, Role: auth.RoleOwner})
		}
		return a, nil
	})
	if err != nil {
		return nil, err
	}
	var all []Member
	for _, a := range out {
		all = append(all, a.Owners...)
	}
	if err := m.withProfiles(ctx, all); err != nil {
		return nil, err
	}
	k := 0
	for i := range out {
		n := len(out[i].Owners)
		out[i].Owners = all[k : k+n : k+n]
		k += n
		slices.SortFunc(out[i].Owners, func(a, b Member) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Email, b.Email))
		})
	}
	return out, nil
}

func (m *Module) GetTargets(ctx context.Context, systemID uuid.UUID) (map[int]float64, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT month, target_kwh FROM system_targets WHERE system_id = $1`, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]float64{}
	for rows.Next() {
		var m int
		var v float64
		if err := rows.Scan(&m, &v); err != nil {
			return nil, err
		}
		out[m] = v
	}
	return out, rows.Err()
}

func (m *Module) SetTargets(ctx context.Context, systemID uuid.UUID, targets map[int]float64) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM system_targets WHERE system_id = $1`, systemID)
	for m, v := range targets {
		b.Queue(`INSERT INTO system_targets (system_id, month, target_kwh) VALUES ($1, $2, $3)`, systemID, m, v)
	}
	return db.MapErr(m.db.Q(ctx).SendBatch(ctx, b).Close())
}
