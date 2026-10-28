// Package alerting is the alerting module: rules evaluated against each
// system's data every 5 minutes, the alerts they open and resolve, and the
// morning forecast push. Alerts reach users as notifications (notifications
// module) and system webhooks (through the alert hook). It owns the tables
// alerts and alert_rules.
package alerting

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// AlertRule overrides the built-in defaults of one rule kind for a system.
type AlertRule struct {
	SystemID uuid.UUID
	Kind     string
	Enabled  bool
	Params   map[string]float64
}

// Alert is an incident opened and resolved by the alert engine.
type Alert struct {
	ID           uuid.UUID
	SystemID     uuid.UUID
	Kind         string
	SubjectKey   string // "" for the system, else an inverter or data source id
	InverterID   *uuid.UUID
	DataSourceID *uuid.UUID
	Severity     notifications.Severity
	Details      map[string]any
	OpenedAt     time.Time
	LastSeenAt   time.Time
	Missed       int
	ResolvedAt   *time.Time
	AckedBy      *uuid.UUID
	AckedAt      *time.Time
	SnoozedUntil *time.Time
	RemindedAt   *time.Time
}

type AlertFilter struct {
	OpenOnly bool
	Before   *time.Time // opened_at cursor
	Limit    int
}

// Store is what the engine and the forecaster read and write: their own
// alerts and rules, and the other modules' data.
type Store interface {
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)

	// alerting
	ListAlertRules(ctx context.Context, systemID uuid.UUID) ([]AlertRule, error)
	ListAlerts(ctx context.Context, systemID uuid.UUID, f AlertFilter) ([]Alert, error)
	// OpenAlert inserts an open alert; db.ErrConflict if one is open for the
	// same subject.
	OpenAlert(ctx context.Context, a *Alert) (*Alert, error)
	// SeeAlert records that the alert's condition still holds.
	SeeAlert(ctx context.Context, id uuid.UUID, at time.Time, details map[string]any) error
	// MissAlert counts an evaluation without the condition and returns the
	// count in a row.
	MissAlert(ctx context.Context, id uuid.UUID) (int, error)
	ResolveAlert(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkAlertReminded(ctx context.Context, id uuid.UUID, at time.Time) error

	// systems
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	ListInverters(ctx context.Context, systemID uuid.UUID) ([]systems.Inverter, error)
	// datasources
	ListDataSources(ctx context.Context, systemID uuid.UUID) ([]datasources.DataSource, error)
	// telemetry
	ListInverterStates(ctx context.Context, systemID uuid.UUID) ([]telemetry.InverterState, error)
	OpenEvent(ctx context.Context, inverterID uuid.UUID) (*telemetry.InverterEvent, error)
	Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error)
	FirstDataDay(ctx context.Context, systemID uuid.UUID) (*time.Time, error)
	ListIngestRejections(ctx context.Context, systemID uuid.UUID, day time.Time) ([]telemetry.IngestRejection, error)
	PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error)
	// notifications
	ListRecipients(ctx context.Context, systemID uuid.UUID) ([]notifications.Recipient, error)
	CreateNotification(ctx context.Context, n *notifications.Notification, channels []string) (*notifications.Notification, error)
}

// Module is the alerting module.
type Module struct {
	db            *db.DB
	notifications *notifications.Module
	store         Store
	now           func() time.Time
}

// New returns the module and registers how its notification types render.
func New(d *db.DB, sys *systems.Module, tel *telemetry.Module, ds *datasources.Module, notes *notifications.Module) *Module {
	m := &Module{db: d, notifications: notes, now: time.Now}
	m.store = moduleStore{alerting: m, Module: sys, tel: tel, ds: ds, notes: notes}
	registerRenderers()
	return m
}

// Store returns what the engine and forecaster run on.
func (m *Module) Store() Store { return m.store }

// moduleStore implements Store with the modules.
type moduleStore struct {
	alerting *Module
	*systems.Module
	tel   *telemetry.Module
	ds    *datasources.Module
	notes *notifications.Module
}

func (s moduleStore) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	return s.alerting.db.TryLockJob(ctx, name, ttl, every)
}
func (s moduleStore) ListAlertRules(ctx context.Context, systemID uuid.UUID) ([]AlertRule, error) {
	return s.alerting.ListAlertRules(ctx, systemID)
}
func (s moduleStore) ListAlerts(ctx context.Context, systemID uuid.UUID, f AlertFilter) ([]Alert, error) {
	return s.alerting.ListAlerts(ctx, systemID, f)
}
func (s moduleStore) OpenAlert(ctx context.Context, a *Alert) (*Alert, error) {
	return s.alerting.OpenAlert(ctx, a)
}
func (s moduleStore) SeeAlert(ctx context.Context, id uuid.UUID, at time.Time, details map[string]any) error {
	return s.alerting.SeeAlert(ctx, id, at, details)
}
func (s moduleStore) MissAlert(ctx context.Context, id uuid.UUID) (int, error) {
	return s.alerting.MissAlert(ctx, id)
}
func (s moduleStore) ResolveAlert(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.alerting.ResolveAlert(ctx, id, at)
}
func (s moduleStore) MarkAlertReminded(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.alerting.MarkAlertReminded(ctx, id, at)
}
func (s moduleStore) ListDataSources(ctx context.Context, systemID uuid.UUID) ([]datasources.DataSource, error) {
	return s.ds.ListDataSources(ctx, systemID)
}
func (s moduleStore) ListInverterStates(ctx context.Context, systemID uuid.UUID) ([]telemetry.InverterState, error) {
	return s.tel.ListInverterStates(ctx, systemID)
}
func (s moduleStore) OpenEvent(ctx context.Context, inverterID uuid.UUID) (*telemetry.InverterEvent, error) {
	return s.tel.OpenEvent(ctx, inverterID)
}
func (s moduleStore) Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error) {
	return s.tel.Energy(ctx, systemID, granularity, from, to)
}
func (s moduleStore) FirstDataDay(ctx context.Context, systemID uuid.UUID) (*time.Time, error) {
	return s.tel.FirstDataDay(ctx, systemID)
}
func (s moduleStore) ListIngestRejections(ctx context.Context, systemID uuid.UUID, day time.Time) ([]telemetry.IngestRejection, error) {
	return s.tel.ListIngestRejections(ctx, systemID, day)
}
func (s moduleStore) PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error) {
	return s.tel.PowerCurve(ctx, systemID, from, to, bucket)
}
func (s moduleStore) ListRecipients(ctx context.Context, systemID uuid.UUID) ([]notifications.Recipient, error) {
	return s.notes.ListRecipients(ctx, systemID)
}
func (s moduleStore) CreateNotification(ctx context.Context, n *notifications.Notification, channels []string) (*notifications.Notification, error) {
	return s.notes.CreateNotification(ctx, n, channels)
}

// Retention of resolved alerts.
const (
	keepResolved = 365 * 24 * time.Hour
	pruneBatch   = 5000
)

// PruneJob deletes alerts resolved more than a year ago; it runs about
// once an hour.
func (m *Module) PruneJob() scheduler.Job {
	return scheduler.Job{Name: "alerts_prune", Run: func(ctx context.Context, deadline time.Time) error {
		unlock, ok, err := m.db.TryLockJob(ctx, "alerts_prune", 10*time.Minute, time.Hour)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		before := m.now().Add(-keepResolved)
		for m.now().Before(deadline) {
			n, err := m.PruneResolved(ctx, before, pruneBatch)
			if err != nil || n == 0 {
				return err
			}
		}
		return nil
	}}
}
