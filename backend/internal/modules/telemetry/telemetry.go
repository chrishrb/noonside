// Package telemetry is the telemetry module: the single write path for
// inverter data (ingest with plausibility checks) and the time series it
// stores. It owns the tables measurement_days, inverter_daily,
// inverter_state, inverter_events and ingest_rejections.
package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// Systems is what telemetry needs from the systems module.
type Systems interface {
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	ListInverters(ctx context.Context, systemID uuid.UUID) ([]systems.Inverter, error)
	CreateInverter(ctx context.Context, inv *systems.Inverter) (*systems.Inverter, error)
}

// Module is the telemetry module.
type Module struct {
	db      *db.DB
	systems Systems
	now     func() time.Time
	logger  *slog.Logger
}

func New(d *db.DB, sys Systems) *Module {
	return &Module{db: d, systems: sys, now: time.Now, logger: slog.Default().With("component", "telemetry")}
}

type Measurement struct {
	Time         time.Time
	InverterID   uuid.UUID
	PacW         *float64
	PdcW         *float64
	UdcV         *float64
	EdayWh       *float64
	TemperatureC *float64
}

// DailyEnergy is the energy of one inverter on one local calendar day.
type DailyEnergy struct {
	InverterID uuid.UUID
	Day        time.Time // date in the system's time zone, stored as midnight UTC
	EnergyWh   float64
	PacMaxW    *float64
}

// IngestRejection counts the implausible values of one inverter, local day
// and reason that ingest dropped.
type IngestRejection struct {
	SystemID   uuid.UUID
	InverterID uuid.UUID
	Day        time.Time // local date, stored as midnight UTC
	Reason     string
	Count      int
	// Sample is the last rejected value, for diagnosis.
	Sample map[string]any
}

type InverterState struct {
	InverterID uuid.UUID
	Time       time.Time
	PacW       *float64
	PdcW       *float64
	StatusCode *int
	StatusText string
	ErrorCode  *int
	ErrorText  string
	// PowerLimitPct is the active power limit the device reported.
	PowerLimitPct *int
}

type InverterEvent struct {
	ID         int64
	InverterID uuid.UUID
	StartedAt  time.Time
	EndedAt    *time.Time
	StatusCode *int
	StatusText string
	ErrorCode  *int
	ErrorText  string
	// PowerLimitPct is the active power limit during the event (curtailment).
	PowerLimitPct *int
}

// PowerPoint is one bucket of the intraday power curve.
type PowerPoint struct {
	Time       time.Time
	InverterID uuid.UUID
	PacW       float64
	PdcW       float64
	// UdcV and TemperatureC are nil without a reading in the bucket.
	UdcV         *float64
	TemperatureC *float64
}

// EnergyBucket is aggregated energy of one inverter for a day, month or year.
type EnergyBucket struct {
	Start      time.Time // local calendar start (midnight UTC-encoded date)
	InverterID uuid.UUID
	EnergyWh   float64
	PacMaxW    *float64
	// Corrected is true if a day in the bucket was corrected manually.
	Corrected bool
}

type EventFilter struct {
	InverterID *uuid.UUID
	From       *time.Time
	To         *time.Time
	StatusCode *int
	OnlyErrors bool
	BeforeID   *int64
	Limit      int
}

// Interval is a time range [Start, End).
type Interval struct {
	Start, End time.Time
}

// Minute data past the systems' retention is deleted on every run, in
// batches of pruneBatch day rows and for at most the job's budget; a large
// history is spread over several runs. With nothing to delete a run costs two
// index lookups per system.
const pruneBatch = 500

// RetentionJob deletes minute data older than each system's retention.
func (m *Module) RetentionJob() scheduler.Job {
	return scheduler.Job{Name: "minute_retention", Run: func(ctx context.Context, deadline time.Time) error {
		pruneMinuteData(ctx, retentionStore{m}, m.now, deadline, m.logger)
		return nil
	}}
}

// pruneStore is what the retention job needs.
type pruneStore interface {
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	DeleteMeasurementsBefore(ctx context.Context, systemID uuid.UUID, before time.Time, maxRows int) (int64, bool, error)
}

type retentionStore struct{ m *Module }

func (s retentionStore) ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error) {
	return s.m.systems.ListSystems(ctx, userID)
}

func (s retentionStore) DeleteMeasurementsBefore(ctx context.Context, systemID uuid.UUID, before time.Time, maxRows int) (int64, bool, error) {
	return s.m.DeleteMeasurementsBefore(ctx, systemID, before, maxRows)
}

// pruneMinuteData deletes minute data older than each system's retention
// until the deadline.
func pruneMinuteData(ctx context.Context, st pruneStore, now func() time.Time, deadline time.Time, logger *slog.Logger) {
	start := now()
	all, err := st.ListSystems(ctx, nil)
	if err != nil {
		logger.Error("list systems for retention", "error", err)
		return
	}
	for _, sys := range all {
		cutoff := sys.MinuteCutoff(start)
		if cutoff == nil {
			continue
		}
		var total int64
		for done := false; !done; {
			if now().After(deadline) {
				logger.Info("minute data retention paused, continuing next run", "system_id", sys.ID)
				break
			}
			var n int64
			if n, done, err = st.DeleteMeasurementsBefore(ctx, sys.ID, *cutoff, pruneBatch); err != nil {
				logger.Error("delete old minute data", "system_id", sys.ID, "error", err)
				break
			}
			total += n
		}
		if total > 0 {
			logger.Info("deleted old minute data", "system_id", sys.ID, "before", cutoff.Format(time.DateOnly), "samples", total)
		}
	}
}

// keepRejections is how long implausible values are counted.
const keepRejections = 90 * 24 * time.Hour

// RejectionsPruneJob deletes ingest rejections older than keepRejections;
// it runs about once an hour.
func (m *Module) RejectionsPruneJob() scheduler.Job {
	return scheduler.Job{Name: "ingest_rejections_prune", Run: func(ctx context.Context, _ time.Time) error {
		unlock, ok, err := m.db.TryLockJob(ctx, "ingest_rejections_prune", 5*time.Minute, time.Hour)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		n, err := m.PruneIngestRejections(ctx, m.now().Add(-keepRejections))
		if err != nil {
			return err
		}
		if n > 0 {
			m.logger.Info("deleted old ingest rejections", "rows", n)
		}
		return nil
	}}
}
