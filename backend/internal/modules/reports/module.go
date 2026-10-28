package reports

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// The reports module has no tables: it reads the other modules and queues
// report notifications.

// Store is what the reports read and write.
type Store interface {
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	ListInverters(ctx context.Context, systemID uuid.UUID) ([]systems.Inverter, error)
	GetTargets(ctx context.Context, systemID uuid.UUID) (map[int]float64, error)
	Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error)
	ListAlertsOpenedIn(ctx context.Context, systemID uuid.UUID, from, to time.Time) ([]alerting.Alert, error)
	ListRecipients(ctx context.Context, systemID uuid.UUID) ([]notifications.Recipient, error)
	CreateNotification(ctx context.Context, n *notifications.Notification, channels []string) (*notifications.Notification, error)
}

// Prices measures production at negative prices (implemented by the prices
// module).
type Prices interface {
	NegativeDuring(ctx context.Context, sys *systems.System, from, to time.Time, only map[uuid.UUID]bool) (*prices.NegativeEnergy, error)
}

// ModuleStore implements Store with the modules.
type ModuleStore struct {
	DB *db.DB
	*systems.Module
	Telemetry     *telemetry.Module
	Alerting      *alerting.Module
	Notifications *notifications.Module
}

func (s ModuleStore) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	return s.DB.TryLockJob(ctx, name, ttl, every)
}

func (s ModuleStore) Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error) {
	return s.Telemetry.Energy(ctx, systemID, granularity, from, to)
}

func (s ModuleStore) ListAlertsOpenedIn(ctx context.Context, systemID uuid.UUID, from, to time.Time) ([]alerting.Alert, error) {
	return s.Alerting.ListAlertsOpenedIn(ctx, systemID, from, to)
}

func (s ModuleStore) ListRecipients(ctx context.Context, systemID uuid.UUID) ([]notifications.Recipient, error) {
	return s.Notifications.ListRecipients(ctx, systemID)
}

func (s ModuleStore) CreateNotification(ctx context.Context, n *notifications.Notification, channels []string) (*notifications.Notification, error) {
	return s.Notifications.CreateNotification(ctx, n, channels)
}

// Job is the scheduler job queueing the due reports.
func (s *Service) Job() scheduler.Job { return scheduler.Job{Name: "reports", Run: s.Run} }
