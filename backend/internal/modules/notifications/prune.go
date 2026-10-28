package notifications

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/scheduler"
)

// Retention of notifications.
const (
	keepRead   = 90 * 24 * time.Hour
	keepAny    = 365 * 24 * time.Hour
	pruneBatch = 5000
)

// PruneJob deletes old notifications and finished deliveries, in batches
// until nothing is left or the deadline passes; it runs about once an hour.
func (m *Module) PruneJob() scheduler.Job {
	return scheduler.Job{Name: "notifications_prune", Run: func(ctx context.Context, deadline time.Time) error {
		unlock, ok, err := m.db.TryLockJob(ctx, "notifications_prune", 10*time.Minute, time.Hour)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		now := m.now()
		var total int64
		for m.now().Before(deadline) {
			n, err := m.PruneNotifications(ctx, now.Add(-keepRead), now.Add(-keepAny), pruneBatch)
			if err != nil {
				return err
			}
			total += n
			if n == 0 {
				break
			}
		}
		if total > 0 {
			m.logger.Info("deleted old notifications", "rows", total)
		}
		return nil
	}}
}

// DispatchJob sends due deliveries with d.
func DispatchJob(d *Dispatcher) scheduler.Job {
	return scheduler.Job{Name: "notifications", Run: d.Run}
}
