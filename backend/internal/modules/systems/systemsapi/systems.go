package systemsapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/systems/pvgis"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/google/uuid"
)

func (h *Handler) ListSystems(ctx context.Context) ([]generated.System, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	var filter *uuid.UUID
	if !p.Superadmin {
		filter = &p.UserID
	}
	systems, err := h.systems.ListSystems(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]generated.System, len(systems))
	for i := range systems {
		out[i] = *toSystem(&systems[i].System, systems[i].Role)
	}
	return out, nil
}

func (h *Handler) ListAdminSystems(ctx context.Context) ([]generated.AdminSystem, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Superadmin {
		return nil, httpapi.Forbidden("superadmin only")
	}
	list, err := h.systems.AdminSystems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]generated.AdminSystem, len(list))
	for i := range list {
		a := &list[i]
		owners := make([]generated.AdminSystemOwner, len(a.Owners))
		for j, o := range a.Owners {
			owners[j] = generated.AdminSystemOwner{UserID: o.UserID, Email: o.Email, Name: o.Name}
		}
		out[i] = generated.AdminSystem{
			System: *toSystem(&a.System, auth.RoleOwner), InverterCount: a.InverterCount,
			MemberCount: a.MemberCount, Owners: owners,
		}
	}
	return out, nil
}

func (h *Handler) CreateSystem(ctx context.Context, req *generated.SystemCreate) (*generated.System, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Superadmin {
		return nil, httpapi.Forbidden("only superadmins can create systems")
	}
	sys := &systems.System{Name: strings.TrimSpace(req.Name), Timezone: "Europe/Berlin", Currency: "EUR", CO2KgPerKWh: 0.7}
	if err := h.applySystemFields(ctx, sys, systemFields{
		Location: req.Location, Latitude: req.Latitude, Longitude: req.Longitude, Timezone: req.Timezone,
		PeakPowerKwp: req.PeakPowerKwp, CommissionedAt: req.CommissionedAt, Orientation: req.Orientation,
		ModuleInfo: req.ModuleInfo, Operator: req.Operator, TariffPerKwh: req.TariffPerKwh,
		Currency: req.Currency, Co2KgPerKwh: req.Co2KgPerKwh, MinuteRetentionYears: req.MinuteRetentionYears,
		Planes: req.Planes, PriceZone: optZone(req.PriceZone.Set, req.PriceZone.Null, req.PriceZone.Value),
		UnpaidAtNegativePrices: req.UnpaidAtNegativePrices,
	}); err != nil {
		return nil, err
	}
	var created *systems.System
	err = h.systems.InTx(ctx, func(ctx context.Context) error {
		var err error
		if created, err = h.systems.CreateSystem(ctx, sys); err != nil {
			return err
		}
		return h.systems.UpsertMember(ctx, created.ID, p.UserID, auth.RoleOwner)
	})
	if err != nil {
		return nil, err
	}
	return toSystem(created, auth.RoleOwner), nil
}

func (h *Handler) GetSystem(ctx context.Context, params generated.GetSystemParams) (*generated.System, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemRead)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	return toSystem(sys, h.effectiveRole(ctx, p, sys.ID)), nil
}

func (h *Handler) UpdateSystem(ctx context.Context, req *generated.SystemUpdate, params generated.UpdateSystemParams) (*generated.System, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemWrite)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	if v, ok := req.Name.Get(); ok {
		sys.Name = strings.TrimSpace(v)
	}
	if err := h.applySystemFields(ctx, sys, systemFields{
		Location: req.Location, Latitude: req.Latitude, Longitude: req.Longitude, Timezone: req.Timezone,
		PeakPowerKwp: req.PeakPowerKwp, CommissionedAt: req.CommissionedAt, Orientation: req.Orientation,
		ModuleInfo: req.ModuleInfo, Operator: req.Operator, TariffPerKwh: req.TariffPerKwh,
		Currency: req.Currency, Co2KgPerKwh: req.Co2KgPerKwh, MinuteRetentionYears: req.MinuteRetentionYears,
		Planes: req.Planes, PriceZone: optZone(req.PriceZone.Set, req.PriceZone.Null, req.PriceZone.Value),
		UnpaidAtNegativePrices: req.UnpaidAtNegativePrices,
	}); err != nil {
		return nil, err
	}
	if sys, err = h.systems.UpdateSystem(ctx, sys); err != nil {
		return nil, err
	}
	// Minute data past a shorter retention is deleted by the scheduler within
	// a minute; large histories would not fit the API's 60 s limit.
	return toSystem(sys, h.effectiveRole(ctx, p, sys.ID)), nil
}

