package notificationsapi

import (
	"context"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/google/uuid"
)

// --- inbox ------------------------------------------------------------------

func (h *Handler) ListNotifications(ctx context.Context, params generated.ListNotificationsParams) (*generated.NotificationPage, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	f := notifications.NotificationFilter{UnreadOnly: params.Unread.Or(false), Limit: params.Limit.Or(50)}
	if c, ok := params.Cursor.Get(); ok {
		ts, id, found := strings.Cut(c, "_")
		t, terr := time.Parse(time.RFC3339Nano, ts)
		u, uerr := uuid.Parse(id)
		if !found || terr != nil || uerr != nil {
			return nil, httpapi.BadRequest("invalid cursor")
		}
		f.BeforeTime, f.BeforeID = &t, &u
	}
	ns, err := h.notifications.ListNotifications(ctx, p.UserID, f)
	if err != nil {
		return nil, err
	}
	unread, err := h.notifications.CountUnreadNotifications(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	locale := h.locale(ctx, p)
	out := &generated.NotificationPage{Items: make([]generated.Notification, len(ns)), Unread: unread}
	for i, n := range ns {
		m := notifications.RenderNotification(locale, n)
		out.Items[i] = generated.Notification{
			ID: n.ID, Type: n.Type, Severity: generated.Severity(n.Severity),
			Title: m.Title, Body: m.Body, URL: notifications.Link(n),
			SystemID: httpapi.OptUUID(n.SystemID), AlertID: httpapi.OptUUID(n.AlertID),
			CreatedAt: n.CreatedAt, ReadAt: httpapi.OptTime(n.ReadAt),
		}
	}
	if len(ns) == f.Limit && len(ns) > 0 {
		last := ns[len(ns)-1]
		out.NextCursor = generated.NewOptString(last.CreatedAt.Format(time.RFC3339Nano) + "_" + last.ID.String())
	}
	return out, nil
}

func (h *Handler) GetUnreadNotificationCount(ctx context.Context) (*generated.UnreadCount, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	n, err := h.notifications.CountUnreadNotifications(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return &generated.UnreadCount{Count: n}, nil
}

func (h *Handler) MarkNotificationsRead(ctx context.Context, req *generated.MarkNotificationsReadRequest) (*generated.UnreadCount, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	if !req.All.Or(false) {
		if len(req.Ids) == 0 {
			return nil, httpapi.BadRequest("ids or all is required")
		}
		ids = req.Ids
	}
	if err := h.notifications.MarkNotificationsRead(ctx, p.UserID, ids, h.now()); err != nil {
		return nil, err
	}
	return h.GetUnreadNotificationCount(ctx)
}

func (h *Handler) DeleteNotifications(ctx context.Context, req *generated.DeleteNotificationsRequest) (*generated.UnreadCount, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	if !req.Read.Or(false) {
		if len(req.Ids) == 0 {
			return nil, httpapi.BadRequest("ids or read is required")
		}
		ids = req.Ids
	}
	if err := h.notifications.DeleteNotifications(ctx, p.UserID, ids); err != nil {
		return nil, err
	}
	return h.GetUnreadNotificationCount(ctx)
}

// --- preferences ------------------------------------------------------------

func toPref(p notifications.NotificationPrefs) generated.NotificationPreference {
	return generated.NotificationPreference{
		SystemID: httpapi.OptUUID(p.SystemID),
		Alerts: generated.NotificationPreferenceAlerts{
			Channels: toChannels(p.AlertChannels), MinSeverity: generated.Severity(p.MinSeverity), NotifyResolved: p.NotifyResolved,
		},
		Reports:    generated.NotificationPreferenceReports{Channels: toChannels(p.ReportChannels), Digest: generated.NotificationPreferenceReportsDigest(p.Digest)},
		Forecast:   generated.NotificationPreferenceForecast{Channels: toChannels(p.ForecastChannels), Enabled: p.ForecastPush},
		MutedUntil: httpapi.OptTime(p.MutedUntil),
	}
}

func fromPref(userID uuid.UUID, p generated.NotificationPreference) notifications.NotificationPrefs {
	return notifications.NotificationPrefs{
		UserID: userID, SystemID: httpapi.PtrOf[uuid.UUID](p.SystemID),
		AlertChannels: fromChannels(p.Alerts.Channels), MinSeverity: notifications.Severity(p.Alerts.MinSeverity), NotifyResolved: p.Alerts.NotifyResolved,
		Digest: string(p.Reports.Digest), ReportChannels: fromChannels(p.Reports.Channels),
		ForecastPush: p.Forecast.Enabled, ForecastChannels: fromChannels(p.Forecast.Channels),
		MutedUntil: httpapi.PtrOf[time.Time](p.MutedUntil),
	}
}

func toChannels(cs []string) generated.NotificationChannels {
	out := generated.NotificationChannels{}
	for _, c := range cs {
		out = append(out, generated.NotificationChannel(c))
	}
	return out
}

func fromChannels(cs generated.NotificationChannels) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, string(c))
	}
	return out
}

