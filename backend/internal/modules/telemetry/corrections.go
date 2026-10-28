package telemetry

// Manual corrections of daily energy. The device's value stays in
// inverter_daily.energy_wh, so ingest never touches a correction and clearing
// it brings the device value back.

import (
	"context"
	"fmt"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DayEnergyEntry is one inverter's energy on one day: what the device
// reported and the manual correction, if any.
type DayEnergyEntry struct {
	InverterID uuid.UUID
	DeviceWh   *float64
	ManualWh   *float64
	ManualAt   *time.Time
}

// ManualEnergy sets (EnergyWh) or clears (nil) one inverter's correction.
type ManualEnergy struct {
	InverterID uuid.UUID
	EnergyWh   *float64
}

// DayEnergy returns every inverter of the system for the local date day, in
// display order, also those without data.
func (m *Module) DayEnergy(ctx context.Context, systemID uuid.UUID, day time.Time) ([]DayEnergyEntry, error) {
	ids, err := m.inverterIDs(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT i.id,
			-- A row created by a correction alone has no device value.
			CASE WHEN d.energy_wh = 0 AND d.pac_max_w IS NULL AND d.manual_energy_wh IS NOT NULL THEN NULL ELSE d.energy_wh END,
			d.manual_energy_wh, d.manual_at
		FROM unnest($1::uuid[]) WITH ORDINALITY AS i(id, n)
		LEFT JOIN inverter_daily d ON d.inverter_id = i.id AND d.day = $2
		ORDER BY i.n`, ids, pgDate(day))
	return db.Collect(rows, err, func(r pgx.Row) (DayEnergyEntry, error) {
		var e DayEnergyEntry
		return e, r.Scan(&e.InverterID, &e.DeviceWh, &e.ManualWh, &e.ManualAt)
	})
}

// SetManualEnergy corrects or resets the energy of the given inverters on the
// local date day. Values must be plausible (as for ingest) and the day must
// not lie in the future.
func (m *Module) SetManualEnergy(ctx context.Context, systemID uuid.UUID, day time.Time, by uuid.UUID, es []ManualEnergy) error {
	return m.db.InTx(ctx, func(ctx context.Context) error {
		sys, err := m.systems.GetSystem(ctx, systemID)
		if err != nil {
			return err
		}
		now := m.now().In(sys.Loc())
		if pgDate(day).After(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)) {
			return &ValidationError{"day lies in the future"}
		}
		invs, err := m.systems.ListInverters(ctx, systemID)
		if err != nil {
			return err
		}
		lims := make(map[uuid.UUID]limits, len(invs))
		for _, inv := range invs {
			lims[inv.ID] = limitsFor(inv, sys)
		}
		b := &pgx.Batch{}
		for _, e := range es {
			l, ok := lims[e.InverterID]
			if !ok {
				return &ValidationError{"unknown inverter " + e.InverterID.String()}
			}
			if e.EnergyWh == nil {
				b.Queue(`UPDATE inverter_daily SET manual_energy_wh = NULL, manual_by = NULL, manual_at = NULL
					WHERE inverter_id = $1 AND day = $2`, e.InverterID, pgDate(day))
				// A day that only held the correction has nothing left.
				b.Queue(`DELETE FROM inverter_daily WHERE inverter_id = $1 AND day = $2
					AND energy_wh = 0 AND pac_max_w IS NULL AND manual_energy_wh IS NULL`, e.InverterID, pgDate(day))
				continue
			}
			wh := *e.EnergyWh
			switch {
			case wh < 0:
				return &ValidationError{"energy must not be negative"}
			case l.energyWh > 0 && wh > l.energyWh:
				return &ValidationError{fmt.Sprintf("energy above the plausible maximum of %.1f kWh for this inverter", l.energyWh/1000)}
			}
			b.Queue(`
				INSERT INTO inverter_daily (inverter_id, day, manual_energy_wh, manual_by, manual_at) VALUES ($1, $2, $3, $4, now())
				ON CONFLICT (inverter_id, day) DO UPDATE SET
					manual_energy_wh = EXCLUDED.manual_energy_wh, manual_by = EXCLUDED.manual_by, manual_at = EXCLUDED.manual_at`,
				e.InverterID, pgDate(day), wh, by)
		}
		return db.MapErr(m.db.Q(ctx).SendBatch(ctx, b).Close())
	})
}
