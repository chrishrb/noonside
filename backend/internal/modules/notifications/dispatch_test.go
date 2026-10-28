package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completed struct {
	status  string
	errMsg  string
	retryAt *time.Time
}

type fakeStore struct {
	DispatchStore
	deliveries []Delivery
	done       map[int64]completed
	subs       []PushSubscription
	pushes     map[uuid.UUID][2]bool // ok, gone
	targets    map[uuid.UUID]NotificationTargets
}

func (f *fakeStore) GetNotificationTargets(_ context.Context, id uuid.UUID) (*NotificationTargets, error) {
	t := f.targets[id]
	return &t, nil
}

type sentTarget struct {
	channel, url, secret string
	msg                  notify.Message
}

type fakeTargets struct {
	sent []sentTarget
	err  error
}

func (f *fakeTargets) Ntfy(_ context.Context, url, token string, m notify.Message) error {
	f.sent = append(f.sent, sentTarget{"ntfy", url, token, m})
	return f.err
}

func (f *fakeTargets) Webhook(_ context.Context, url, secret string, m notify.Message) error {
	f.sent = append(f.sent, sentTarget{"webhook", url, secret, m})
	return f.err
}

type fakeBox struct{}

func (fakeBox) Open(s string) (string, error) { return "open:" + s, nil }

func (f *fakeStore) TryLockJob(context.Context, string, time.Duration, time.Duration) (func(), bool, error) {
	return func() {}, true, nil
}

func (f *fakeStore) ClaimDeliveries(context.Context, int, time.Duration) ([]Delivery, error) {
	out := f.deliveries
	f.deliveries = nil
	return out, nil
}

func (f *fakeStore) CompleteDelivery(_ context.Context, id int64, status, msg string, retryAt *time.Time) error {
	f.done[id] = completed{status, msg, retryAt}
	return nil
}

func (f *fakeStore) ListPushSubscriptions(context.Context, uuid.UUID) ([]PushSubscription, error) {
	return f.subs, nil
}

func (f *fakeStore) CountUnreadNotifications(context.Context, uuid.UUID) (int, error) { return 3, nil }

func (f *fakeStore) RecordPush(_ context.Context, id uuid.UUID, ok, gone bool) error {
	f.pushes[id] = [2]bool{ok, gone}
	return nil
}

type fakeMailer struct {
	sent []email.Alerts
	err  error
}

func (m *fakeMailer) Enabled() bool { return true }
func (m *fakeMailer) Send(_ context.Context, _ string, e email.Email) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, e.(email.Alerts))
	return nil
}

type fakePusher struct {
	payloads [][]byte
	gone     map[string]bool
}

func (p *fakePusher) Enabled() bool { return true }
func (p *fakePusher) Send(_ context.Context, sub push.Subscription, payload []byte, _ bool) (bool, error) {
	if p.gone[sub.Endpoint] {
		return true, errors.New("gone")
	}
	p.payloads = append(p.payloads, payload)
	return false, nil
}

func delivery(id int64, user uuid.UUID, channel string, attempts int) Delivery {
	sys, alert := uuid.New(), uuid.New()
	return Delivery{
		ID: id, Channel: channel, Attempts: attempts, Email: "a@example.com", Locale: "en",
		Notification: Notification{
			ID: uuid.New(), UserID: user, SystemID: &sys, AlertID: &alert,
			Type: NotificationAlertOpened, Severity: SeverityWarning,
			Payload: map[string]any{"kind": kindNoData, "system_name": "Home", "subject": "Home", "details": map[string]any{"minutes": float64(90)}},
		},
	}
}

func newDispatcher(st *fakeStore, m Mailer, p Pusher) *Dispatcher {
	st.done = map[int64]completed{}
	st.pushes = map[uuid.UUID][2]bool{}
	return NewDispatcher(st, m, p, "https://solar.example/")
}

func TestDispatchBatchesEmailsPerUser(t *testing.T) {
	u1, u2 := uuid.New(), uuid.New()
	st := &fakeStore{deliveries: []Delivery{
		delivery(1, u1, ChannelEmail, 1), delivery(2, u1, ChannelEmail, 1), delivery(3, u2, ChannelEmail, 1),
	}}
	m := &fakeMailer{}
	require.NoError(t, newDispatcher(st, m, nil).Run(context.Background(), time.Now().Add(time.Minute)))

	require.Len(t, m.sent, 2)
	assert.Len(t, m.sent[0].Items, 2)
	assert.Equal(t, "2 new notifications", m.sent[0].Subject())
	assert.Equal(t, "https://solar.example/notifications", m.sent[0].ButtonURL)
	assert.Equal(t, "No data from Home", m.sent[1].Subject())
	assert.Contains(t, m.sent[1].ButtonURL, "https://solar.example/s/")
	for id := int64(1); id <= 3; id++ {
		assert.Equal(t, "sent", st.done[id].status)
	}
}

