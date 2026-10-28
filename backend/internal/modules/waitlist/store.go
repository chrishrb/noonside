package waitlist

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const waitlistColumns = `id, email, role, plan, consent_version, confirm_sent_at, confirmed_at, unsubscribe_token, created_at`

func scanWaitlistEntry(r pgx.Row) (*WaitlistEntry, error) {
	var e WaitlistEntry
	err := r.Scan(&e.ID, &e.Email, &e.Role, &e.Plan, &e.ConsentVersion, &e.ConfirmSentAt, &e.ConfirmedAt, &e.UnsubscribeToken, &e.CreatedAt)
	if err != nil {
		return nil, db.MapErr(err)
	}
	return &e, nil
}

// UpsertWaitlistEntry keeps the unsubscribe token of an existing entry, and
// an empty role or plan doesn't overwrite a stored one.
func (m *Module) UpsertWaitlistEntry(ctx context.Context, e *WaitlistEntry) (*WaitlistEntry, error) {
	return scanWaitlistEntry(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO waitlist_entries (email, role, plan, consent_version, unsubscribe_token)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email) DO UPDATE SET
			role = COALESCE(NULLIF(EXCLUDED.role, ''), waitlist_entries.role),
			plan = COALESCE(NULLIF(EXCLUDED.plan, ''), waitlist_entries.plan),
			consent_version = EXCLUDED.consent_version,
			updated_at = now()
		RETURNING `+waitlistColumns,
		e.Email, e.Role, e.Plan, e.ConsentVersion, e.UnsubscribeToken))
}

func (m *Module) SetWaitlistConfirmToken(ctx context.Context, id uuid.UUID, hash []byte, sentAt time.Time) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE waitlist_entries SET confirm_token_hash = $2, confirm_sent_at = $3, updated_at = now()
		WHERE id = $1`, id, hash, sentAt)
	if err != nil {
		return db.MapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) ConfirmWaitlistEntry(ctx context.Context, hash []byte, sentAfter time.Time) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE waitlist_entries SET confirmed_at = COALESCE(confirmed_at, now()), updated_at = now()
		WHERE confirm_token_hash = $1 AND confirm_sent_at > $2`, hash, sentAfter)
	if err != nil {
		return db.MapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) DeleteWaitlistEntry(ctx context.Context, unsubscribeToken string) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM waitlist_entries WHERE unsubscribe_token = $1`, unsubscribeToken)
	if err != nil {
		return db.MapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) CountWaitlistMailsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := m.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM waitlist_entries WHERE confirm_sent_at >= $1`, since).Scan(&n)
	return n, db.MapErr(err)
}

func (m *Module) IncrementPricingClick(ctx context.Context, plan string, day time.Time) error {
	_, err := m.db.Q(ctx).Exec(ctx, `
		INSERT INTO pricing_clicks (plan, day, count) VALUES ($1, $2, 1)
		ON CONFLICT (plan, day) DO UPDATE SET count = pricing_clicks.count + 1`, plan, pgDate(day))
	return db.MapErr(err)
}

func (m *Module) ListWaitlistEntries(ctx context.Context) ([]WaitlistEntry, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+waitlistColumns+` FROM waitlist_entries ORDER BY created_at, email`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (WaitlistEntry, error) {
		e, err := scanWaitlistEntry(r)
		if err != nil {
			return WaitlistEntry{}, err
		}
		return *e, nil
	})
}

func (m *Module) ListPricingClicks(ctx context.Context) ([]PricingClick, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT plan, day, count FROM pricing_clicks ORDER BY day, plan`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PricingClick, error) {
		var c PricingClick
		err := r.Scan(&c.Plan, &c.Day, &c.Count)
		return c, err
	})
}

func (m *Module) PruneWaitlist(ctx context.Context, before time.Time) (int64, error) {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM waitlist_entries WHERE confirmed_at IS NULL AND COALESCE(confirm_sent_at, created_at) < $1`, before)
	return tag.RowsAffected(), err
}

// pgDate strips the time zone so pgx encodes the calendar date as-is.
func pgDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
