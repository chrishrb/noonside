package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/google/uuid"
)

// Mailer sends emails (implemented by email.Mailer).
type Mailer interface {
	Send(ctx context.Context, to string, e email.Email) error
	Enabled() bool
}

// Pusher sends web push messages (implemented by push.Client). gone means
// the subscription no longer exists.
type Pusher interface {
	Enabled() bool
	Send(ctx context.Context, sub push.Subscription, payload []byte, urgent bool) (gone bool, err error)
}

// TargetSender posts to a user's ntfy topic and webhook (implemented by
// notify.Client).
type TargetSender interface {
	Ntfy(ctx context.Context, topicURL, token string, m notify.Message) error
	Webhook(ctx context.Context, url, secret string, m notify.Message) error
}

// Opener decrypts sealed secrets (implemented by secretbox.Box).
type Opener interface {
	Open(sealed string) (string, error)
}

// PushPayload is what the service worker receives.
type PushPayload struct {
	// ID is the notification's id; opening the push marks it read.
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag,omitempty"`
	Badge int    `json:"badge"`
}

const (
	dispatchBatch = 100
	// claimLease hides claimed deliveries from other runs until they are
	// completed; a killed run's deliveries are retried after it.
	claimLease  = 5 * time.Minute
	maxAttempts = 5
)

// retryDelays by attempt (1-based) before giving up.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// MaxAttempts is how often a delivery is tried before it fails.
const MaxAttempts = maxAttempts

// RetryAt is when a delivery that failed on attempt (1-based) is retried.
func RetryAt(attempt int, now time.Time) time.Time {
	return now.Add(retryDelays[max(min(attempt, len(retryDelays)), 1)-1])
}

// DispatchStore is what the dispatcher reads and writes (implemented by
// Module).
type DispatchStore interface {
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
	// ClaimDeliveries takes up to limit due deliveries for lease; they are
	// retried after it unless completed.
	ClaimDeliveries(ctx context.Context, limit int, lease time.Duration) ([]Delivery, error)
	// CompleteDelivery records the outcome; retryAt nil means final.
	CompleteDelivery(ctx context.Context, id int64, status string, deliveryErr string, retryAt *time.Time) error
	ListPushSubscriptions(ctx context.Context, userID uuid.UUID) ([]PushSubscription, error)
	CountUnreadNotifications(ctx context.Context, userID uuid.UUID) (int, error)
	// RecordPush records a push result; gone deletes the subscription.
	RecordPush(ctx context.Context, id uuid.UUID, ok, gone bool) error
	// GetNotificationTargets returns the user's targets (empty if none).
	GetNotificationTargets(ctx context.Context, userID uuid.UUID) (*NotificationTargets, error)
}

type Dispatcher struct {
	store     DispatchStore
	mailer    Mailer
	pusher    Pusher
	publicURL string
	// report renders report notifications as their own email.
	report func(locale string, n Notification) (email.Email, error)
	// targets sends ntfy and webhook deliveries; box opens their secrets.
	targets TargetSender
	box     Opener
	now     func() time.Time
	logger  *slog.Logger
}

// NewDispatcher sends queued notifications. pusher may be nil.
func NewDispatcher(st DispatchStore, mailer Mailer, pusher Pusher, publicURL string) *Dispatcher {
	return &Dispatcher{
		store: st, mailer: mailer, pusher: pusher, publicURL: strings.TrimRight(publicURL, "/"),
		now: time.Now, logger: slog.Default().With("component", "notifications"),
	}
}

// WithReports sets how report notifications are rendered (reports module);
// without it their emails are skipped.
func (d *Dispatcher) WithReports(render func(locale string, n Notification) (email.Email, error)) *Dispatcher {
	d.report = render
	return d
}

// WithTargets enables ntfy and webhook deliveries; without it they are
// skipped. box may be nil if no target has a token or secret.
func (d *Dispatcher) WithTargets(s TargetSender, box Opener) *Dispatcher {
	d.targets, d.box = s, box
	return d
}

// Run sends due deliveries until none are left or deadline passes.
func (d *Dispatcher) Run(ctx context.Context, deadline time.Time) error {
	unlock, ok, err := d.store.TryLockJob(ctx, "dispatch", claimLease, 0)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	for d.now().Before(deadline) {
		batch, err := d.store.ClaimDeliveries(ctx, dispatchBatch, claimLease)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		d.process(ctx, batch)
	}
	return nil
}

func (d *Dispatcher) process(ctx context.Context, batch []Delivery) {
	// One email per user and run, however many notifications are due.
	emails := map[uuid.UUID][]Delivery{}
	var order []uuid.UUID
	for _, dl := range batch {
		switch dl.Channel {
		case ChannelEmail:
			if dl.Notification.Type == NotificationReport {
				d.complete(ctx, []Delivery{dl}, d.sendReport(ctx, dl))
				continue
			}
			uid := dl.Notification.UserID
			if _, ok := emails[uid]; !ok {
				order = append(order, uid)
			}
			emails[uid] = append(emails[uid], dl)
		case ChannelPush:
			d.complete(ctx, []Delivery{dl}, d.sendPush(ctx, dl))
		case ChannelNtfy, ChannelWebhook:
			d.complete(ctx, []Delivery{dl}, d.sendTarget(ctx, dl))
		}
	}
	for _, uid := range order {
		dls := emails[uid]
		d.complete(ctx, dls, d.sendEmail(ctx, dls))
	}
}

