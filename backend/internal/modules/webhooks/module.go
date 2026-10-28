package webhooks

import (
	"context"
	"slices"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// The webhooks module posts a system's events to the URLs its admins set.
// It owns the tables webhooks and webhook_deliveries (the outbox).

// Module holds the webhooks and their outbox.
type Module struct {
	db *db.DB
}

func New(d *db.DB) *Module { return &Module{db: d} }

// Webhook events.
const (
	EventAlertOpened     = "alert.opened"
	EventAlertResolved   = "alert.resolved"
	EventDailySummary    = "daily.summary"
	EventSurplusForecast = "surplus.forecast"
	// EventTest is only sent by the test button.
	EventTest = "test"
)

// WebhookEvents are the events a webhook can subscribe to.
var WebhookEvents = []string{EventAlertOpened, EventAlertResolved, EventDailySummary, EventSurplusForecast}

// Webhook posts a system's events to a URL. The signing secret is sealed.
type Webhook struct {
	ID        uuid.UUID
	SystemID  uuid.UUID
	Name      string
	URL       string
	SecretEnc string
	Events    []string
	Locale    string
	Enabled   bool
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
	// LastDelivery is the newest delivery, if any (listings only).
	LastDelivery *WebhookDelivery
}

// Wants reports whether the webhook is enabled for event.
func (w *Webhook) Wants(event string) bool { return w.Enabled && slices.Contains(w.Events, event) }

// WebhookDelivery is one queued or sent webhook request.
type WebhookDelivery struct {
	ID            int64
	EventID       uuid.UUID
	WebhookID     uuid.UUID
	Event         string
	DedupeKey     string
	Payload       map[string]any
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        *time.Time
	// Webhook is set on claimed deliveries.
	Webhook *Webhook
}

// Store is what the service and the sender read and write.
type Store interface {
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
	ListInverters(ctx context.Context, systemID uuid.UUID) ([]systems.Inverter, error)
	Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error)
	PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error)

	// ListWebhooks returns the system's webhooks with their newest delivery.
	ListWebhooks(ctx context.Context, systemID uuid.UUID) ([]Webhook, error)
	// ListWebhooksForEvent returns the enabled webhooks of all systems that
	// subscribe to event.
	ListWebhooksForEvent(ctx context.Context, event string) ([]Webhook, error)
	// QueueWebhookDelivery queues d unless one with the same webhook, event
	// and dedupe key exists; it reports whether it queued.
	QueueWebhookDelivery(ctx context.Context, d *WebhookDelivery) (bool, error)
	// ClaimWebhookDeliveries takes up to limit due deliveries (with their
	// webhook) for lease; they are retried after it unless completed.
	ClaimWebhookDeliveries(ctx context.Context, limit int, lease time.Duration) ([]WebhookDelivery, error)
	// CompleteWebhookDelivery records the outcome; retryAt nil means final.
	CompleteWebhookDelivery(ctx context.Context, id int64, status, deliveryErr string, retryAt *time.Time) error
	// PruneWebhookDeliveries deletes finished deliveries created before the
	// instant.
	PruneWebhookDeliveries(ctx context.Context, before time.Time) (int64, error)
}

// ModuleStore implements Store with the modules.
type ModuleStore struct {
	*Module
	Systems   *systems.Module
	Telemetry *telemetry.Module
}

func (s ModuleStore) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	return s.db.TryLockJob(ctx, name, ttl, every)
}

func (s ModuleStore) GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error) {
	return s.Systems.GetSystem(ctx, id)
}

func (s ModuleStore) ListInverters(ctx context.Context, systemID uuid.UUID) ([]systems.Inverter, error) {
	return s.Systems.ListInverters(ctx, systemID)
}

func (s ModuleStore) Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error) {
	return s.Telemetry.Energy(ctx, systemID, granularity, from, to)
}

func (s ModuleStore) PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error) {
	return s.Telemetry.PowerCurve(ctx, systemID, from, to, bucket)
}

// Job queues the scheduled events (daily summary, surplus forecast).
func (s *Service) Job() scheduler.Job { return scheduler.Job{Name: "webhook_events", Run: s.Run} }

// Job posts the queued deliveries.
func (s *Sender) Job() scheduler.Job { return scheduler.Job{Name: "webhooks", Run: s.Run} }
