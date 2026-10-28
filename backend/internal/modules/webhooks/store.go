package webhooks

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// webhooks ------------------------------------------------------------------

const webhookCols = `w.id, w.system_id, w.name, w.url, w.secret_enc, w.events, w.locale, w.enabled,
	w.created_by, w.created_at, w.updated_at`

func webhookDest(w *Webhook) []any {
	return []any{&w.ID, &w.SystemID, &w.Name, &w.URL, &w.SecretEnc, &w.Events, &w.Locale, &w.Enabled,
		&w.CreatedBy, &w.CreatedAt, &w.UpdatedAt}
}

func scanWebhook(r pgx.Row) (Webhook, error) {
	var w Webhook
	return w, db.MapErr(r.Scan(webhookDest(&w)...))
}

func oneWebhook(w Webhook, err error) (*Webhook, error) {
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (m *Module) ListWebhooks(ctx context.Context, systemID uuid.UUID) ([]Webhook, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT `+webhookCols+`, l.id, l.event, l.status, l.attempts, l.created_at, l.sent_at, l.last_error
		FROM webhooks w
		LEFT JOIN LATERAL (
			SELECT id, event, status, attempts, created_at, sent_at, last_error FROM webhook_deliveries
			WHERE webhook_id = w.id ORDER BY created_at DESC, id DESC LIMIT 1
		) l ON true
		WHERE w.system_id = $1 ORDER BY w.name, w.created_at`, systemID)
	return db.Collect(rows, err, func(r pgx.Row) (Webhook, error) {
		var w Webhook
		var (
			id                       *int64
			event, status, lastError *string
			attempts                 *int
			createdAt                *time.Time
			sentAt                   *time.Time
		)
		if err := r.Scan(append(webhookDest(&w), &id, &event, &status, &attempts, &createdAt, &sentAt, &lastError)...); err != nil {
			return w, err
		}
		if id != nil {
			w.LastDelivery = &WebhookDelivery{
				ID: *id, WebhookID: w.ID, Event: *event, Status: *status, Attempts: *attempts,
				CreatedAt: *createdAt, SentAt: sentAt, LastError: *lastError,
			}
		}
		return w, nil
	})
}

func (m *Module) GetWebhook(ctx context.Context, systemID, id uuid.UUID) (*Webhook, error) {
	return oneWebhook(scanWebhook(m.db.Q(ctx).QueryRow(ctx, `SELECT `+webhookCols+` FROM webhooks w WHERE w.system_id = $1 AND w.id = $2`, systemID, id)))
}

func (m *Module) ListWebhooksForEvent(ctx context.Context, event string) ([]Webhook, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+webhookCols+` FROM webhooks w
		WHERE w.enabled AND $1 = ANY(w.events) ORDER BY w.system_id, w.id`, event)
	return db.Collect(rows, err, scanWebhook)
}

func (m *Module) CreateWebhook(ctx context.Context, w *Webhook) (*Webhook, error) {
	return oneWebhook(scanWebhook(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO webhooks AS w (system_id, name, url, secret_enc, events, locale, enabled, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+webhookCols,
		w.SystemID, w.Name, w.URL, w.SecretEnc, w.Events, w.Locale, w.Enabled, w.CreatedBy)))
}

func (m *Module) UpdateWebhook(ctx context.Context, w *Webhook) (*Webhook, error) {
	return oneWebhook(scanWebhook(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE webhooks AS w SET name = $3, url = $4, secret_enc = $5, events = $6, locale = $7, enabled = $8, updated_at = now()
		WHERE w.system_id = $1 AND w.id = $2 RETURNING `+webhookCols,
		w.SystemID, w.ID, w.Name, w.URL, w.SecretEnc, w.Events, w.Locale, w.Enabled)))
}

func (m *Module) DeleteWebhook(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM webhooks WHERE system_id = $1 AND id = $2`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

// webhook deliveries --------------------------------------------------------

const webhookDeliveryCols = `d.id, d.event_id, d.webhook_id, d.event, d.dedupe_key, d.payload, d.status, d.attempts,
	d.next_attempt_at, d.last_error, d.created_at, d.sent_at`

func webhookDeliveryDest(d *WebhookDelivery) []any {
	return []any{&d.ID, &d.EventID, &d.WebhookID, &d.Event, &d.DedupeKey, &d.Payload, &d.Status, &d.Attempts,
		&d.NextAttemptAt, &d.LastError, &d.CreatedAt, &d.SentAt}
}

func (m *Module) QueueWebhookDelivery(ctx context.Context, d *WebhookDelivery) (bool, error) {
	payload := d.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	tag, err := m.db.Q(ctx).Exec(ctx, `
		INSERT INTO webhook_deliveries (webhook_id, event, dedupe_key, payload) VALUES ($1, $2, $3, $4)
		ON CONFLICT (webhook_id, event, dedupe_key) DO NOTHING`, d.WebhookID, d.Event, d.DedupeKey, payload)
	if err != nil {
		return false, db.MapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

func (m *Module) ClaimWebhookDeliveries(ctx context.Context, limit int, lease time.Duration) ([]WebhookDelivery, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `
		WITH due AS (
			SELECT id FROM webhook_deliveries
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_deliveries d
		SET next_attempt_at = now() + $2 * interval '1 second', attempts = d.attempts + 1
		FROM due, webhooks w
		WHERE d.id = due.id AND w.id = d.webhook_id
		RETURNING `+webhookDeliveryCols+`, `+webhookCols, limit, lease.Seconds())
	return db.Collect(rows, err, func(r pgx.Row) (WebhookDelivery, error) {
		var d WebhookDelivery
		d.Webhook = &Webhook{}
		return d, r.Scan(append(webhookDeliveryDest(&d), webhookDest(d.Webhook)...)...)
	})
}

func (m *Module) CompleteWebhookDelivery(ctx context.Context, id int64, status, deliveryErr string, retryAt *time.Time) error {
	if retryAt != nil {
		_, err := m.db.Q(ctx).Exec(ctx, `UPDATE webhook_deliveries SET status = 'pending', last_error = $2, next_attempt_at = $3 WHERE id = $1`,
			id, deliveryErr, *retryAt)
		return err
	}
	_, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE webhook_deliveries SET status = $2, last_error = $3,
			sent_at = CASE WHEN $2 = 'sent' THEN now() END
		WHERE id = $1`, id, status, deliveryErr)
	return err
}

func (m *Module) ListWebhookDeliveries(ctx context.Context, webhookID uuid.UUID, limit int) ([]WebhookDelivery, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+webhookDeliveryCols+` FROM webhook_deliveries d
		WHERE d.webhook_id = $1 ORDER BY d.created_at DESC, d.id DESC LIMIT $2`, webhookID, limit)
	return db.Collect(rows, err, func(r pgx.Row) (WebhookDelivery, error) {
		var d WebhookDelivery
		return d, r.Scan(webhookDeliveryDest(&d)...)
	})
}

func (m *Module) PruneWebhookDeliveries(ctx context.Context, before time.Time) (int64, error) {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM webhook_deliveries WHERE id IN (
		SELECT id FROM webhook_deliveries WHERE status <> 'pending' AND created_at < $1 LIMIT 5000)`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
