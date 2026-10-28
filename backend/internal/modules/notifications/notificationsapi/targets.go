package notificationsapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

func toNotificationTargets(t *notifications.NotificationTargets) *generated.NotificationTargets {
	return &generated.NotificationTargets{
		NtfyURL: t.NtfyURL, NtfyHasToken: t.NtfyTokenEnc != "",
		WebhookURL: t.WebhookURL, WebhookHasSecret: t.WebhookSecretEnc != "",
	}
}

func (h *Handler) GetNotificationTargets(ctx context.Context) (*generated.NotificationTargets, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.notifications.GetNotificationTargets(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return toNotificationTargets(t), nil
}

func (h *Handler) SetNotificationTargets(ctx context.Context, req *generated.NotificationTargetsUpdate) (*generated.NotificationTargets, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.notifications.GetNotificationTargets(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	checkURL := func(field, raw string) error {
		if h.cfg.Notify == nil {
			return httpapi.Conflict("ntfy and webhooks are not available on this server")
		}
		if err := h.cfg.Notify.CheckURL(raw); err != nil {
			return httpapi.BadRequest(field + ": " + err.Error())
		}
		return nil
	}
	seal := func(field, v string) (string, error) {
		if v == "" {
			return "", nil
		}
		if h.cfg.Box == nil {
			return "", httpapi.BadRequest(field + ": server has no secret key configured; cannot store secrets")
		}
		return h.cfg.Box.Seal(v)
	}
	if v, ok := req.NtfyURL.Get(); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			if err := checkURL("ntfy_url", v); err != nil {
				return nil, err
			}
			if u, _ := url.Parse(v); strings.Trim(u.Path, "/") == "" {
				return nil, httpapi.BadRequest("ntfy_url: add the topic, e.g. https://ntfy.sh/my-topic")
			}
		} else {
			t.NtfyTokenEnc = ""
		}
		t.NtfyURL = v
	}
	if v, ok := req.NtfyToken.Get(); ok {
		if t.NtfyTokenEnc, err = seal("ntfy_token", strings.TrimSpace(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := req.WebhookURL.Get(); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			if err := checkURL("webhook_url", v); err != nil {
				return nil, err
			}
		} else {
			t.WebhookSecretEnc = ""
		}
		t.WebhookURL = v
	}
	if v, ok := req.WebhookSecret.Get(); ok {
		if t.WebhookSecretEnc, err = seal("webhook_secret", v); err != nil {
			return nil, err
		}
	}
	if t.NtfyURL == "" && t.NtfyTokenEnc != "" {
		return nil, httpapi.BadRequest("ntfy_token: set the topic URL first")
	}
	if t.WebhookURL == "" && t.WebhookSecretEnc != "" {
		return nil, httpapi.BadRequest("webhook_secret: set the webhook URL first")
	}
	if err := h.notifications.SetNotificationTargets(ctx, t); err != nil {
		return nil, err
	}
	return toNotificationTargets(t), nil
}

func (h *Handler) SendTestToTarget(ctx context.Context, params generated.SendTestToTargetParams) (*generated.TestTargetResult, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if h.cfg.Notify == nil {
		return nil, httpapi.Conflict("ntfy and webhooks are not available on this server")
	}
	t, err := h.notifications.GetNotificationTargets(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	channel := string(params.Channel)
	if (channel == notifications.ChannelNtfy && t.NtfyURL == "") || (channel == notifications.ChannelWebhook && t.WebhookURL == "") {
		return nil, httpapi.Conflict("save a " + channel + " target first")
	}
	locale := h.locale(ctx, p)
	n := notifications.Notification{ID: uuid.New(), UserID: p.UserID, Type: notifications.NotificationTest, Severity: notifications.SeverityInfo, CreatedAt: h.now()}
	m := notifications.TargetMessage(h.cfg.PublicURL, locale, n)
	m.Body = notifications.Phrase(locale, "test_target_body")
	var box notifications.Opener
	if h.cfg.Box != nil {
		box = h.cfg.Box
	}
	if err := notifications.SendToTarget(ctx, h.cfg.Notify, box, *t, channel, m); err != nil {
		return &generated.TestTargetResult{Ok: false, Error: generated.NewOptString(err.Error())}, nil
	}
	return &generated.TestTargetResult{Ok: true}, nil
}
