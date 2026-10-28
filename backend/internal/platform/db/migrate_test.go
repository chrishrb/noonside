package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMigrations(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"20260928000000_b.sql", "20260925000000_init.sql", "README.md", "x.sql"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("SELECT 1;"), 0o600))
	}
	ms, err := ReadMigrations(dir)
	require.NoError(t, err)
	require.Len(t, ms, 2)
	assert.Equal(t, Migration{Version: "20260925000000", Name: "init", Path: filepath.Join(dir, "20260925000000_init.sql")}, ms[0])
	assert.Equal(t, "b", ms[1].Name)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "20260928000000_c.sql"), nil, 0o600))
	_, err = ReadMigrations(dir)
	assert.ErrorContains(t, err, "share version")
}

// The repository's migrations parse.
func TestRepoMigrations(t *testing.T) {
	ms, err := ReadMigrations("../../../db/migrations")
	require.NoError(t, err)
	assert.NotEmpty(t, ms)
}