func (h *Handler) DeleteSystem(ctx context.Context, params generated.DeleteSystemParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemDelete); err != nil {
		return err
	}
	// Modules with external resources (hosted FTP accounts) clean them up
	// through systems.Module.OnDelete.
	return h.systems.DeleteSystem(ctx, params.SystemId)
}

func (h *Handler) GetTargets(ctx context.Context, params generated.GetTargetsParams) (*generated.MonthlyTargets, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemRead); err != nil {
		return nil, err
	}
	t, err := h.systems.GetTargets(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	return toTargets(t), nil
}

func (h *Handler) SetTargets(ctx context.Context, req *generated.MonthlyTargets, params generated.SetTargetsParams) (*generated.MonthlyTargets, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemWrite); err != nil {
		return nil, err
	}
	t := make(map[int]float64, 12)
	for i, v := range req.TargetKwh {
		t[i+1] = v
	}
	if err := h.systems.SetTargets(ctx, params.SystemId, t); err != nil {
		return nil, err
	}
	return toTargets(t), nil
}

func (h *Handler) EstimateTargets(ctx context.Context, params generated.EstimateTargetsParams) (*generated.MonthlyTargets, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemWrite); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	if h.cfg.Targets == nil {
		return nil, httpapi.BadRequest("target estimates are disabled")
	}
	if sys.Latitude == nil || sys.Longitude == nil {
		return nil, httpapi.BadRequest("the system has no coordinates; set its address first")
	}
	var planes []pvgis.Plane
	for _, p := range sys.Planes {
		planes = append(planes, pvgis.Plane{KWp: p.KWp, Tilt: p.TiltDeg, Azimuth: p.AzimuthDeg})
	}
	if len(planes) == 0 && sys.PeakPowerKWp <= 0 {
		return nil, httpapi.BadRequest("the system has no peak power or planes")
	}
	months, err := h.cfg.Targets.Monthly(ctx, *sys.Latitude, *sys.Longitude, planes, sys.PeakPowerKWp)
	var se *pvgis.StatusError
	if errors.As(err, &se) && se.Status == http.StatusBadRequest {
		return nil, httpapi.BadRequest("PVGIS has no data for this location: " + se.Message)
	}
	if err != nil {
		h.logger.WarnContext(ctx, "pvgis estimate", "system", sys.ID, "error", err)
		return nil, httpapi.BadGateway("PVGIS is unavailable; try again later")
	}
	out := &generated.MonthlyTargets{TargetKwh: make([]float64, 12)}
	for i, v := range months {
		out.TargetKwh[i] = math.Round(v)
	}
	return out, nil
}

func toTargets(t map[int]float64) *generated.MonthlyTargets {
	out := &generated.MonthlyTargets{TargetKwh: make([]float64, 12)}
	for m := 1; m <= 12; m++ {
		out.TargetKwh[m-1] = t[m]
	}
	return out
}

// effectiveRole returns the caller's role for display; errors fall back to viewer.
func (h *Handler) effectiveRole(ctx context.Context, p *auth.Principal, systemID uuid.UUID) auth.Role {
	if !p.IsUser() {
		return auth.RoleViewer
	}
	r, err := h.authz.RoleOf(ctx, p, systemID)
	if err != nil {
		return auth.RoleViewer
	}
	return r
}

type systemFields struct {
	Location       generated.OptString
	Latitude       generated.OptFloat64
	Longitude      generated.OptFloat64
	Timezone       generated.OptString
	PeakPowerKwp   generated.OptFloat64
	CommissionedAt generated.OptDate
	Orientation    generated.OptString
	ModuleInfo     generated.OptString
	Operator       generated.OptString
	TariffPerKwh   generated.OptFloat64
	Currency       generated.OptString
	Co2KgPerKwh    generated.OptFloat64
	// MinuteRetentionYears: null clears it (keep everything).
	MinuteRetentionYears generated.OptNilInt
	// Planes replace the system's planes unless nil (not given).
	Planes []generated.Plane
	// PriceZone is nil if not given; a nil value clears it.
	PriceZone              *nullable[string]
	UnpaidAtNegativePrices generated.OptBool
}

// nullable is a given value that may be null (V nil).
type nullable[T any] struct{ V *T }

