package systems

import (
	"context"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const inverterCols = `id, system_id, external_ref, name, model, serial_number, peak_power_kwp, max_ac_power_w, plane_id, color, sort_order, created_at, updated_at`

func scanInverter(r pgx.Row) (Inverter, error) {
	var i Inverter
	err := r.Scan(&i.ID, &i.SystemID, &i.ExternalRef, &i.Name, &i.Model, &i.SerialNumber, &i.PeakPowerKWp, &i.MaxACPowerW, &i.PlaneID, &i.Color, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt)
	return i, db.MapErr(err)
}

func one(i Inverter, err error) (*Inverter, error) {
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (m *Module) CreateInverter(ctx context.Context, inv *Inverter) (*Inverter, error) {
	return one(scanInverter(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO inverters (system_id, external_ref, name, model, serial_number, peak_power_kwp, color, sort_order, max_ac_power_w, plane_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+inverterCols,
		inv.SystemID, inv.ExternalRef, inv.Name, inv.Model, inv.SerialNumber, inv.PeakPowerKWp, inv.Color, inv.SortOrder, inv.MaxACPowerW, inv.PlaneID)))
}

func (m *Module) GetInverter(ctx context.Context, systemID, id uuid.UUID) (*Inverter, error) {
	return one(scanInverter(m.db.Q(ctx).QueryRow(ctx, `SELECT `+inverterCols+` FROM inverters WHERE system_id = $1 AND id = $2`, systemID, id)))
}

func (m *Module) GetInverterByRef(ctx context.Context, systemID uuid.UUID, ref string) (*Inverter, error) {
	return one(scanInverter(m.db.Q(ctx).QueryRow(ctx, `SELECT `+inverterCols+` FROM inverters WHERE system_id = $1 AND external_ref = $2`, systemID, ref)))
}

func (m *Module) UpdateInverter(ctx context.Context, inv *Inverter) (*Inverter, error) {
	return one(scanInverter(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE inverters SET external_ref = $3, name = $4, model = $5, serial_number = $6,
			peak_power_kwp = $7, color = $8, sort_order = $9, max_ac_power_w = $10, plane_id = $11, updated_at = now()
		WHERE system_id = $1 AND id = $2 RETURNING `+inverterCols,
		inv.SystemID, inv.ID, inv.ExternalRef, inv.Name, inv.Model, inv.SerialNumber, inv.PeakPowerKWp, inv.Color, inv.SortOrder, inv.MaxACPowerW, inv.PlaneID)))
}

func (m *Module) DeleteInverter(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM inverters WHERE system_id = $1 AND id = $2`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) ListInverters(ctx context.Context, systemID uuid.UUID) ([]Inverter, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+inverterCols+` FROM inverters WHERE system_id = $1 ORDER BY sort_order, name`, systemID)
	return db.Collect(rows, err, scanInverter)
}
