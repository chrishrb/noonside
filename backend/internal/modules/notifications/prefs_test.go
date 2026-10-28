package notifications

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestChannelsTargets(t *testing.T) {
	p := DefaultNotificationPrefs(uuid.New())
	ch, _, _ := Channels(p, TargetsSet{Ntfy: true, Webhook: true}, SeverityWarning, NotificationAlertOpened, summerNoon)
	assert.Equal(t, []string{ChannelEmail, ChannelPush, ChannelNtfy, ChannelWebhook}, ch)
	ch, _, _ = Channels(p, TargetsSet{Webhook: true}, SeverityWarning, NotificationAlertOpened, summerNoon)
	assert.Equal(t, []string{ChannelEmail, ChannelPush, ChannelWebhook}, ch, "no ntfy target")
	p.AlertChannels = []string{ChannelInApp, ChannelEmail, ChannelPush, ChannelNtfy}
	ch, _, _ = Channels(p, TargetsSet{Ntfy: true, Webhook: true}, SeverityWarning, NotificationAlertOpened, summerNoon)
	assert.Equal(t, []string{ChannelEmail, ChannelPush, ChannelNtfy}, ch, "switched off")
}

func TestChannels(t *testing.T) {
	now := summerNoon
	p := DefaultNotificationPrefs(uuid.New())
	ch, inApp, ok := Channels(p, TargetsSet{}, SeverityWarning, NotificationAlertOpened, now)
	assert.True(t, ok)
	assert.True(t, inApp)
	assert.Equal(t, []string{ChannelEmail, ChannelPush}, ch)

	_, _, ok = Channels(p, TargetsSet{}, SeverityInfo, NotificationAlertOpened, now)
	assert.False(t, ok, "below min severity")

	p.NotifyResolved = false
	_, _, ok = Channels(p, TargetsSet{}, SeverityError, NotificationAlertResolved, now)
	assert.False(t, ok)

	later := now.Add(time.Hour)
	p.MutedUntil = &later
	_, _, ok = Channels(p, TargetsSet{}, SeverityError, NotificationAlertOpened, now)
	assert.False(t, ok, "muted")

	p = DefaultNotificationPrefs(uuid.New())
	p.AlertChannels = []string{ChannelInApp}
	ch, inApp, ok = Channels(p, TargetsSet{}, SeverityError, NotificationAlertOpened, now)
	assert.True(t, ok, "in-app only")
	assert.True(t, inApp)
	assert.Empty(t, ch)
	p.AlertChannels = nil
	_, _, ok = Channels(p, TargetsSet{}, SeverityError, NotificationAlertOpened, now)
	assert.False(t, ok)
}

func TestDeliveries(t *testing.T) {
	all := TargetsSet{Ntfy: true, Webhook: true}
	ch, inApp := Deliveries([]string{ChannelWebhook, ChannelInApp, ChannelPush}, all)
	assert.True(t, inApp)
	assert.Equal(t, []string{ChannelPush, ChannelWebhook}, ch, "in display order, in_app is no delivery")
	ch, inApp = Deliveries([]string{ChannelNtfy, ChannelEmail, "carrier-pigeon"}, TargetsSet{Webhook: true})
	assert.False(t, inApp)
	assert.Equal(t, []string{ChannelEmail}, ch, "no ntfy target, unknown channel ignored")
	ch, inApp = Deliveries(nil, all)
	assert.False(t, inApp)
	assert.Empty(t, ch)
}
func TestRenderAnnouncement(t *testing.T) {
	n := Notification{
		Type: NotificationAnnouncement, Severity: SeverityInfo,
		Payload: map[string]any{
			"title": map[string]any{"en": "New: yield reports", "de": "Neu: Ertragsberichte"},
			"body":  map[string]any{"en": "Get your yield by email."},
			"link":  "/profile/notifications",
		},
	}
	m := RenderNotification("de", n)
	assert.Equal(t, "Neu: Ertragsberichte", m.Title)
	assert.Equal(t, "Get your yield by email.", m.Body, "falls back to English")
	assert.Equal(t, "/profile/notifications", Link(n))

	n.Payload["link"] = "//evil.example"
	assert.Equal(t, "/notifications", Link(n), "in-app paths only")

	n.Type = "some_future_type"
	n.Payload["title"] = "Plain"
	assert.Equal(t, "Plain", RenderNotification("en", n).Title, "unknown types render like announcements")
}

var summerNoon = time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

func TestLinkDataSourceFailing(t *testing.T) {
	sid, aid := uuid.New(), uuid.New()
	n := Notification{
		Type: NotificationAlertOpened, SystemID: &sid, AlertID: &aid,
		Payload: map[string]any{"kind": kindDataSourceFailing, "details": map[string]any{"source_kind": "solarlog_remote"}},
	}
	assert.Equal(t, "/s/"+sid.String()+"/settings/sources", Link(n))

	n.Payload["details"] = map[string]any{"source_kind": kindPVOutputExport}
	assert.Equal(t, "/s/"+sid.String()+"/settings/pvoutput", Link(n), "uploads live under integrations")

	delete(n.Payload, "details")
	assert.Equal(t, "/s/"+sid.String()+"/settings/sources", Link(n), "alerts from before source_kind")
}
