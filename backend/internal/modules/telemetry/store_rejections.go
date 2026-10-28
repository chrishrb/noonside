package telemetry

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (m *Module) RecordIngestRejections(ctx context.Context, rs []IngestRejection) error {
	if len(rs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, r := range rs {
		b.Queue(`
			INSERT INTO ingest_rejections (system_id, inverter_id, day, reason, count, sample)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (system_id, inverter_id, day, reason) DO UPDATE SET
				count = ingest_rejections.count + EXCLUDED.count,
				sample = EXCLUDED.sample,
				updated_at = now()`,
			r.SystemID, r.InverterID, pgDate(r.Day), r.Reason, r.Count, r.Sample)
	}
	return db.MapErr(m.db.Q(ctx).SendBatch(ctx, b).Close())
}

func (m *Module) ListIngestRejections(ctx context.Context, systemID uuid.UUID, day time.Time) ([]IngestRejection, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT system_id, inverter_id, day, reason, count, sample FROM ingest_rejections
		WHERE system_id = $1 AND day = $2 ORDER BY inverter_id, reason`, systemID, pgDate(day))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (IngestRejection, error) {
		var x IngestRejection
		err := r.Scan(&x.SystemID, &x.InverterID, &x.Day, &x.Reason, &x.Count, &x.Sample)
		return x, err
	})
}

func (m *Module) PruneIngestRejections(ctx context.Context, before time.Time) (int64, error) {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM ingest_rejections WHERE day < $1`, pgDate(before))
	return tag.RowsAffected(), err
}
