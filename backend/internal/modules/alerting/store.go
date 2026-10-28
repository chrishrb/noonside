package alerting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// alert rules ---------------------------------------------------------------

func (m *Module) ListAlertRules(ctx context.Context, systemID uuid.UUID) ([]AlertRule, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT system_id, kind, enabled, params FROM alert_rules WHERE system_id = $1 ORDER BY kind`, systemID)
	return db.Collect(rows, err, func(r pgx.Row) (AlertRule, error) {
		var a AlertRule
		err := r.Scan(&a.SystemID, &a.Kind, &a.Enabled, &a.Params)
		return a, err
	})
}

func (m *Module) SetAlertRules(ctx context.Context, systemID uuid.UUID, rules []AlertRule) error {
	return m.db.InTx(ctx, func(ctx context.Context) error {
		if _, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM alert_rules WHERE system_id = $1`, systemID); err != nil {
			return err
		}
		for _, r := range rules {
			params := r.Params
			if params == nil {
				params = map[string]float64{}
			}
			if _, err := m.db.Q(ctx).Exec(ctx, `INSERT INTO alert_rules (system_id, kind, enabled, params) VALUES ($1, $2, $3, $4)`,
				systemID, r.Kind, r.Enabled, params); err != nil {
				return db.MapErr(err)
			}
		}
		return nil
	})
}

// alerts --------------------------------------------------------------------

const alertCols = `id, system_id, kind, subject_key, inverter_id, data_source_id, severity, details,
	opened_at, last_seen_at, missed, resolved_at, acked_by, acked_at, snoozed_until, reminded_at`

func scanAlert(r pgx.Row) (Alert, error) {
	var a Alert
	err := r.Scan(&a.ID, &a.SystemID, &a.Kind, &a.SubjectKey, &a.InverterID, &a.DataSourceID, &a.Severity, &a.Details,
		&a.OpenedAt, &a.LastSeenAt, &a.Missed, &a.ResolvedAt, &a.AckedBy, &a.AckedAt, &a.SnoozedUntil, &a.RemindedAt)
	return a, db.MapErr(err)
}

func oneAlert(a Alert, err error) (*Alert, error) {
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (m *Module) ListAlerts(ctx context.Context, systemID uuid.UUID, f AlertFilter) ([]Alert, error) {
	where := []string{"system_id = $1"}
	args := []any{systemID}
	if f.OpenOnly {
		where = append(where, "resolved_at IS NULL")
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		where = append(where, fmt.Sprintf("opened_at < $%d", len(args)))
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+alertCols+` FROM alerts WHERE `+strings.Join(where, " AND ")+
		fmt.Sprintf(` ORDER BY opened_at DESC LIMIT %d`, limit), args...)
	return db.Collect(rows, err, scanAlert)
}

func (m *Module) CountOpenAlerts(ctx context.Context, systemID uuid.UUID, now time.Time) (int, error) {
	var n int
	err := m.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM alerts WHERE system_id = $1 AND resolved_at IS NULL
		AND (snoozed_until IS NULL OR snoozed_until <= $2)`, systemID, now).Scan(&n)
	return n, err
}

func (m *Module) ListAlertsOpenedIn(ctx context.Context, systemID uuid.UUID, from, to time.Time) ([]Alert, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+alertCols+` FROM alerts
		WHERE system_id = $1 AND opened_at >= $2 AND opened_at < $3 ORDER BY opened_at LIMIT 100`, systemID, from, to)
	return db.Collect(rows, err, scanAlert)
}

func (m *Module) GetAlert(ctx context.Context, systemID, id uuid.UUID) (*Alert, error) {
	return oneAlert(scanAlert(m.db.Q(ctx).QueryRow(ctx, `SELECT `+alertCols+` FROM alerts WHERE system_id = $1 AND id = $2`, systemID, id)))
}

func (m *Module) OpenAlert(ctx context.Context, a *Alert) (*Alert, error) {
	details := a.Details
	if details == nil {
		details = map[string]any{}
	}
	return oneAlert(scanAlert(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO alerts (system_id, kind, subject_key, inverter_id, data_source_id, severity, details, opened_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8) RETURNING `+alertCols,
		a.SystemID, a.Kind, a.SubjectKey, a.InverterID, a.DataSourceID, a.Severity, details, a.OpenedAt)))
}

func (m *Module) SeeAlert(ctx context.Context, id uuid.UUID, at time.Time, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE alerts SET last_seen_at = $2, missed = 0, details = $3 WHERE id = $1`, id, at, details)
	return err
}

func (m *Module) MissAlert(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := m.db.Q(ctx).QueryRow(ctx, `UPDATE alerts SET missed = missed + 1 WHERE id = $1 RETURNING missed`, id).Scan(&n)
	return n, db.MapErr(err)
}

// ResolveAlert resolves the alert and marks the notifications about it
// being open read.
func (m *Module) ResolveAlert(ctx context.Context, id uuid.UUID, at time.Time) error {
	return m.db.InTx(ctx, func(ctx context.Context) error {
		tag, err := m.db.Q(ctx).Exec(ctx, `UPDATE alerts SET resolved_at = $2 WHERE id = $1 AND resolved_at IS NULL`, id, at)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return m.notifications.MarkAlertNotificationsRead(ctx, id, at)
	})
}

func (m *Module) AckAlert(ctx context.Context, systemID, id, userID uuid.UUID, at time.Time) (*Alert, error) {
	return oneAlert(scanAlert(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE alerts SET acked_by = $3, acked_at = $4 WHERE system_id = $1 AND id = $2 RETURNING `+alertCols,
		systemID, id, userID, at)))
}

func (m *Module) SnoozeAlert(ctx context.Context, systemID, id uuid.UUID, until *time.Time) (*Alert, error) {
	return oneAlert(scanAlert(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE alerts SET snoozed_until = $3 WHERE system_id = $1 AND id = $2 RETURNING `+alertCols,
		systemID, id, until)))
}

func (m *Module) MarkAlertReminded(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE alerts SET reminded_at = $2 WHERE id = $1`, id, at)
	return err
}

// PruneResolved deletes up to limit alerts resolved before the instant.
func (m *Module) PruneResolved(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM alerts WHERE id IN (
		SELECT id FROM alerts WHERE resolved_at < $1 LIMIT $2)`, before, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
