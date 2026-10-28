package solarlog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHousekeep(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	now := time.Date(2024, 10, 30, 12, 0, 0, 0, loc)
	dir := t.TempDir()
	write := func(name string, size int, mod time.Time) {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, make([]byte, size), 0o600))
		require.NoError(t, os.Chtimes(p, mod, mod))
	}
	write("base_vars.js", 10, now)
	write("days_hist.js", 10, now.AddDate(-1, 0, 0)) // overwritten by the device, kept
	write("min241001.js", 10, now)                   // older than 14 days and imported
	write("min241016.js", 10, now)                   // 14 days: kept
	write("min241025.js", 10, now)
	write("min_day.js", 10, now)
	write("photo.jpg", 100, now.Add(-2*time.Hour))   // foreign, within the grace period
	write("backup.tar", 100, now.Add(-48*time.Hour)) // foreign, deleted
	write("min_huge.js", storage.MaxFileSize+1, now) // too large to be an export
	lim := Limits{Retention: 14 * 24 * time.Hour, ForeignGrace: 24 * time.Hour}
	ctx := context.Background()

	names := func() []string {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		slices.Sort(out)
		return out
	}

	// Nothing imported yet: dated files stay.
	u, err := housekeep(ctx, storage.NewLocal(dir), now, nil, nil, loc, lim)
	require.NoError(t, err)
	assert.Equal(t, 2, u.Removed)
	assert.Contains(t, names(), "min241001.js")
	assert.Equal(t, int64(6*10+100), u.UsedBytes)

	// Imported up to Oct 10: Oct 1 goes (older than the retention too).
	last := time.Date(2024, 10, 10, 8, 0, 0, 0, loc)
	u, err = housekeep(ctx, storage.NewLocal(dir), now, &last, nil, loc, lim)
	require.NoError(t, err)
	assert.Equal(t, 1, u.Removed)
	assert.Equal(t, []string{"base_vars.js", "days_hist.js", "min241016.js", "min241025.js", "min_day.js", "photo.jpg"}, names())

	// Imported up to today: the retention decides.
	write("min241015.js", 10, now)
	u, err = housekeep(ctx, storage.NewLocal(dir), now, &now, nil, loc, lim)
	require.NoError(t, err)
	assert.Equal(t, 1, u.Removed, "Oct 15 is older than 14 days")

	// Files before the system's minute-data cutoff are never imported.
	write("min231231.js", 10, now)
	last = time.Date(2023, 6, 1, 0, 0, 0, 0, loc)
	cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	u, err = housekeep(ctx, storage.NewLocal(dir), now, &last, &cutoff, loc, lim)
	require.NoError(t, err)
	assert.Equal(t, 1, u.Removed)
	assert.NotContains(t, names(), "min231231.js")
}
