// Package notifications is the notifications module: every user's
// notifications (the in-app inbox), their preferences and own targets (ntfy,
// webhook), browser push subscriptions, and the outbox that delivers
// notifications by email, push, ntfy and webhook. Other modules create
// notifications (alerting, reports) and register how their types render. It
// owns the tables notifications, notification_deliveries,
// notification_prefs, notification_targets and push_subscriptions.
package notifications

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/google/uuid"
)

// Members lists a system's members with their profiles (implemented by the
// systems module).
type Members interface {
	ListMembers(ctx context.Context, systemID uuid.UUID) ([]systems.Member, error)
}

// Profiles reads user profiles (implemented by the identity module).
type Profiles interface {
	Profiles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]identity.Profile, error)
}

// Module is the notifications module.
type Module struct {
	db       *db.DB
	members  Members
	profiles Profiles
	now      func() time.Time
	logger   *slog.Logger
}

func New(d *db.DB, members Members, profiles Profiles) *Module {
	return &Module{
		db: d, members: members, profiles: profiles,
		now: time.Now, logger: slog.Default().With("component", "notifications"),
	}
}

// Severity orders alerts and notifications; users filter by a minimum.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

func (s Severity) Valid() bool { return s.rank() > 0 }

func (s Severity) rank() int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityError:
		return 3
	}
	return 0
}

// AtLeast reports whether s is min or more severe.
func (s Severity) AtLeast(min Severity) bool { return s.rank() >= min.rank() }

// Notification types.
const (
	NotificationAlertOpened   = "alert_opened"
	NotificationAlertResolved = "alert_resolved"
	NotificationAlertReminder = "alert_reminder"
	NotificationTest          = "test"
	// NotificationForecast is the morning push with the day's expected yield.
	NotificationForecast = "forecast"
	// NotificationReport is a periodic yield report.
	NotificationReport = "report"
	// NotificationAnnouncement is news for users (e.g. a new feature), not
	// tied to a system. Its payload holds the text per locale, see
	// RenderNotification.
	NotificationAnnouncement = "announcement"
)

// Channels. In-app is not a delivery: it only decides whether a
// notification counts as unread.
const (
	ChannelInApp   = "in_app"
	ChannelEmail   = "email"
	ChannelPush    = "push"
	ChannelNtfy    = "ntfy"
	ChannelWebhook = "webhook"
)

// Notification is one message to one user. Payload holds what is needed to
// render it (kind, subject name, details) in the reader's language.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	SystemID  *uuid.UUID
	AlertID   *uuid.UUID
	Type      string
	Severity  Severity
	Payload   map[string]any
	CreatedAt time.Time
	ReadAt    *time.Time
}

type NotificationFilter struct {
	UnreadOnly bool
	// Cursor: notifications created before (BeforeTime, BeforeID).
	BeforeTime *time.Time
	BeforeID   *uuid.UUID
	Limit      int
}

// NotificationPrefs are a user's settings; SystemID nil is the default.
// Each notification type has its own channels (Channel* constants).
type NotificationPrefs struct {
	UserID         uuid.UUID
	SystemID       *uuid.UUID
	AlertChannels  []string
	MinSeverity    Severity
	NotifyResolved bool
	// Digest is the yield report frequency ("none" = off).
	Digest         string
	ReportChannels []string
	// ForecastPush sends the day's expected yield in the morning (the name
	// is historical: it goes to ForecastChannels).
	ForecastPush     bool
	ForecastChannels []string
	MutedUntil       *time.Time
}

// AllChannels in display order.
var AllChannels = []string{ChannelInApp, ChannelEmail, ChannelPush, ChannelNtfy, ChannelWebhook}

// DefaultNotificationPrefs apply until a user saves their own. Keep them in
// sync with the column defaults.
func DefaultNotificationPrefs(userID uuid.UUID) NotificationPrefs {
	return NotificationPrefs{
		UserID: userID, AlertChannels: slices.Clone(AllChannels),
		MinSeverity: SeverityWarning, NotifyResolved: true,
		Digest: "none", ReportChannels: []string{ChannelEmail},
		ForecastChannels: []string{ChannelPush},
	}
}

// Recipient is a system member who may receive the system's alerts, with
// their effective preferences for it.
type Recipient struct {
	UserID uuid.UUID
	Email  string
	Name   string
	Locale string
	Role   auth.Role
	Prefs  NotificationPrefs
	// Targets says which of the user's ntfy/webhook targets are set.
	Targets TargetsSet
}

// TargetsSet says which notification targets a user has configured.
type TargetsSet struct {
	Ntfy, Webhook bool
}

// NotificationTargets are a user's own delivery targets. The token and
// secret are sealed with secretbox by the caller.
type NotificationTargets struct {
	UserID           uuid.UUID
	NtfyURL          string
	NtfyTokenEnc     string
	WebhookURL       string
	WebhookSecretEnc string
	UpdatedAt        time.Time
}

// Set reports which targets are configured.
func (t NotificationTargets) Set() TargetsSet {
	return TargetsSet{Ntfy: t.NtfyURL != "", Webhook: t.WebhookURL != ""}
}

// Delivery is a pending email or push of a notification.
type Delivery struct {
	ID           int64
	Channel      string
	Attempts     int
	Notification Notification
	Email        string
	Name         string
	Locale       string
}

type PushSubscription struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Endpoint      string
	P256dh        string
	Auth          string
	UserAgent     string
	CreatedAt     time.Time
	LastSuccessAt *time.Time
}

// Push returns where the push service delivers.
func (s PushSubscription) Push() push.Subscription {
	return push.Subscription{Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth}
}

// Channels returns the delivery channels an alert notification goes to
// under prefs, whether it counts as unread in the app, and false if the user
// gets no notification at all.
func Channels(p NotificationPrefs, targets TargetsSet, sev Severity, typ string, now time.Time) ([]string, bool, bool) {
	if Muted(p, now) {
		return nil, false, false
	}
	if !sev.AtLeast(p.MinSeverity) {
		return nil, false, false
	}
	if typ == NotificationAlertResolved && !p.NotifyResolved {
		return nil, false, false
	}
	ch, inApp := Deliveries(p.AlertChannels, targets)
	return ch, inApp, inApp || len(ch) > 0
}

// Deliveries splits a type's chosen channels into delivery channels (ntfy
// and webhook only if the user set that target) and whether the
// notification counts as unread in the app.
func Deliveries(channels []string, targets TargetsSet) (deliveries []string, inApp bool) {
	for _, c := range AllChannels {
		if !slices.Contains(channels, c) {
			continue
		}
		switch {
		case c == ChannelInApp:
			inApp = true
		case c == ChannelNtfy && !targets.Ntfy, c == ChannelWebhook && !targets.Webhook:
		default:
			deliveries = append(deliveries, c)
		}
	}
	return deliveries, inApp
}

// Muted reports whether the prefs mute notifications at now.
func Muted(p NotificationPrefs, now time.Time) bool {
	return p.MutedUntil != nil && p.MutedUntil.After(now)
}
