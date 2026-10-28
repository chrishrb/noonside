package telemetryapi

import (
	"context"
	"math"
	"time"

	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

func (h *Handler) GetDayEnergy(ctx context.Context, params generated.GetDayEnergyParams) (*generated.DayEnergyDetail, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	return h.dayEnergy(ctx, params.SystemId, params.Day)
}

func (h *Handler) SetDayEnergy(ctx context.Context, req *generated.DayEnergyUpdate, params generated.SetDayEnergyParams) (*generated.DayEnergyDetail, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataEdit)
	if err != nil {
		return nil, err
	}
	if !p.IsUser() {
		return nil, httpapi.Forbidden("corrections need a user")
	}
	es := make([]telemetry.ManualEnergy, len(req.Inverters))
	seen := make(map[uuid.UUID]bool, len(req.Inverters))
	for i, it := range req.Inverters {
		if seen[it.InverterID] {
			return nil, httpapi.BadRequest("inverter listed twice")
		}
		seen[it.InverterID] = true
		es[i] = telemetry.ManualEnergy{InverterID: it.InverterID}
		if v, ok := it.EnergyKwh.Get(); ok {
			wh := math.Round(v * 1000)
			es[i].EnergyWh = &wh
		}
	}
	if err := h.telemetry.SetManualEnergy(ctx, params.SystemId, params.Day, p.UserID, es); err != nil {
		return nil, err
	}
	return h.dayEnergy(ctx, params.SystemId, params.Day)
}

func (h *Handler) dayEnergy(ctx context.Context, systemID uuid.UUID, day time.Time) (*generated.DayEnergyDetail, error) {
	es, err := h.telemetry.DayEnergy(ctx, systemID, day)
	if err != nil {
		return nil, err
	}
	out := &generated.DayEnergyDetail{Date: day, Inverters: make([]generated.DayEnergyInverter, len(es))}
	for i, e := range es {
		out.Inverters[i] = generated.DayEnergyInverter{
			InverterID: e.InverterID,
			DeviceKwh:  nilKWh(e.DeviceWh),
			ManualKwh:  nilKWh(e.ManualWh),
			ManualAt:   generated.NilDateTime{Null: true},
		}
		if e.ManualAt != nil {
			out.Inverters[i].ManualAt = generated.NewNilDateTime(*e.ManualAt)
		}
	}
	return out, nil
}

func nilKWh(wh *float64) generated.NilFloat64 {
	if wh == nil {
		return generated.NilFloat64{Null: true}
	}
	return generated.NewNilFloat64(math.Round(*wh) / 1000)
}
