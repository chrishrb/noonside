package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// notifications -------------------------------------------------------------

// ListRecipients returns the system's members with their effective
// notification preferences for it: the system's own, else the user's
// default, else DefaultNotificationPrefs; muted until the later of both.
func (m *Module) ListRecipients(ctx context.Context, systemID uuid.UUID) ([]Recipient, error) {
	members, err := m.members.ListMembers(ctx, systemID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(members))
	for i, mem := range members {
		ids[i] = mem.UserID
	}
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+prefsCols+` FROM notification_prefs
		WHERE user_id = ANY($1) AND (system_id IS NULL OR system_id = $2)`, ids, systemID)
	prefs, err := db.Collect(rows, err, scanPrefs)
	if err != nil {
		return nil, err
	}
	defaults, overrides := map[uuid.UUID]NotificationPrefs{}, map[uuid.UUID]NotificationPrefs{}
	for _, p := range prefs {
		if p.SystemID == nil {
			defaults[p.UserID] = p
		} else {
			overrides[p.UserID] = p
		}
	}
	targets := map[uuid.UUID]TargetsSet{}
	rows, err = m.db.Q(ctx).Query(ctx, `SELECT user_id, ntfy_url <> '', webhook_url <> '' FROM notification_targets
		WHERE user_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var t TargetsSet
		if err := rows.Scan(&id, &t.Ntfy, &t.Webhook); err != nil {
			return nil, err
		}
		targets[id] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Recipient, len(members))
	for i, mem := range members {
		p := DefaultNotificationPrefs(mem.UserID)
		d, hasDefault := defaults[mem.UserID]
		o, hasOverride := overrides[mem.UserID]
		switch {
		case hasOverride:
			p = o
		case hasDefault:
			p = d
		}
		p.MutedUntil = later(d.MutedUntil, o.MutedUntil)
		p.UserID, p.SystemID = mem.UserID, &systemID
		out[i] = Recipient{
			UserID: mem.UserID, Email: mem.Email, Name: mem.Name, Locale: mem.Locale, Role: mem.Role,
			Prefs: p, Targets: targets[mem.UserID],
		}
	}
	return out, nil
}

// later returns the later of two optional times (SQL greatest, ignoring
// nulls).
func later(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return b
	case b == nil || a.After(*b):
		return a
	}
	return b
}

const notificationCols = `id, user_id, system_id, alert_id, type, severity, payload, created_at, read_at`

func scanNotification(r pgx.Row) (Notification, error) {
	var n Notification
	err := r.Scan(&n.ID, &n.UserID, &n.SystemID, &n.AlertID, &n.Type, &n.Severity, &n.Payload, &n.CreatedAt, &n.ReadAt)
	return n, db.MapErr(err)
}

func (m *Module) CreateNotification(ctx context.Context, n *Notification, channels []string) (*Notification, error) {
	var out Notification
	err := m.db.InTx(ctx, func(ctx context.Context) error {
		payload := n.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		var err error
		out, err = scanNotification(m.db.Q(ctx).QueryRow(ctx, `
			INSERT INTO notifications (user_id, system_id, alert_id, type, severity, payload, read_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+notificationCols,
			n.UserID, n.SystemID, n.AlertID, n.Type, n.Severity, payload, n.ReadAt))
		if err != nil {
			return err
		}
		for _, ch := range channels {
			if _, err := m.db.Q(ctx).Exec(ctx, `INSERT INTO notification_deliveries (notification_id, channel) VALUES ($1, $2)`, out.ID, ch); err != nil {
				return db.MapErr(err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *Module) ListNotifications(ctx context.Context, userID uuid.UUID, f NotificationFilter) ([]Notification, error) {
	where := []string{"user_id = $1"}
	args := []any{userID}
	if f.UnreadOnly {
		where = append(where, "read_at IS NULL")
	}
	if f.BeforeTime != nil && f.BeforeID != nil {
		args = append(args, *f.BeforeTime, *f.BeforeID)
		where = append(where, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+notificationCols+` FROM notifications WHERE `+strings.Join(where, " AND ")+
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT %d`, limit), args...)
	return db.Collect(rows, err, scanNotification)
}

func (m *Module) CountUnreadNotifications(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := m.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (m *Module) MarkNotificationsRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, at time.Time) error {
	if ids == nil {
		_, err := m.db.Q(ctx).Exec(ctx, `UPDATE notifications SET read_at = $2 WHERE user_id = $1 AND read_at IS NULL`, userID, at)
		return err
	}
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE notifications SET read_at = $3 WHERE user_id = $1 AND id = ANY($2) AND read_at IS NULL`, userID, ids, at)
	return err
}

func (m *Module) DeleteNotifications(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	if ids == nil {
		_, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM notifications WHERE user_id = $1 AND read_at IS NOT NULL`, userID)
		return err
	}
	_, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM notifications WHERE user_id = $1 AND id = ANY($2)`, userID, ids)
	return err
}

const prefsCols = `user_id, system_id, alert_channels, min_severity, notify_resolved, digest, report_channels, forecast_push, forecast_channels, muted_until`

func (m *Module) ListNotificationPrefs(ctx context.Context, userID uuid.UUID) ([]NotificationPrefs, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+prefsCols+` FROM notification_prefs WHERE user_id = $1 ORDER BY system_id NULLS FIRST`, userID)
	return db.Collect(rows, err, scanPrefs)
}

func scanPrefs(r pgx.Row) (NotificationPrefs, error) {
	var p NotificationPrefs
	err := r.Scan(&p.UserID, &p.SystemID, &p.AlertChannels, &p.MinSeverity, &p.NotifyResolved, &p.Digest, &p.ReportChannels, &p.ForecastPush, &p.ForecastChannels, &p.MutedUntil)
	return p, err
}

func (m *Module) SetNotificationPrefs(ctx context.Context, userID uuid.UUID, prefs []NotificationPrefs) error {
	return m.db.InTx(ctx, func(ctx context.Context) error {
		if _, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM notification_prefs WHERE user_id = $1`, userID); err != nil {
			return err
		}
		for _, p := range prefs {
			if _, err := m.db.Q(ctx).Exec(ctx, `INSERT INTO notification_prefs (`+prefsCols+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				userID, p.SystemID, nonNil(p.AlertChannels), p.MinSeverity, p.NotifyResolved, p.Digest, nonNil(p.ReportChannels), p.ForecastPush, nonNil(p.ForecastChannels), p.MutedUntil); err != nil {
				return db.MapErr(err)
			}
		}
		return nil
	})
}

// nonNil turns a nil slice into an empty array (the columns are NOT NULL).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// targets -------------------------------------------------------------------

func (m *Module) GetNotificationTargets(ctx context.Context, userID uuid.UUID) (*NotificationTargets, error) {
	t := NotificationTargets{UserID: userID}
	err := m.db.Q(ctx).QueryRow(ctx, `
		SELECT ntfy_url, ntfy_token_enc, webhook_url, webhook_secret_enc, updated_at
		FROM notification_targets WHERE user_id = $1`, userID).
		Scan(&t.NtfyURL, &t.NtfyTokenEnc, &t.WebhookURL, &t.WebhookSecretEnc, &t.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return &t, nil
}

func (m *Module) SetNotificationTargets(ctx context.Context, t *NotificationTargets) error {
	_, err := m.db.Q(ctx).Exec(ctx, `
		INSERT INTO notification_targets (user_id, ntfy_url, ntfy_token_enc, webhook_url, webhook_secret_enc)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			ntfy_url = EXCLUDED.ntfy_url, ntfy_token_enc = EXCLUDED.ntfy_token_enc,
			webhook_url = EXCLUDED.webhook_url, webhook_secret_enc = EXCLUDED.webhook_secret_enc,
			updated_at = now()`,
		t.UserID, t.NtfyURL, t.NtfyTokenEnc, t.WebhookURL, t.WebhookSecretEnc)
	return db.MapErr(err)
}

// deliveries ----------------------------------------------------------------

func (m *Module) ClaimDeliveries(ctx context.Context, limit int, lease time.Duration) ([]Delivery, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `
		WITH due AS (
			SELECT id FROM notification_deliveries
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_deliveries d
		SET next_attempt_at = now() + $2 * interval '1 second', attempts = d.attempts + 1
		FROM due, notifications n
		WHERE d.id = due.id AND n.id = d.notification_id
		RETURNING d.id, d.channel, d.attempts,
			n.id, n.user_id, n.system_id, n.alert_id, n.type, n.severity, n.payload, n.created_at, n.read_at`,
		limit, lease.Seconds())
	dls, err := db.Collect(rows, err, func(r pgx.Row) (Delivery, error) {
		var d Delivery
		n := &d.Notification
		err := r.Scan(&d.ID, &d.Channel, &d.Attempts,
			&n.ID, &n.UserID, &n.SystemID, &n.AlertID, &n.Type, &n.Severity, &n.Payload, &n.CreatedAt, &n.ReadAt)
		return d, err
	})
	if err != nil || len(dls) == 0 {
		return dls, err
	}
	// The recipients' address, name and language (identity module).
	ids := make([]uuid.UUID, len(dls))
	for i := range dls {
		ids[i] = dls[i].Notification.UserID
	}
	profiles, err := m.profiles.Profiles(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range dls {
		p := profiles[dls[i].Notification.UserID]
		dls[i].Email, dls[i].Name, dls[i].Locale = p.Email, p.Name, p.Locale
	}
	return dls, nil
}

func (m *Module) CompleteDelivery(ctx context.Context, id int64, status, deliveryErr string, retryAt *time.Time) error {
	if retryAt != nil {
		_, err := m.db.Q(ctx).Exec(ctx, `UPDATE notification_deliveries SET status = 'pending', last_error = $2, next_attempt_at = $3 WHERE id = $1`,
			id, deliveryErr, *retryAt)
		return err
	}
	_, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE notification_deliveries SET status = $2, last_error = $3,
			sent_at = CASE WHEN $2 = 'sent' THEN now() END
		WHERE id = $1`, id, status, deliveryErr)
	return err
}

// push subscriptions --------------------------------------------------------

const pushCols = `id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_success_at`

func scanPush(r pgx.Row) (PushSubscription, error) {
	var p PushSubscription
	err := r.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.UserAgent, &p.CreatedAt, &p.LastSuccessAt)
	return p, db.MapErr(err)
}

// UpsertPushSubscription moves an endpoint to the user who registers it last
// (a device signed in as another user).
func (m *Module) UpsertPushSubscription(ctx context.Context, sub *PushSubscription) (*PushSubscription, error) {
	p, err := scanPush(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent, failures = 0
		RETURNING `+pushCols, sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (m *Module) DeletePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	return err
}

func (m *Module) ListPushSubscriptions(ctx context.Context, userID uuid.UUID) ([]PushSubscription, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+pushCols+` FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at`, userID)
	return db.Collect(rows, err, scanPush)
}

// pushMaxFailures drops a subscription that keeps failing without the push
// service reporting it gone.
const pushMaxFailures = 20

func (m *Module) RecordPush(ctx context.Context, id uuid.UUID, ok, gone bool) error {
	var err error
	switch {
	case gone:
		_, err = m.db.Q(ctx).Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	case ok:
		_, err = m.db.Q(ctx).Exec(ctx, `UPDATE push_subscriptions SET last_success_at = now(), failures = 0 WHERE id = $1`, id)
	default:
		if _, err = m.db.Q(ctx).Exec(ctx, `UPDATE push_subscriptions SET failures = failures + 1 WHERE id = $1`, id); err == nil {
			_, err = m.db.Q(ctx).Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1 AND failures >= $2`, id, pushMaxFailures)
		}
	}
	return err
}

// retention -----------------------------------------------------------------

// PruneNotifications deletes up to limit old rows (read notifications before
// readBefore, any before before, finished deliveries) and returns how many
// it deleted.
func (m *Module) PruneNotifications(ctx context.Context, readBefore, before time.Time, limit int) (int64, error) {
	var total int64
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM notifications WHERE id IN (
			SELECT id FROM notifications WHERE (read_at IS NOT NULL AND read_at < $1) OR created_at < $2 LIMIT $3)`,
			[]any{readBefore, before, limit}},
		{`DELETE FROM notification_deliveries WHERE id IN (
			SELECT id FROM notification_deliveries WHERE status <> 'pending' AND next_attempt_at < $1 LIMIT $2)`,
			[]any{readBefore, limit}},
	} {
		tag, err := m.db.Q(ctx).Exec(ctx, q.sql, q.args...)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// MarkAlertNotificationsRead marks the unread notifications about an
// alert being open read (the alert was resolved). Call it in the
// resolving transaction.
func (m *Module) MarkAlertNotificationsRead(ctx context.Context, alertID uuid.UUID, at time.Time) error {
	_, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE notifications SET read_at = $2
		WHERE alert_id = $1 AND read_at IS NULL
			AND type IN ('`+NotificationAlertOpened+`', '`+NotificationAlertReminder+`')`, alertID, at)
	return err
}

// TryLockJob takes a scheduler job lease.
func (m *Module) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	return m.db.TryLockJob(ctx, name, ttl, every)
}

// InTx runs fn in a transaction (see db.DB.InTx).
func (m *Module) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.db.InTx(ctx, fn)
}
