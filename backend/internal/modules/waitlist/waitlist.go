// Package waitlist is the waitlist module: sign-ups on the landing page
// with double opt-in, and clicks on its pricing buttons. It owns the tables
// waitlist_entries and pricing_clicks.
package waitlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// Module is the waitlist module.
type Module struct {
	db     *db.DB
	now    func() time.Time
	logger *slog.Logger
}

func New(d *db.DB) *Module {
	return &Module{db: d, now: time.Now, logger: slog.Default().With("component", "waitlist")}
}

// WaitlistEntry is a sign-up on the landing page's waitlist.
type WaitlistEntry struct {
	ID               uuid.UUID
	Email            string
	Role             string
	Plan             string
	ConsentVersion   string
	ConfirmSentAt    *time.Time
	ConfirmedAt      *time.Time
	UnsubscribeToken string
	CreatedAt        time.Time
}

// PricingClick counts clicks on a plan's button on the landing page per UTC day.
type PricingClick struct {
	Plan  string
	Day   time.Time
	Count int
}

// keepUnconfirmed is how long a sign-up waits for its double opt-in (the
// Datenschutzerklärung promises 7 days).
const keepUnconfirmed = 7 * 24 * time.Hour

// PruneJob deletes unconfirmed sign-ups after keepUnconfirmed; it runs
// about once an hour.
func (m *Module) PruneJob() scheduler.Job {
	return scheduler.Job{Name: "waitlist_prune", Run: func(ctx context.Context, _ time.Time) error {
		unlock, ok, err := m.db.TryLockJob(ctx, "waitlist_prune", 5*time.Minute, time.Hour)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		n, err := m.PruneWaitlist(ctx, m.now().Add(-keepUnconfirmed))
		if err != nil {
			return err
		}
		if n > 0 {
			m.logger.Info("deleted unconfirmed waitlist entries", "rows", n)
		}
		return nil
	}}
}
