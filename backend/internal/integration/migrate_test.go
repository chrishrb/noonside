//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The repository's migrations are all recorded, and a new one is applied
// once, also when its version is older than the newest applied one.
func TestMigrate(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	ran, err := db.Migrate(ctx, d, "../../db/migrations")
	require.NoError(t, err)
	assert.Empty(t, ran, "the test database is migrated by `mise run test:integration`")

	dir := t.TempDir()
	version := fmt.Sprintf("1%013d", time.Now().UnixNano()%1e13) // before every real one
	table := "migrate_test_" + version
	sql := fmt.Sprintf("CREATE TABLE %s (id int);\nINSERT INTO %s VALUES (1);", table, table)
	require.NoError(t, os.WriteFile(filepath.Join(dir, version+"_test.sql"), []byte(sql), 0o600))
	t.Cleanup(func() {
		_, _ = d.Pool().Exec(ctx, "DROP TABLE IF EXISTS "+table)
		_, _ = d.Pool().Exec(ctx, "DELETE FROM supabase_migrations.schema_migrations WHERE version = $1", version)
	})

	ran, err = db.Migrate(ctx, d, dir)
	require.NoError(t, err)
	assert.Equal(t, []string{version}, ran)
	ran, err = db.Migrate(ctx, d, dir)
	require.NoError(t, err)
	assert.Empty(t, ran)

	var n int
	require.NoError(t, d.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
	assert.Equal(t, 1, n)

	// A failing migration is rolled back and not recorded.
	bad := t.TempDir()
	badVersion := version + "1"
	require.NoError(t, os.WriteFile(filepath.Join(bad, badVersion+"_bad.sql"),
		[]byte(fmt.Sprintf("INSERT INTO %s VALUES (2);\nSELECT nope;", table)), 0o600))
	_, err = db.Migrate(ctx, d, bad)
	require.Error(t, err)
	require.NoError(t, d.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, d.Pool().QueryRow(ctx,
		"SELECT count(*) FROM supabase_migrations.schema_migrations WHERE version = $1", badVersion).Scan(&n))
	assert.Equal(t, 0, n)
}
