package datasources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/adapter"
	"github.com/chrishrb/noonside/internal/platform/errtrack"
	"github.com/google/uuid"
)

// ErrBusy is returned when the source is already running (here or on
// another replica).
var ErrBusy = errors.New("data source is already running")

// Factory builds the poller for a data source.
type Factory func(ds DataSource) (adapter.Poller, error)

// runStore is what the runner reads and writes.
type runStore interface {
	ListEnabledDataSources(ctx context.Context) ([]DataSource, error)
	// TryLockDataSource takes a lease on the source for ttl unless another
	// run holds one; unlock releases it. Leases of killed runs simply expire.
	TryLockDataSource(ctx context.Context, id uuid.UUID, ttl time.Duration) (unlock func(), ok bool, err error)
	RecordDataSourceRun(ctx context.Context, id uuid.UUID, at time.Time, runErr error) error
	// MarkUploadImported records that a run imported the uploads until at.
	MarkUploadImported(ctx context.Context, id uuid.UUID, at time.Time) error
}

// runner polls the due sources on their intervals.
type runner struct {
	store   runStore
	factory Factory
	timeout time.Duration
	now     func() time.Time
	logger  *slog.Logger

	mu      sync.Mutex
	running map[uuid.UUID]bool
	wg      sync.WaitGroup
}

func newRunner(st runStore, factory Factory) *runner {
	return &runner{
		store:   st,
		factory: factory,
		timeout: 10 * time.Minute,
		now:     time.Now,
		logger:  slog.Default().With("component", "scheduler"),
		running: map[uuid.UUID]bool{},
	}
}

func (r *runner) startDue(ctx context.Context) {
	sources, err := r.store.ListEnabledDataSources(ctx)
	if err != nil {
		r.logger.Error("list data sources", "error", err)
		return
	}
	now := r.now()
	for _, ds := range sources {
		if !due(ds, now) {
			continue
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			// Its own transaction: the poll outlives the job's.
			ctx, finish := errtrack.StartJob(ctx, "data_source "+ds.Kind, "data_source_id", ds.ID.String(), "kind", ds.Kind)
			var err error
			defer func() { finish(err) }()
			defer errtrack.Recover(ctx, r.logger, "data source run panicked", "data_source_id", ds.ID, "kind", ds.Kind)
			switch err = r.RunOnce(ctx, ds); {
			case errors.Is(err, ErrBusy):
				r.logger.Info("data source busy, skipped", "data_source_id", ds.ID, "kind", ds.Kind)
				err = nil // not a failed run
			case err != nil:
				r.logger.Warn("data source run failed", "data_source_id", ds.ID, "kind", ds.Kind, "error", err)
			}
		}()
	}
}

// uploadQuiet is how long a hosted source waits after the last upload, so
// the files of one upload session are imported in one run.
const uploadQuiet = 30 * time.Second

// due reports whether ds should run now. Sources run on their interval,
// hosted FTP sources after uploads (once uploadQuiet passed without
// another). Failing sources back off exponentially up to 1h, based on the
// time since the last success.
func due(ds DataSource, now time.Time) bool {
	wait := ds.PollInterval
	if ds.Kind == KindSolarLogHosted {
		pending := ds.UploadAt != nil && (ds.ImportedUploadAt == nil || ds.UploadAt.After(*ds.ImportedUploadAt))
		if !pending || now.Sub(*ds.UploadAt) < uploadQuiet {
			return false
		}
		// Uploads decide; only a failed import waits before its retry.
		wait = 0
	}
	if ds.LastRunAt == nil {
		return true
	}
	if ds.LastError != "" {
		failingFor := now.Sub(ds.CreatedAt)
		if ds.LastSuccessAt != nil {
			failingFor = now.Sub(*ds.LastSuccessAt)
		}
		wait = min(max(ds.PollInterval, failingFor/4), time.Hour)
	}
	return now.Sub(*ds.LastRunAt) >= wait
}

// RunOnce runs ds now and records the outcome.
func (r *runner) RunOnce(ctx context.Context, ds DataSource) error {
	r.mu.Lock()
	if r.running[ds.ID] {
		r.mu.Unlock()
		return ErrBusy
	}
	r.running[ds.ID] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.running, ds.ID)
		r.mu.Unlock()
	}()

	// The lease outlives the poll timeout by the time needed to record the
	// run, so a killed server can't block the source for longer.
	unlock, ok, err := r.store.TryLockDataSource(ctx, ds.ID, r.timeout+time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}
	defer unlock()

	runErr := r.poll(ctx, ds)
	if err := r.store.RecordDataSourceRun(context.WithoutCancel(ctx), ds.ID, r.now(), runErr); err != nil {
		r.logger.Error("record data source run", "data_source_id", ds.ID, "error", err)
	}
	// Only the uploads noted before the run started count as imported;
	// later ones start another run. Failed imports are retried.
	if runErr == nil && ds.UploadAt != nil {
		if err := r.store.MarkUploadImported(context.WithoutCancel(ctx), ds.ID, *ds.UploadAt); err != nil {
			r.logger.Error("record imported upload", "data_source_id", ds.ID, "error", err)
		}
	}
	return runErr
}

func (r *runner) poll(ctx context.Context, ds DataSource) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("poller panicked: %v", rec)
		}
	}()
	p, err := r.factory(ds)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return p.Poll(ctx)
}
