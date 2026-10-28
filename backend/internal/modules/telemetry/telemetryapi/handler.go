// Package telemetryapi serves the telemetry module's API operations: data
// pushed by devices and the inverter event log.
package telemetryapi

import (
	"context"
	"strconv"
	"time"

	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

type Handler struct {
	telemetry *telemetry.Module
	authz     httpapi.Authz
}

func New(t *telemetry.Module, roles auth.RoleLookup) *Handler {
	return &Handler{telemetry: t, authz: httpapi.Authz{Roles: roles}}
}

func (h *Handler) IngestMeasurements(ctx context.Context, req *generated.IngestMeasurementsRequest, params generated.IngestMeasurementsParams) (*generated.IngestResult, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataIngest); err != nil {
		return nil, err
	}
	points := make([]telemetry.Point, len(req.Measurements))
	for i, m := range req.Measurements {
		pt := telemetry.Point{
			InverterRef:  m.Inverter,
			Time:         m.Time,
			PacW:         httpapi.PtrOf[float64](m.PacW),
			PdcW:         httpapi.PtrOf[float64](m.PdcW),
			UdcV:         httpapi.PtrOf[float64](m.UdcV),
			EdayWh:       httpapi.PtrOf[float64](m.EdayWh),
			TemperatureC: httpapi.PtrOf[float64](m.TemperatureC),
		}
		if m.StatusCode.Set || m.StatusText.Set || m.ErrorCode.Set || m.ErrorText.Set || m.PowerLimitPct.Set {
			pt.Status = &telemetry.Status{
				Code: httpapi.PtrOf[int](m.StatusCode), Text: m.StatusText.Or(""),
				ErrorCode: httpapi.PtrOf[int](m.ErrorCode), ErrorText: m.ErrorText.Or(""),
				PowerLimitPct: httpapi.PtrOf[int](m.PowerLimitPct),
			}
		}
		points[i] = pt
	}
	res, err := h.telemetry.Measurements(ctx, params.SystemId, points, telemetry.Options{CreateInverters: req.CreateInverters.Or(false)})
	if err != nil {
		return nil, err
	}
	return &generated.IngestResult{Accepted: res.Accepted, Rejected: res.Rejected, InvertersCreated: res.InvertersCreated}, nil
}

func (h *Handler) IngestDaily(ctx context.Context, req *generated.IngestDailyRequest, params generated.IngestDailyParams) (*generated.IngestResult, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataIngest); err != nil {
		return nil, err
	}
	days := make([]telemetry.Day, len(req.Days))
	for i, d := range req.Days {
		days[i] = telemetry.Day{InverterRef: d.Inverter, Date: d.Date, EnergyWh: d.EnergyWh, PacMaxW: httpapi.PtrOf[float64](d.PacMaxW)}
	}
	res, err := h.telemetry.Daily(ctx, params.SystemId, days, telemetry.Options{CreateInverters: req.CreateInverters.Or(false)})
	if err != nil {
		return nil, err
	}
	return &generated.IngestResult{Accepted: res.Accepted, Rejected: res.Rejected, InvertersCreated: res.InvertersCreated}, nil
}

func (h *Handler) ListEvents(ctx context.Context, params generated.ListEventsParams) (*generated.EventPage, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeEventsRead); err != nil {
		return nil, err
	}
	f := telemetry.EventFilter{
		InverterID: httpapi.PtrOf[uuid.UUID](params.Inverter),
		From:       httpapi.PtrOf[time.Time](params.From),
		To:         httpapi.PtrOf[time.Time](params.To),
		StatusCode: httpapi.PtrOf[int](params.Status),
		OnlyErrors: params.ErrorsOnly.Or(false),
		Limit:      params.Limit.Or(100),
	}
	if c, ok := params.Cursor.Get(); ok {
		id, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			return nil, httpapi.BadRequest("invalid cursor")
		}
		f.BeforeID = &id
	}
	evs, err := h.telemetry.ListEvents(ctx, params.SystemId, f)
	if err != nil {
		return nil, err
	}
	out := &generated.EventPage{Items: make([]generated.Event, len(evs))}
	for i, e := range evs {
		out.Items[i] = generated.Event{
			ID: e.ID, InverterID: e.InverterID, StartedAt: e.StartedAt, EndedAt: httpapi.OptTime(e.EndedAt),
			StatusCode: httpapi.OptInt(e.StatusCode), StatusText: e.StatusText,
			ErrorCode: httpapi.OptInt(e.ErrorCode), ErrorText: e.ErrorText, PowerLimitPct: httpapi.OptInt(e.PowerLimitPct),
		}
	}
	if len(evs) == f.Limit && len(evs) > 0 {
		out.NextCursor = generated.NewOptString(strconv.FormatInt(evs[len(evs)-1].ID, 10))
	}
	return out, nil
}
