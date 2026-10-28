package telemetry

// Ingest is the single write path for measurements, daily energy and
// inverter status, shared by the push API and the polling adapters.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
)

// ValidationError is returned for input the caller must fix.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// StatusCode makes the API answer 400 with the message.
func (e *ValidationError) StatusCode() int { return http.StatusBadRequest }

// Point is one reading of one inverter.
type Point struct {
	InverterRef  string
	Time         time.Time
	PacW         *float64
	PdcW         *float64
	UdcV         *float64
	EdayWh       *float64
	TemperatureC *float64
	Status       *Status
}

// Status is an inverter's operating state at a point in time.
type Status struct {
	Code      *int
	Text      string
	ErrorCode *int
	ErrorText string
	// PowerLimitPct is an active power limit the device reports
	// (curtailment); nil or 100 means none.
	PowerLimitPct *int
}

func (s *Status) same(e *InverterEvent) bool {
	return eqInt(s.Code, e.StatusCode) && eqInt(s.ErrorCode, e.ErrorCode) && s.Text == e.StatusText && s.ErrorText == e.ErrorText &&
		eqInt(s.limit(), e.PowerLimitPct)
}

func (s *Status) onlyLimit() bool {
	return s.PowerLimitPct != nil && s.Code == nil && s.Text == "" && s.ErrorCode == nil && s.ErrorText == ""
}

// limit is the power limit, nil without one (100 % is no limit).
func (s *Status) limit() *int {
	if s.PowerLimitPct == nil || *s.PowerLimitPct >= 100 {
		return nil
	}
	return s.PowerLimitPct
}

func eqInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Day is the energy of one inverter on one local date.
type Day struct {
	InverterRef string
	Date        time.Time
	EnergyWh    float64
	PacMaxW     *float64
}

type Options struct {
	// CreateInverters creates unknown inverter refs instead of failing.
	CreateInverters bool
}

type Result struct {
	Accepted int
	// Rejected counts the implausible values dropped: whole points or days,
	// and single fields of points that were otherwise stored.
	Rejected         int
	InvertersCreated int
}

// Measurements stores readings, derives daily energy from eday and updates
// the live state and the event log.
func (m *Module) Measurements(ctx context.Context, systemID uuid.UUID, points []Point, opts Options) (Result, error) {
	var res Result
	err := m.db.InTx(ctx, func(ctx context.Context) error {
		sys, err := m.systems.GetSystem(ctx, systemID)
		if err != nil {
			return err
		}
		refs := make([]string, 0, len(points))
		for _, p := range points {
			if p.Time.IsZero() {
				return &ValidationError{Msg: "measurement time is required"}
			}
			refs = append(refs, p.InverterRef)
		}
		inverters, created, err := m.resolveInverters(ctx, systemID, refs, opts)
		if err != nil {
			return err
		}
		res.InvertersCreated = created

		loc := sys.Loc()
		now := m.now()
		localDay := func(t time.Time) time.Time {
			l := t.In(loc)
			return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
		}
		rejected := rejections{}
		ms := make([]Measurement, 0, len(points))
		type dayKey struct {
			inv uuid.UUID
			day time.Time
		}
		days := map[dayKey]*DailyEnergy{}
		byInverter := map[uuid.UUID][]Point{}
		// Minute data before the retention cutoff is not stored; it still
		// counts for daily energy.
		cutoff := sys.MinuteCutoff(now)
		for _, p := range points {
			inv := inverters[p.InverterRef]
			id := inv.ID
			rs, drop := checkPoint(&p, limitsFor(inv, sys), now)
			for _, r := range rs {
				rejected.add(id, localDay(p.Time), r)
			}
			res.Rejected += len(rs)
			if drop {
				continue
			}
			res.Accepted++
			if cutoff == nil || !p.Time.Before(*cutoff) {
				ms = append(ms, Measurement{
					Time: p.Time, InverterID: id, PacW: p.PacW, PdcW: p.PdcW,
					UdcV: p.UdcV, EdayWh: p.EdayWh, TemperatureC: p.TemperatureC,
				})
			}
			byInverter[id] = append(byInverter[id], p)

			k := dayKey{id, localDay(p.Time)}
			d, ok := days[k]
			if !ok {
				d = &DailyEnergy{InverterID: id, Day: k.day}
				days[k] = d
			}
			if p.EdayWh != nil && *p.EdayWh > d.EnergyWh {
				d.EnergyWh = *p.EdayWh
			}
			if p.PacW != nil && (d.PacMaxW == nil || *p.PacW > *d.PacMaxW) {
				v := *p.PacW
				d.PacMaxW = &v
			}
		}
		if err := m.UpsertMeasurements(ctx, ms); err != nil {
			return fmt.Errorf("upsert measurements: %w", err)
		}
		daily := make([]DailyEnergy, 0, len(days))
		for _, d := range days {
			daily = append(daily, *d)
		}
		if err := m.UpsertDailyEnergy(ctx, daily); err != nil {
			return fmt.Errorf("upsert daily energy: %w", err)
		}
		for id, ps := range byInverter {
			if err := m.applyState(ctx, id, ps); err != nil {
				return err
			}
		}
		if err := m.RecordIngestRejections(ctx, rejected.list(systemID)); err != nil {
			return fmt.Errorf("record rejections: %w", err)
		}
		return nil
	})
	return res, err
}