// outcome of sending: skipped (channel unavailable), or err (retry).
type outcome struct {
	skipped bool
	err     error
}

func (d *Dispatcher) complete(ctx context.Context, dls []Delivery, o outcome) {
	ctx = context.WithoutCancel(ctx)
	for _, dl := range dls {
		var err error
		switch {
		case o.skipped:
			err = d.store.CompleteDelivery(ctx, dl.ID, "skipped", "", nil)
		case o.err == nil:
			err = d.store.CompleteDelivery(ctx, dl.ID, "sent", "", nil)
		case dl.Attempts >= maxAttempts:
			d.logger.Warn("notification delivery failed", "channel", dl.Channel, "notification_id", dl.Notification.ID, "error", o.err)
			err = d.store.CompleteDelivery(ctx, dl.ID, "failed", o.err.Error(), nil)
		default:
			retry := RetryAt(dl.Attempts, d.now())
			err = d.store.CompleteDelivery(ctx, dl.ID, "pending", o.err.Error(), &retry)
		}
		if err != nil {
			d.logger.Error("record notification delivery", "delivery_id", dl.ID, "error", err)
		}
	}
}

// kindDataSourceFailing is alerting.KindDataSourceFailing: its alerts open
// the data source settings, or the PVOutput section of the integrations for
// an upload (datasources.KindPVOutputExport in details.source_kind).
const (
	kindDataSourceFailing = "data_source_failing"
	kindPVOutputExport    = "pvoutput_export"
)

// Link returns the app path a notification opens.
func Link(n Notification) string {
	if n.SystemID != nil && n.Type == NotificationForecast {
		return "/s/" + n.SystemID.String() + "/overview"
	}
	// In-app paths only; "//host" would leave the app.
	if l, _ := n.Payload["link"].(string); (n.Type == NotificationReport || n.Type == NotificationAnnouncement) &&
		strings.HasPrefix(l, "/") && !strings.HasPrefix(l, "//") {
		return l
	}
	if n.SystemID != nil && n.AlertID != nil {
		if k, _ := n.Payload["kind"].(string); k == kindDataSourceFailing {
			if d, _ := n.Payload["details"].(map[string]any); d["source_kind"] == kindPVOutputExport {
				return "/s/" + n.SystemID.String() + "/settings/pvoutput"
			}
			return "/s/" + n.SystemID.String() + "/settings/sources"
		}
		return "/s/" + n.SystemID.String() + "/status"
	}
	return "/notifications"
}

func (d *Dispatcher) sendEmail(ctx context.Context, dls []Delivery) outcome {
	if d.mailer == nil || !d.mailer.Enabled() {
		return outcome{skipped: true}
	}
	locale := dls[0].Locale
	e := email.Alerts{
		Heading:     fmt.Sprintf(Phrase(locale, "email_heading"), len(dls)),
		ButtonURL:   d.publicURL + "/notifications",
		ButtonLabel: Phrase(locale, "open_notifications"),
		ManageURL:   d.publicURL + "/profile/notifications",
		ManageLabel: Phrase(locale, "manage"),
		Footer:      Phrase(locale, "email_footer"),
	}
	for _, dl := range dls {
		m := RenderNotification(locale, dl.Notification)
		e.Items = append(e.Items, email.AlertItem{
			Title: m.Title, Body: m.Body, Severity: string(dl.Notification.Severity),
			URL: d.publicURL + Link(dl.Notification),
		})
	}
	if len(dls) == 1 {
		e.ButtonURL = e.Items[0].URL
	}
	return outcome{err: d.mailer.Send(ctx, dls[0].Email, e)}
}

// sendReport sends a yield report as its own email.
func (d *Dispatcher) sendReport(ctx context.Context, dl Delivery) outcome {
	if d.mailer == nil || !d.mailer.Enabled() || d.report == nil {
		return outcome{skipped: true}
	}
	e, err := d.report(dl.Locale, dl.Notification)
	if err != nil {
		return outcome{err: err}
	}
	return outcome{err: d.mailer.Send(ctx, dl.Email, e)}
}