func (h *Handler) GetNotificationPreferences(ctx context.Context) (*generated.NotificationPreferences, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	prefs, err := h.notifications.ListNotificationPrefs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	// Only memberships, also for superadmins: alerts go to members.
	systems, err := h.systems.ListSystems(ctx, &p.UserID)
	if err != nil {
		return nil, err
	}
	out := &generated.NotificationPreferences{
		Default:        toPref(notifications.DefaultNotificationPrefs(p.UserID)),
		Systems:        []generated.NotificationPreference{},
		MemberSystems:  make([]generated.NotificationPreferencesMemberSystemsItem, len(systems)),
		EmailAvailable: h.mailer != nil && h.mailer.Enabled(),
		PushAvailable:  h.cfg.Pusher != nil && h.cfg.Pusher.Enabled(),
	}
	for i, sys := range systems {
		out.MemberSystems[i] = generated.NotificationPreferencesMemberSystemsItem{SystemID: sys.ID, Name: sys.Name}
	}
	for _, pr := range prefs {
		if pr.SystemID == nil {
			out.Default = toPref(pr)
		} else {
			out.Systems = append(out.Systems, toPref(pr))
		}
	}
	return out, nil
}

func (h *Handler) SetNotificationPreferences(ctx context.Context, req *generated.NotificationPreferencesUpdate) (*generated.NotificationPreferences, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if req.Default.SystemID.IsSet() {
		return nil, httpapi.BadRequest("the default must not have a system_id")
	}
	memberships, err := h.systems.ListMembershipsForUser(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	member := map[uuid.UUID]bool{}
	for _, m := range memberships {
		member[m.SystemID] = true
	}
	prefs := []notifications.NotificationPrefs{fromPref(p.UserID, req.Default)}
	seen := map[uuid.UUID]bool{}
	for _, sp := range req.Systems {
		id, ok := sp.SystemID.Get()
		if !ok {
			return nil, httpapi.BadRequest("system preferences need a system_id")
		}
		if !member[id] {
			return nil, httpapi.BadRequest("not a member of system " + id.String())
		}
		if seen[id] {
			return nil, httpapi.BadRequest("duplicate system " + id.String())
		}
		seen[id] = true
		prefs = append(prefs, fromPref(p.UserID, sp))
	}
	if err := h.notifications.SetNotificationPrefs(ctx, p.UserID, prefs); err != nil {
		return nil, err
	}
	return h.GetNotificationPreferences(ctx)
}

// --- push -------------------------------------------------------------------

func (h *Handler) GetPushConfig(ctx context.Context) (*generated.PushConfig, error) {
	if _, err := httpapi.CurrentUser(ctx); err != nil {
		return nil, err
	}
	if h.cfg.Pusher == nil || !h.cfg.Pusher.Enabled() {
		return &generated.PushConfig{Enabled: false}, nil
	}
	return &generated.PushConfig{Enabled: true, PublicKey: generated.NewOptString(h.cfg.VAPIDPublicKey)}, nil
}

func (h *Handler) SavePushSubscription(ctx context.Context, req *generated.PushSubscriptionRequest) error {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return err
	}
	// The server posts to the endpoint, so only known push services.
	if !push.AllowedEndpoint(req.Endpoint) {
		return httpapi.BadRequest("endpoint is not a known push service")
	}
	_, err = h.notifications.UpsertPushSubscription(ctx, &notifications.PushSubscription{
		UserID: p.UserID, Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth,
		UserAgent: req.UserAgent.Or(""),
	})
	return err
}

func (h *Handler) DeletePushSubscription(ctx context.Context, params generated.DeletePushSubscriptionParams) error {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return err
	}
	return h.notifications.DeletePushSubscription(ctx, p.UserID, params.Endpoint)
}

func (h *Handler) SendTestPush(ctx context.Context) (*generated.TestPushResult, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if h.cfg.Pusher == nil || !h.cfg.Pusher.Enabled() {
		return nil, httpapi.Conflict("push notifications are not configured on this server")
	}
	n, err := notifications.SendTestPush(ctx, h.notifications, h.cfg.Pusher, p.UserID, h.locale(ctx, p))
	if err != nil {
		return nil, err
	}
	return &generated.TestPushResult{Devices: n}, nil
}
