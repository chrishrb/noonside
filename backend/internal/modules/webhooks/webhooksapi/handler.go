// Package webhooksapi serves the webhooks module's API operations: a
// system's webhooks, test posts and their recent deliveries.
package webhooksapi

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/webhooks"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
)

// Notifier posts webhooks (implemented by notify.Client).
type Notifier interface {
	webhooks.Poster
	// CheckURL validates a target URL (https and, unless allowed, public).
	CheckURL(raw string) error
}

type Config struct {
	// PublicURL is the app's base URL for links in payloads.
	PublicURL string
	// Notify posts the webhooks (nil: webhooks are not available).
	Notify Notifier
	// Box seals the signing secrets (nil: secrets cannot be set).
	Box *secretbox.Box
}

type Handler struct {
	webhooks *webhooks.Module
	systems  *systems.Module
	identity *identity.Module
	authz    httpapi.Authz
	cfg      Config
	now      func() time.Time
}

func New(w *webhooks.Module, sys *systems.Module, id *identity.Module, cfg Config) *Handler {
	return &Handler{webhooks: w, systems: sys, identity: id, authz: httpapi.Authz{Roles: sys}, cfg: cfg, now: time.Now}
}

// --- webhooks ----------------------------------------------------------------

func toWebhook(w *webhooks.Webhook) generated.Webhook {
	out := generated.Webhook{
		ID: w.ID, Name: w.Name, URL: w.URL, Enabled: w.Enabled, HasSecret: w.SecretEnc != "",
		Locale: generated.Locale(w.Locale), CreatedAt: w.CreatedAt, Events: make([]generated.WebhookEvent, len(w.Events)),
	}
	for i, e := range w.Events {
		out.Events[i] = generated.WebhookEvent(e)
	}
	if d := w.LastDelivery; d != nil {
		at := d.CreatedAt
		if d.SentAt != nil {
			at = *d.SentAt
		}
		out.LastDeliveryAt = generated.NewOptDateTime(at)
		out.LastStatus = generated.NewOptWebhookLastStatus(generated.WebhookLastStatus(d.Status))
		if d.LastError != "" {
			out.LastError = generated.NewOptString(d.LastError)
		}
	}
	return out
}

func webhookEvents(in []generated.WebhookEvent) ([]string, error) {
	out := []string{}
	for _, e := range in {
		if !slices.Contains(out, string(e)) {
			out = append(out, string(e))
		}
	}
	if len(out) == 0 {
		return nil, httpapi.BadRequest("events: choose at least one event")
	}
	return out, nil
}

// checkWebhookURL validates a webhook URL as the sender will use it.
func (h *Handler) checkWebhookURL(raw string) (string, error) {
	if h.cfg.Notify == nil {
		return "", httpapi.Conflict("webhooks are not available on this server")
	}
	raw = strings.TrimSpace(raw)
	if err := h.cfg.Notify.CheckURL(raw); err != nil {
		return "", httpapi.BadRequest("url: " + err.Error())
	}
	return raw, nil
}

func (h *Handler) ListWebhooks(ctx context.Context, params generated.ListWebhooksParams) ([]generated.Webhook, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage); err != nil {
		return nil, err
	}
	ws, err := h.webhooks.ListWebhooks(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.Webhook, len(ws))
	for i := range ws {
		out[i] = toWebhook(&ws[i])
	}
	return out, nil
}

func (h *Handler) CreateWebhook(ctx context.Context, req *generated.WebhookCreate, params generated.CreateWebhookParams) (*generated.Webhook, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage)
	if err != nil {
		return nil, err
	}
	u, err := h.checkWebhookURL(req.URL)
	if err != nil {
		return nil, err
	}
	events, err := webhookEvents(req.Events)
	if err != nil {
		return nil, err
	}
	w := &webhooks.Webhook{
		SystemID: params.SystemId, Name: strings.TrimSpace(req.Name), URL: u, Events: events,
		Enabled: req.Enabled.Or(true), Locale: string(req.Locale.Or(generated.Locale(h.locale(ctx, p)))),
	}
	if w.Name == "" {
		return nil, httpapi.BadRequest("name is required")
	}
	if p.IsUser() {
		w.CreatedBy = &p.UserID
	}
	if w.SecretEnc, err = h.seal(req.Secret.Or(""), "secret"); err != nil {
		return nil, err
	}
	if w, err = h.webhooks.CreateWebhook(ctx, w); err != nil {
		return nil, err
	}
	out := toWebhook(w)
	return &out, nil
}