func TestDispatchRetriesThenFails(t *testing.T) {
	u := uuid.New()
	st := &fakeStore{deliveries: []Delivery{delivery(1, u, ChannelEmail, 1), delivery(2, uuid.New(), ChannelEmail, maxAttempts)}}
	d := newDispatcher(st, &fakeMailer{err: errors.New("smtp down")}, nil)
	require.NoError(t, d.Run(context.Background(), time.Now().Add(time.Minute)))

	assert.Equal(t, "pending", st.done[1].status)
	require.NotNil(t, st.done[1].retryAt)
	assert.Equal(t, "smtp down", st.done[1].errMsg)
	assert.Equal(t, "failed", st.done[2].status)
}

func TestDispatchPush(t *testing.T) {
	u := uuid.New()
	live, dead := PushSubscription{ID: uuid.New(), Endpoint: "https://fcm.googleapis.com/a"}, PushSubscription{ID: uuid.New(), Endpoint: "https://fcm.googleapis.com/b"}
	st := &fakeStore{deliveries: []Delivery{delivery(1, u, ChannelPush, 1)}, subs: []PushSubscription{live, dead}}
	p := &fakePusher{gone: map[string]bool{dead.Endpoint: true}}
	require.NoError(t, newDispatcher(st, nil, p).Run(context.Background(), time.Now().Add(time.Minute)))

	assert.Equal(t, "sent", st.done[1].status)
	assert.Equal(t, [2]bool{true, false}, st.pushes[live.ID])
	assert.Equal(t, [2]bool{false, true}, st.pushes[dead.ID], "gone subscriptions are dropped")
	require.Len(t, p.payloads, 1)
	var payload PushPayload
	require.NoError(t, json.Unmarshal(p.payloads[0], &payload))
	assert.Equal(t, "No data from Home", payload.Title)
	assert.Equal(t, 3, payload.Badge)
	assert.NotEmpty(t, payload.Tag)
}

func TestDispatchSkipsUnavailableChannels(t *testing.T) {
	st := &fakeStore{deliveries: []Delivery{delivery(1, uuid.New(), ChannelPush, 1), delivery(2, uuid.New(), ChannelEmail, 1)}}
	require.NoError(t, newDispatcher(st, nil, &fakePusher{}).Run(context.Background(), time.Now().Add(time.Minute)))
	assert.Equal(t, "skipped", st.done[1].status, "no subscriptions")
	assert.Equal(t, "skipped", st.done[2].status, "no mailer")
}

func TestDispatchTargets(t *testing.T) {
	withTargets, without := uuid.New(), uuid.New()
	st := &fakeStore{
		deliveries: []Delivery{
			delivery(1, withTargets, ChannelNtfy, 1), delivery(2, withTargets, ChannelWebhook, 1),
			delivery(3, without, ChannelWebhook, 1),
		},
		targets: map[uuid.UUID]NotificationTargets{withTargets: {
			NtfyURL: "https://ntfy.sh/t", NtfyTokenEnc: "tok", WebhookURL: "https://hook.example/x", WebhookSecretEnc: "sec",
		}},
	}
	tg := &fakeTargets{}
	d := newDispatcher(st, nil, nil).WithTargets(tg, fakeBox{})
	require.NoError(t, d.Run(context.Background(), time.Now().Add(time.Minute)))

	require.Len(t, tg.sent, 2)
	assert.Equal(t, sentTarget{"ntfy", "https://ntfy.sh/t", "open:tok", tg.sent[0].msg}, tg.sent[0])
	assert.Equal(t, "webhook", tg.sent[1].channel)
	assert.Equal(t, "open:sec", tg.sent[1].secret)
	m := tg.sent[0].msg
	assert.Equal(t, "No data from Home", m.Title)
	assert.Equal(t, "warning", m.Severity)
	assert.Equal(t, kindNoData, m.Kind)
	assert.Contains(t, m.URL, "https://solar.example/s/")
	assert.Equal(t, "sent", st.done[1].status)
	assert.Equal(t, "sent", st.done[2].status)
	assert.Equal(t, "skipped", st.done[3].status, "no target")

	t.Run("errors are retried", func(t *testing.T) {
		st.deliveries = []Delivery{delivery(4, withTargets, ChannelWebhook, 1)}
		tg.err = errors.New("502 Bad Gateway")
		require.NoError(t, d.Run(context.Background(), time.Now().Add(time.Minute)))
		assert.Equal(t, "pending", st.done[4].status)
		assert.Equal(t, "502 Bad Gateway", st.done[4].errMsg)
	})
	t.Run("skipped without a sender", func(t *testing.T) {
		st.deliveries = []Delivery{delivery(5, withTargets, ChannelNtfy, 1)}
		require.NoError(t, newDispatcher(st, nil, nil).Run(context.Background(), time.Now().Add(time.Minute)))
		assert.Equal(t, "skipped", st.done[5].status)
	})
}

// kindNoData is an alert kind (alerting.KindNoData); alerting renders its
// notifications, here a stand-in does.
const kindNoData = "no_data"

func init() {
	RegisterRenderer(NotificationAlertOpened, func(_ string, n Notification) Message {
		subject, _ := n.Payload["subject"].(string)
		return Message{Title: "No data from " + subject}
	})
}