// optZone converts an optional nullable price zone of the request.
func optZone[T ~string](set, null bool, v T) *nullable[string] {
	if !set {
		return nil
	}
	if null {
		return &nullable[string]{}
	}
	z := string(v)
	return &nullable[string]{V: &z}
}

// applySystemFields applies f and geocodes a changed address unless the
// coordinates are given too.
func (h *Handler) applySystemFields(ctx context.Context, s *systems.System, f systemFields) error {
	prev := s.Location
	if err := setSystemFields(s, f); err != nil {
		return err
	}
	if !f.Location.Set || s.Location == prev || f.Latitude.Set || f.Longitude.Set {
		return nil
	}
	if s.Location == "" {
		s.Latitude, s.Longitude = nil, nil
		return nil
	}
	if h.cfg.Geocoder == nil {
		return nil
	}
	places, err := h.cfg.Geocoder.Search(ctx, s.Location, "", 1)
	if err != nil {
		// Don't block saving while the geocoder is down; the coordinates stay.
		h.logger.WarnContext(ctx, "geocode system location", "error", err)
		return nil
	}
	if len(places) == 0 {
		return httpapi.BadRequest("location not found; check the address or pick a suggestion")
	}
	s.Latitude, s.Longitude = &places[0].Lat, &places[0].Lon
	return nil
}

func setSystemFields(s *systems.System, f systemFields) error {
	if v, ok := f.Timezone.Get(); ok {
		if _, err := time.LoadLocation(v); err != nil || v == "" || v == "Local" {
			return httpapi.BadRequest("unknown timezone " + v)
		}
		s.Timezone = v
	}
	if v, ok := f.Location.Get(); ok {
		s.Location = strings.TrimSpace(v)
	}
	if v := httpapi.PtrOf[float64](f.Latitude); v != nil {
		s.Latitude = v
	}
	if v := httpapi.PtrOf[float64](f.Longitude); v != nil {
		s.Longitude = v
	}
	if v, ok := f.PeakPowerKwp.Get(); ok {
		s.PeakPowerKWp = v
	}
	if v := httpapi.PtrOf[time.Time](f.CommissionedAt); v != nil {
		s.CommissionedAt = v
	}
	if v, ok := f.Orientation.Get(); ok {
		s.Orientation = v
	}
	if v, ok := f.ModuleInfo.Get(); ok {
		s.ModuleInfo = v
	}
	if v, ok := f.Operator.Get(); ok {
		s.Operator = v
	}
	if v, ok := f.TariffPerKwh.Get(); ok {
		s.TariffPerKWh = v
	}
	if v, ok := f.Currency.Get(); ok {
		s.Currency = strings.ToUpper(v)
	}
	if v, ok := f.Co2KgPerKwh.Get(); ok {
		s.CO2KgPerKWh = v
	}
	if f.MinuteRetentionYears.Set {
		if v, ok := f.MinuteRetentionYears.Get(); ok {
			s.MinuteRetentionYears = &v
		} else {
			s.MinuteRetentionYears = nil
		}
	}
	if f.PriceZone != nil {
		s.PriceZone = f.PriceZone.V
	}
	if v, ok := f.UnpaidAtNegativePrices.Get(); ok {
		s.UnpaidAtNegativePrices = v
	}
	if f.Planes != nil {
		// A plane ID keeps that plane (and its inverters); it must be one of
		// the system's current planes.
		known := map[uuid.UUID]bool{}
		for _, p := range s.Planes {
			known[p.ID] = true
		}
		planes := make([]systems.Plane, len(f.Planes))
		for i, p := range f.Planes {
			id := p.ID.Or(uuid.Nil)
			if id != uuid.Nil && !known[id] {
				return httpapi.BadRequest("unknown plane " + id.String())
			}
			delete(known, id)
			planes[i] = systems.Plane{ID: id, Name: strings.TrimSpace(p.Name.Or("")), KWp: p.Kwp, TiltDeg: p.TiltDeg, AzimuthDeg: p.AzimuthDeg}
		}
		s.Planes = planes
	}
	return nil
}

// --- inverters ---------------------------------------------------------------

func (h *Handler) ListInverters(ctx context.Context, params generated.ListInvertersParams) ([]generated.Inverter, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemRead); err != nil {
		return nil, err
	}
	invs, err := h.systems.ListInverters(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.Inverter, len(invs))
	for i := range invs {
		out[i] = toInverter(&invs[i])
	}
	return out, nil
}