func (h *Handler) UpdateWebhook(ctx context.Context, req *generated.WebhookUpdate, params generated.UpdateWebhookParams) (*generated.Webhook, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage); err != nil {
		return nil, err
	}
	w, err := h.webhooks.GetWebhook(ctx, params.SystemId, params.WebhookId)
	if err != nil {
		return nil, err
	}
	if v, ok := req.Name.Get(); ok {
		if w.Name = strings.TrimSpace(v); w.Name == "" {
			return nil, httpapi.BadRequest("name is required")
		}
	}
	if v, ok := req.URL.Get(); ok {
		if w.URL, err = h.checkWebhookURL(v); err != nil {
			return nil, err
		}
	}
	if req.Events != nil {
		if w.Events, err = webhookEvents(req.Events); err != nil {
			return nil, err
		}
	}
	if v, ok := req.Enabled.Get(); ok {
		w.Enabled = v
	}
	if v, ok := req.Locale.Get(); ok {
		w.Locale = string(v)
	}
	if v, ok := req.Secret.Get(); ok {
		if w.SecretEnc, err = h.seal(v, "secret"); err != nil {
			return nil, err
		}
	}
	if w, err = h.webhooks.UpdateWebhook(ctx, w); err != nil {
		return nil, err
	}
	out := toWebhook(w)
	return &out, nil
}

func (h *Handler) DeleteWebhook(ctx context.Context, params generated.DeleteWebhookParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage); err != nil {
		return err
	}
	return h.webhooks.DeleteWebhook(ctx, params.SystemId, params.WebhookId)
}

func (h *Handler) TestWebhook(ctx context.Context, params generated.TestWebhookParams) (*generated.TestTargetResult, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage); err != nil {
		return nil, err
	}
	if h.cfg.Notify == nil {
		return nil, httpapi.Conflict("webhooks are not available on this server")
	}
	w, err := h.webhooks.GetWebhook(ctx, params.SystemId, params.WebhookId)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	var box webhooks.Opener
	if h.cfg.Box != nil { // a nil *secretbox.Box must stay a nil interface
		box = h.cfg.Box
	}
	if err := webhooks.Send(ctx, h.cfg.Notify, box, *w, webhooks.TestPayload(h.cfg.PublicURL, sys, h.now())); err != nil {
		return &generated.TestTargetResult{Ok: false, Error: generated.NewOptString(err.Error())}, nil
	}
	return &generated.TestTargetResult{Ok: true}, nil
}

// webhookDeliveriesShown is how many deliveries the log lists.
const webhookDeliveriesShown = 20

func (h *Handler) ListWebhookDeliveries(ctx context.Context, params generated.ListWebhookDeliveriesParams) ([]generated.WebhookDelivery, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeWebhookManage); err != nil {
		return nil, err
	}
	// Scoped to the system: a webhook of another system is not found.
	if _, err := h.webhooks.GetWebhook(ctx, params.SystemId, params.WebhookId); err != nil {
		return nil, err
	}
	ds, err := h.webhooks.ListWebhookDeliveries(ctx, params.WebhookId, webhookDeliveriesShown)
	if err != nil {
		return nil, err
	}
	out := make([]generated.WebhookDelivery, len(ds))
	for i, d := range ds {
		out[i] = generated.WebhookDelivery{
			ID: d.ID, EventID: d.EventID, Event: d.Event, Status: generated.WebhookDeliveryStatus(d.Status),
			Attempts: d.Attempts, CreatedAt: d.CreatedAt, SentAt: httpapi.OptTime(d.SentAt), LastError: d.LastError,
		}
		if d.Status == "pending" {
			out[i].NextAttemptAt = generated.NewOptDateTime(d.NextAttemptAt)
		}
	}
	return out, nil
}

func (h *Handler) locale(ctx context.Context, p *auth.Principal) string {
	return h.identity.Locale(ctx, p)
}

func (h *Handler) seal(v, field string) (string, error) {
	if v == "" {
		return "", nil
	}
	if h.cfg.Box == nil {
		return "", httpapi.BadRequest(field + ": server has no secret key configured; cannot store secrets")
	}
	return h.cfg.Box.Seal(v)
}
