package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const dataSourceCols = `id, system_id, kind, name, config, enabled, poll_interval_s, last_run_at, last_success_at, last_error, state, upload_at, imported_upload_at, created_at, updated_at`

func scanDataSource(r pgx.Row) (DataSource, error) {
	var d DataSource
	var secs int
	err := r.Scan(&d.ID, &d.SystemID, &d.Kind, &d.Name, &d.Config, &d.Enabled, &secs, &d.LastRunAt, &d.LastSuccessAt, &d.LastError, &d.State, &d.UploadAt, &d.ImportedUploadAt, &d.CreatedAt, &d.UpdatedAt)
	d.PollInterval = time.Duration(secs) * time.Second
	return d, db.MapErr(err)
}

func oneDS(d DataSource, err error) (*DataSource, error) {
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (m *Module) CreateDataSource(ctx context.Context, ds *DataSource) (*DataSource, error) {
	return oneDS(scanDataSource(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO data_sources (system_id, kind, name, config, enabled, poll_interval_s)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+dataSourceCols,
		ds.SystemID, ds.Kind, ds.Name, ds.Config, ds.Enabled, int(ds.PollInterval.Seconds()))))
}

func (m *Module) GetDataSource(ctx context.Context, systemID, id uuid.UUID) (*DataSource, error) {
	return oneDS(scanDataSource(m.db.Q(ctx).QueryRow(ctx, `SELECT `+dataSourceCols+` FROM data_sources WHERE system_id = $1 AND id = $2`, systemID, id)))
}

func (m *Module) UpdateDataSource(ctx context.Context, ds *DataSource) (*DataSource, error) {
	return oneDS(scanDataSource(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE data_sources SET name = $3, config = $4, enabled = $5, poll_interval_s = $6, updated_at = now()
		WHERE system_id = $1 AND id = $2 RETURNING `+dataSourceCols,
		ds.SystemID, ds.ID, ds.Name, ds.Config, ds.Enabled, int(ds.PollInterval.Seconds()))))
}

func (m *Module) DeleteDataSource(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM data_sources WHERE system_id = $1 AND id = $2`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) ListDataSources(ctx context.Context, systemID uuid.UUID) ([]DataSource, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+dataSourceCols+` FROM data_sources WHERE system_id = $1 ORDER BY name`, systemID)
	return db.Collect(rows, err, scanDataSource)
}

func (m *Module) ListEnabledDataSources(ctx context.Context) ([]DataSource, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+dataSourceCols+` FROM data_sources WHERE enabled ORDER BY id`)
	return db.Collect(rows, err, scanDataSource)
}

func (m *Module) ListDataSourcesByKind(ctx context.Context, kind string) ([]DataSource, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+dataSourceCols+` FROM data_sources WHERE kind = $1 ORDER BY id`, kind)
	return db.Collect(rows, err, scanDataSource)
}

func (m *Module) RecordDataSourceRun(ctx context.Context, id uuid.UUID, at time.Time, runErr error) error {
	if runErr != nil {
		_, err := m.db.Q(ctx).Exec(ctx, `UPDATE data_sources SET last_run_at = $2, last_error = $3 WHERE id = $1`, id, at, runErr.Error())
		return err
	}
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE data_sources SET last_run_at = $2, last_success_at = $2, last_error = '' WHERE id = $1`, id, at)
	return err
}

// MarkUploadImported records that the uploads until at were imported.
func (m *Module) MarkUploadImported(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE data_sources SET imported_upload_at = $2 WHERE id = $1`, id, at)
	return err
}

// MarkHostedUpload notes an upload to the hosted FTP account username, so
// the scheduler imports its source; false if no source has that account.
func (m *Module) MarkHostedUpload(ctx context.Context, username string) (bool, error) {
	tag, err := m.db.Q(ctx).Exec(ctx, `
		UPDATE data_sources SET upload_at = now()
		WHERE kind = $1 AND config->>'username' = $2`, KindSolarLogHosted, username)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (m *Module) SetDataSourceState(ctx context.Context, id uuid.UUID, state []byte) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE data_sources SET state = $2 WHERE id = $1`, id, state)
	return err
}

func (m *Module) TryLockDataSource(ctx context.Context, id uuid.UUID, ttl time.Duration) (func(), bool, error) {
	var until time.Time
	err := m.db.Q(ctx).QueryRow(ctx, `
		UPDATE data_sources SET locked_until = now() + $2 * interval '1 second'
		WHERE id = $1 AND (locked_until IS NULL OR locked_until < now())
		RETURNING locked_until`, id, ttl.Seconds()).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() {
		// Only our own lease: after it expired another run may hold a new one.
		_, _ = m.db.Q(ctx).Exec(context.WithoutCancel(ctx), `UPDATE data_sources SET locked_until = NULL WHERE id = $1 AND locked_until = $2`, id, until)
	}, true, nil
}