func (h *Handler) CreateInverter(ctx context.Context, req *generated.InverterCreate, params generated.CreateInverterParams) (*generated.Inverter, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeInverterWrite); err != nil {
		return nil, err
	}
	inv := &systems.Inverter{
		SystemID:     params.SystemId,
		ExternalRef:  strings.TrimSpace(req.ExternalRef),
		Name:         strings.TrimSpace(req.Name),
		Model:        req.Model.Or(""),
		SerialNumber: req.SerialNumber.Or(""),
		PeakPowerKWp: req.PeakPowerKwp.Or(0),
		MaxACPowerW:  httpapi.PtrOf[float64](req.MaxAcPowerW),
		PlaneID:      httpapi.PtrOf[uuid.UUID](req.PlaneID),
		Color:        req.Color.Or(""),
		SortOrder:    req.SortOrder.Or(0),
	}
	if err := h.checkPlane(ctx, inv); err != nil {
		return nil, err
	}
	inv, err := h.systems.CreateInverter(ctx, inv)
	if err != nil {
		return nil, err
	}
	out := toInverter(inv)
	return &out, nil
}

func (h *Handler) UpdateInverter(ctx context.Context, req *generated.InverterUpdate, params generated.UpdateInverterParams) (*generated.Inverter, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeInverterWrite); err != nil {
		return nil, err
	}
	inv, err := h.systems.GetInverter(ctx, params.SystemId, params.InverterId)
	if err != nil {
		return nil, err
	}
	if v, ok := req.ExternalRef.Get(); ok {
		inv.ExternalRef = strings.TrimSpace(v)
	}
	if v, ok := req.Name.Get(); ok {
		inv.Name = strings.TrimSpace(v)
	}
	if v, ok := req.Model.Get(); ok {
		inv.Model = v
	}
	if v, ok := req.SerialNumber.Get(); ok {
		inv.SerialNumber = v
	}
	if v, ok := req.PeakPowerKwp.Get(); ok {
		inv.PeakPowerKWp = v
	}
	if req.MaxAcPowerW.Set {
		inv.MaxACPowerW = httpapi.PtrOf[float64](req.MaxAcPowerW)
	}
	if req.PlaneID.Set {
		inv.PlaneID = httpapi.PtrOf[uuid.UUID](req.PlaneID)
		if err := h.checkPlane(ctx, inv); err != nil {
			return nil, err
		}
	}
	if v, ok := req.Color.Get(); ok {
		inv.Color = v
	}
	if v, ok := req.SortOrder.Get(); ok {
		inv.SortOrder = v
	}
	if inv, err = h.systems.UpdateInverter(ctx, inv); err != nil {
		return nil, err
	}
	out := toInverter(inv)
	return &out, nil
}

// checkPlane rejects a plane that is not one of the inverter's system.
func (h *Handler) checkPlane(ctx context.Context, inv *systems.Inverter) error {
	if inv.PlaneID == nil {
		return nil
	}
	sys, err := h.systems.GetSystem(ctx, inv.SystemID)
	if err != nil {
		return err
	}
	for _, p := range sys.Planes {
		if p.ID == *inv.PlaneID {
			return nil
		}
	}
	return httpapi.BadRequest("unknown plane " + inv.PlaneID.String())
}

func (h *Handler) DeleteInverter(ctx context.Context, params generated.DeleteInverterParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeInverterWrite); err != nil {
		return err
	}
	return h.systems.DeleteInverter(ctx, params.SystemId, params.InverterId)
}

func (h *Handler) UploadSystemImage(ctx context.Context, req generated.UploadSystemImageReq, params generated.UploadSystemImageParams) (*generated.System, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemWrite)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.SetImage(ctx, params.SystemId, req.Data)
	switch {
	case errors.Is(err, media.ErrInvalid):
		return nil, httpapi.BadRequest(err.Error())
	case errors.Is(err, systems.ErrUploadsDisabled):
		return nil, httpapi.NotFound("uploads are not enabled")
	case err != nil:
		return nil, err
	}
	return toSystem(sys, h.effectiveRole(ctx, p, sys.ID)), nil
}

func (h *Handler) DeleteSystemImage(ctx context.Context, params generated.DeleteSystemImageParams) (*generated.System, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeSystemWrite)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.ClearImage(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	return toSystem(sys, h.effectiveRole(ctx, p, sys.ID)), nil
}