func (d *Dispatcher) sendPush(ctx context.Context, dl Delivery) outcome {
	if d.pusher == nil || !d.pusher.Enabled() {
		return outcome{skipped: true}
	}
	n := dl.Notification
	subs, err := d.store.ListPushSubscriptions(ctx, n.UserID)
	if err != nil {
		return outcome{err: err}
	}
	if len(subs) == 0 {
		return outcome{skipped: true}
	}
	unread, err := d.store.CountUnreadNotifications(ctx, n.UserID)
	if err != nil {
		return outcome{err: err}
	}
	m := RenderNotification(dl.Locale, n)
	p := PushPayload{ID: n.ID.String(), Title: m.Title, Body: m.Body, URL: Link(n), Badge: unread}
	switch {
	case n.AlertID != nil:
		p.Tag = n.AlertID.String() // a later message about the alert replaces this one
	case n.Type == NotificationForecast && n.SystemID != nil:
		p.Tag = "forecast:" + n.SystemID.String()
	}
	body, err := json.Marshal(p)
	if err != nil {
		return outcome{err: err}
	}
	var lastErr error
	sent := 0
	for _, sub := range subs {
		gone, err := d.pusher.Send(ctx, sub.Push(), body, n.Severity == SeverityError)
		if rerr := d.store.RecordPush(context.WithoutCancel(ctx), sub.ID, err == nil, gone); rerr != nil {
			d.logger.Error("record push result", "error", rerr)
		}
		switch {
		case err == nil:
			sent++
		case gone:
		default:
			lastErr = err
		}
	}
	if sent == 0 && lastErr != nil {
		return outcome{err: lastErr}
	}
	return outcome{}
}

// Message renders a notification for ntfy and webhooks in the locale.
func (d *Dispatcher) Message(locale string, n Notification) notify.Message {
	return TargetMessage(d.publicURL, locale, n)
}

// TargetMessage renders a notification for ntfy and webhooks; publicURL is
// the app's base URL for the link.
func TargetMessage(publicURL, locale string, n Notification) notify.Message {
	m := RenderNotification(locale, n)
	out := notify.Message{
		ID: n.ID.String(), Type: n.Type, Severity: string(n.Severity), Title: m.Title, Body: m.Body,
		URL: strings.TrimRight(publicURL, "/") + Link(n), CreatedAt: n.CreatedAt,
	}
	if n.SystemID != nil {
		out.SystemID = n.SystemID.String()
	}
	if n.AlertID != nil {
		out.AlertID = n.AlertID.String()
		out.Kind, _ = n.Payload["kind"].(string)
	}
	return out
}

// sendTarget sends an ntfy or webhook delivery to the user's target;
// without one it is skipped.
func (d *Dispatcher) sendTarget(ctx context.Context, dl Delivery) outcome {
	n := dl.Notification
	if d.targets == nil {
		return outcome{skipped: true}
	}
	t, err := d.store.GetNotificationTargets(ctx, n.UserID)
	if err != nil {
		return outcome{err: err}
	}
	err = SendToTarget(ctx, d.targets, d.box, *t, dl.Channel, d.Message(dl.Locale, n))
	if errors.Is(err, ErrNoTarget) {
		return outcome{skipped: true} // removed since the delivery was queued
	}
	return outcome{err: err}
}

// ErrNoTarget means the user has no target for the channel.
var ErrNoTarget = errors.New("no target configured for this channel")

// SendToTarget sends m to the user's ntfy topic or webhook (channel),
// opening the token or secret with box.
func SendToTarget(ctx context.Context, s TargetSender, box Opener, t NotificationTargets, channel string, m notify.Message) error {
	open := func(sealed string) (string, error) {
		if sealed == "" {
			return "", nil
		}
		if box == nil {
			return "", errors.New("server has no secret key configured")
		}
		return box.Open(sealed)
	}
	switch channel {
	case ChannelNtfy:
		if t.NtfyURL == "" {
			return ErrNoTarget
		}
		token, err := open(t.NtfyTokenEnc)
		if err != nil {
			return err
		}
		return s.Ntfy(ctx, t.NtfyURL, token, m)
	case ChannelWebhook:
		if t.WebhookURL == "" {
			return ErrNoTarget
		}
		secret, err := open(t.WebhookSecretEnc)
		if err != nil {
			return err
		}
		return s.Webhook(ctx, t.WebhookURL, secret, m)
	}
	return fmt.Errorf("unknown channel %q", channel)
}

// SendTestPush sends a test message to all of the user's devices now and
// returns how many received it.
func SendTestPush(ctx context.Context, st DispatchStore, pusher Pusher, userID uuid.UUID, locale string) (int, error) {
	subs, err := st.ListPushSubscriptions(ctx, userID)
	if err != nil {
		return 0, err
	}
	unread, err := st.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(PushPayload{
		Title: Phrase(locale, "test_title"), Body: Phrase(locale, "test_body"),
		URL: "/notifications", Tag: "test", Badge: unread,
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, sub := range subs {
		gone, err := pusher.Send(ctx, sub.Push(), body, false)
		if rerr := st.RecordPush(context.WithoutCancel(ctx), sub.ID, err == nil, gone); rerr != nil {
			return sent, rerr
		}
		if err == nil {
			sent++
		}
	}
	return sent, nil
}