// Daily stores daily energy totals (e.g. history backfill).
func (m *Module) Daily(ctx context.Context, systemID uuid.UUID, days []Day, opts Options) (Result, error) {
	var res Result
	err := m.db.InTx(ctx, func(ctx context.Context) error {
		sys, err := m.systems.GetSystem(ctx, systemID)
		if err != nil {
			return err
		}
		refs := make([]string, len(days))
		for i, d := range days {
			refs[i] = d.InverterRef
		}
		inverters, created, err := m.resolveInverters(ctx, systemID, refs, opts)
		if err != nil {
			return err
		}
		res.InvertersCreated = created
		rejected := rejections{}
		out := make([]DailyEnergy, 0, len(days))
		for _, d := range days {
			inv := inverters[d.InverterRef]
			day := time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), 0, 0, 0, 0, time.UTC)
			if r, bad := checkDay(&d, limitsFor(inv, sys)); bad {
				rejected.add(inv.ID, day, r)
				res.Rejected++
				continue
			}
			out = append(out, DailyEnergy{InverterID: inv.ID, Day: day, EnergyWh: d.EnergyWh, PacMaxW: d.PacMaxW})
		}
		if err := m.UpsertDailyEnergy(ctx, out); err != nil {
			return err
		}
		res.Accepted = len(out)
		return m.RecordIngestRejections(ctx, rejected.list(systemID))
	})
	return res, err
}

// State records a status/power snapshot without storing a measurement
// (e.g. Solar-Log's min_cur.js).
func (m *Module) State(ctx context.Context, systemID uuid.UUID, points []Point, opts Options) error {
	return m.db.InTx(ctx, func(ctx context.Context) error {
		refs := make([]string, len(points))
		for i, p := range points {
			refs[i] = p.InverterRef
		}
		inverters, _, err := m.resolveInverters(ctx, systemID, refs, opts)
		if err != nil {
			return err
		}
		for _, p := range points {
			if err := m.applyState(ctx, inverters[p.InverterRef].ID, []Point{p}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Module) resolveInverters(ctx context.Context, systemID uuid.UUID, refs []string, opts Options) (map[string]systems.Inverter, int, error) {
	existing, err := m.systems.ListInverters(ctx, systemID)
	if err != nil {
		return nil, 0, err
	}
	out := make(map[string]systems.Inverter, len(existing))
	for _, inv := range existing {
		out[inv.ExternalRef] = inv
	}
	created := 0
	for _, ref := range refs {
		if ref == "" {
			return nil, 0, &ValidationError{Msg: "inverter ref is required"}
		}
		if _, ok := out[ref]; ok {
			continue
		}
		if !opts.CreateInverters {
			return nil, 0, &ValidationError{Msg: fmt.Sprintf("unknown inverter %q", ref)}
		}
		inv, err := m.systems.CreateInverter(ctx, &systems.Inverter{
			SystemID: systemID, ExternalRef: ref, Name: "WR " + ref, SortOrder: len(out) + 1,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("create inverter %q: %w", ref, err)
		}
		out[ref] = *inv
		created++
	}
	return out, created, nil
}

// applyState updates the live state from the newest point and turns status
// changes into events. Points may arrive in any order.
func (m *Module) applyState(ctx context.Context, inverterID uuid.UUID, points []Point) error {
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })

	open, err := m.OpenEvent(ctx, inverterID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	for i := range points {
		p := &points[i]
		if p.Status == nil {
			continue
		}
		if open != nil && p.Status.onlyLimit() {
			// A limit alone keeps the current status.
			st := *p.Status
			st.Code, st.Text, st.ErrorCode, st.ErrorText = open.StatusCode, open.StatusText, open.ErrorCode, open.ErrorText
			p.Status = &st
		}
		if open != nil {
			if !p.Time.After(open.StartedAt) || p.Status.same(open) {
				continue
			}
			if err := m.CloseEvent(ctx, open.ID, p.Time); err != nil {
				return err
			}
		}
		ev := InverterEvent{
			InverterID: inverterID, StartedAt: p.Time,
			StatusCode: p.Status.Code, StatusText: p.Status.Text,
			ErrorCode: p.Status.ErrorCode, ErrorText: p.Status.ErrorText, PowerLimitPct: p.Status.limit(),
		}
		if ev.ID, err = m.InsertEvent(ctx, ev); err != nil {
			return err
		}
		open = &ev
	}

	last := points[len(points)-1]
	st := InverterState{InverterID: inverterID, Time: last.Time, PacW: last.PacW, PdcW: last.PdcW}
	if last.Status != nil {
		st.StatusCode, st.StatusText = last.Status.Code, last.Status.Text
		st.ErrorCode, st.ErrorText = last.Status.ErrorCode, last.Status.ErrorText
		st.PowerLimitPct = last.Status.limit()
	} else if open != nil {
		st.StatusCode, st.StatusText = open.StatusCode, open.StatusText
		st.ErrorCode, st.ErrorText = open.ErrorCode, open.ErrorText
		st.PowerLimitPct = open.PowerLimitPct
	}
	return m.UpsertInverterState(ctx, st)
}
