package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// JobLocker takes leases for singleton scheduler jobs (implemented by DB).
type JobLocker interface {
	// TryLockJob takes the named lease for ttl if no one holds it and the job
	// last ran at least every ago; it records the run. unlock releases it.
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
}

func (d *DB) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	q := d.Q(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO job_leases (name) VALUES ($1) ON CONFLICT DO NOTHING`, name); err != nil {
		return nil, false, err
	}
	var until time.Time
	err := q.QueryRow(ctx, `
		UPDATE job_leases SET locked_until = now() + $2 * interval '1 second', last_run_at = now()
		WHERE name = $1
			AND (locked_until IS NULL OR locked_until < now())
			AND (last_run_at IS NULL OR last_run_at <= now() - $3 * interval '1 second')
		RETURNING locked_until`, name, ttl.Seconds(), every.Seconds()).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() {
		_, _ = q.Exec(context.WithoutCancel(ctx), `UPDATE job_leases SET locked_until = NULL WHERE name = $1 AND locked_until = $2`, name, until)
	}, true, nil
}
