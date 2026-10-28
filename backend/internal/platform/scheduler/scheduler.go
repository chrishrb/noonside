// Package scheduler runs the modules' periodic jobs on a ticker in the
// long-running server (Run), or once (RunDue). It knows
// nothing about the jobs; modules register them in the order they run.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// Job is a periodic task. Jobs run sequentially on every scheduler run and
// guard their own frequency (e.g. with db.DB.TryLockJob).
type Job struct {
	Name string
	// Run should stop starting new work after the deadline.
	Run func(ctx context.Context, deadline time.Time) error
	// Wait, if set, blocks until work Run started in the background has
	// finished; RunDue waits for it before returning.
	Wait func()
}

// jobBudget is the time each job may take per run.
const jobBudget = time.Minute

type Scheduler struct {
	tick   time.Duration
	now    func() time.Time
	logger *slog.Logger
	jobs   []Job
}

func New() *Scheduler {
	return &Scheduler{
		tick:   30 * time.Second,
		now:    time.Now,
		logger: slog.Default().With("component", "scheduler"),
	}
}

// Add registers jobs; they run in the order added.
func (s *Scheduler) Add(jobs ...Job) { s.jobs = append(s.jobs, jobs...) }

// Run runs the jobs every tick until ctx is cancelled, then waits for their
// background work.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		s.runJobs(ctx)
		select {
		case <-ctx.Done():
			s.wait()
			return
		case <-t.C:
		}
	}
}

// RunDue runs every job once and waits until their background work is done.
// It is the entry point when an external timer drives the scheduler
// instead of Run.
func (s *Scheduler) RunDue(ctx context.Context) {
	s.runJobs(ctx)
	s.wait()
}

func (s *Scheduler) runJobs(ctx context.Context) {
	for _, j := range s.jobs {
		s.runJob(ctx, j)
	}
}

// runJob runs one job as its own Sentry transaction; a panic is logged (and
// reported) like a failure, so the jobs after it still run.
func (s *Scheduler) runJob(ctx context.Context, j Job) {
	ctx, finish := errtrack.StartJob(ctx, j.Name)
	var err error
	defer func() { finish(err) }()
	defer errtrack.Recover(ctx, s.logger, "scheduler job panicked", "job", j.Name)
	if err = j.Run(ctx, s.now().Add(jobBudget)); err != nil {
		s.logger.ErrorContext(ctx, "scheduler job failed", "job", j.Name, "error", err)
	}
}

func (s *Scheduler) wait() {
	for _, j := range s.jobs {
		if j.Wait != nil {
			j.Wait()
		}
	}
}
