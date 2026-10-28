// Package alertingapi serves the alerting module's API operations: a
// system's alerts and its rule settings.
package alertingapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/go-faster/jx"
	"github.com/google/uuid"
)

type Handler struct {
	alerting *alerting.Module
	systems  *systems.Module
	sources  *datasources.Module
	identity *identity.Module
	authz    httpapi.Authz
	now      func() time.Time
}

func New(a *alerting.Module, sys *systems.Module, ds *datasources.Module, id *identity.Module) *Handler {
	return &Handler{alerting: a, systems: sys, sources: ds, identity: id, authz: httpapi.Authz{Roles: sys}, now: time.Now}
}

func (h *Handler) locale(ctx context.Context, p *auth.Principal) string {
	return h.identity.Locale(ctx, p)
}

// --- alerts -----------------------------------------------------------------

func (h *Handler) ListAlerts(ctx context.Context, params generated.ListAlertsParams) (*generated.AlertPage, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeEventsRead)
	if err != nil {
		return nil, err
	}
	f := alerting.AlertFilter{
		OpenOnly: params.State.Or(generated.ListAlertsStateOpen) == generated.ListAlertsStateOpen,
		Limit:    params.Limit.Or(50),
	}
	if c, ok := params.Cursor.Get(); ok {
		t, err := time.Parse(time.RFC3339Nano, c)
		if err != nil {
			return nil, httpapi.BadRequest("invalid cursor")
		}
		f.Before = &t
	}
	as, err := h.alerting.ListAlerts(ctx, params.SystemId, f)
	if err != nil {
		return nil, err
	}
	r, err := h.alertRenderer(ctx, p, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := &generated.AlertPage{Items: make([]generated.Alert, len(as))}
	for i, a := range as {
		out.Items[i] = r.render(a)
	}
	if len(as) == f.Limit && len(as) > 0 {
		out.NextCursor = generated.NewOptString(as[len(as)-1].OpenedAt.Format(time.RFC3339Nano))
	}
	return out, nil
}

func (h *Handler) AckAlert(ctx context.Context, params generated.AckAlertParams) (*generated.Alert, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAlertsManage)
	if err != nil {
		return nil, err
	}
	a, err := h.alerting.AckAlert(ctx, params.SystemId, params.AlertId, p.UserID, h.now())
	if err != nil {
		return nil, err
	}
	return h.renderAlert(ctx, p, *a)
}

func (h *Handler) SnoozeAlert(ctx context.Context, req *generated.SnoozeAlertRequest, params generated.SnoozeAlertParams) (*generated.Alert, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAlertsManage)
	if err != nil {
		return nil, err
	}
	var until *time.Time
	if v, ok := req.Until.Get(); ok {
		until = &v
	}
	a, err := h.alerting.SnoozeAlert(ctx, params.SystemId, params.AlertId, until)
	if err != nil {
		return nil, err
	}
	return h.renderAlert(ctx, p, *a)
}

func (h *Handler) renderAlert(ctx context.Context, p *auth.Principal, a alerting.Alert) (*generated.Alert, error) {
	r, err := h.alertRenderer(ctx, p, a.SystemID)
	if err != nil {
		return nil, err
	}
	out := r.render(a)
	return &out, nil
}

type alertRenderer struct {
	locale    string
	sys       systems.System
	inverters []systems.Inverter
	sources   []datasources.DataSource
}

func (h *Handler) alertRenderer(ctx context.Context, p *auth.Principal, systemID uuid.UUID) (*alertRenderer, error) {
	sys, err := h.systems.GetSystem(ctx, systemID)
	if err != nil {
		return nil, err
	}
	invs, err := h.systems.ListInverters(ctx, systemID)
	if err != nil {
		return nil, err
	}
	dss, err := h.sources.ListDataSources(ctx, systemID)
	if err != nil {
		return nil, err
	}
	return &alertRenderer{locale: h.locale(ctx, p), sys: *sys, inverters: invs, sources: dss}, nil
}

func (r *alertRenderer) render(a alerting.Alert) generated.Alert {
	subject := alerting.SubjectName(a, r.sys, r.inverters, r.sources)
	m := alerting.RenderAlert(r.locale, a.Kind, subject, a.Details, r.sys.Loc())
	details := generated.AlertDetails{}
	for k, v := range a.Details {
		if b, err := json.Marshal(v); err == nil {
			details[k] = jx.Raw(b)
		}
	}
	return generated.Alert{
		ID: a.ID, Kind: a.Kind, Severity: generated.Severity(a.Severity), Title: m.Title, Body: m.Body,
		InverterID: httpapi.OptUUID(a.InverterID), DataSourceID: httpapi.OptUUID(a.DataSourceID), Details: details,
		OpenedAt: a.OpenedAt, LastSeenAt: a.LastSeenAt, ResolvedAt: httpapi.OptTime(a.ResolvedAt),
		AckedAt: httpapi.OptTime(a.AckedAt), AckedBy: httpapi.OptUUID(a.AckedBy), SnoozedUntil: httpapi.OptTime(a.SnoozedUntil),
	}
}

// --- rules ------------------------------------------------------------------

func (h *Handler) ListAlertRules(ctx context.Context, params generated.ListAlertRulesParams) ([]generated.AlertRule, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAlertsManage); err != nil {
		return nil, err
	}
	return h.alertRules(ctx, params.SystemId)
}

func (h *Handler) alertRules(ctx context.Context, systemID uuid.UUID) ([]generated.AlertRule, error) {
	overrides, err := h.alerting.ListAlertRules(ctx, systemID)
	if err != nil {
		return nil, err
	}
	rules := alerting.Effective(overrides)
	out := make([]generated.AlertRule, len(rules))
	for i, r := range rules {
		def, _ := alerting.Rule(r.Kind)
		out[i] = generated.AlertRule{
			Kind: r.Kind, Enabled: r.Enabled, Severity: generated.Severity(r.Severity),
			Params: generated.AlertRuleParams(r.Params), Defaults: generated.AlertRuleDefaults(def.Params),
		}
	}
	return out, nil
}

func (h *Handler) SetAlertRules(ctx context.Context, req []generated.AlertRuleUpdate, params generated.SetAlertRulesParams) ([]generated.AlertRule, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAlertsManage); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	rules := make([]alerting.AlertRule, 0, len(req))
	for _, r := range req {
		if seen[r.Kind] {
			return nil, httpapi.BadRequest("duplicate rule " + r.Kind)
		}
		seen[r.Kind] = true
		ps := map[string]float64(r.Params.Or(nil))
		if err := alerting.ValidateParams(r.Kind, ps); err != nil {
			return nil, httpapi.BadRequest(err.Error())
		}
		rules = append(rules, alerting.AlertRule{SystemID: params.SystemId, Kind: r.Kind, Enabled: r.Enabled, Params: ps})
	}
	if err := h.alerting.SetAlertRules(ctx, params.SystemId, rules); err != nil {
		return nil, err
	}
	return h.alertRules(ctx, params.SystemId)
}
