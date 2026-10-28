//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Data source runs are serialized by a lease that expires on its own, so a
// killed run cannot block the source.
func TestDataSourceLease(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var sys, ds obj
	owner.do("POST", "/systems", obj{"name": "Lease PV"}, 201, &sys)
	owner.do("POST", "/systems/"+sys["id"].(string)+"/data-sources",
		obj{"kind": "solarlog_remote", "name": "lease", "config": obj{"protocol": "ftp", "host": "ftp.example.com"}, "enabled": false}, 201, &ds)
	id := uuid.MustParse(ds["id"].(string))

	unlock, ok, err := e.DataSources.TryLockDataSource(ctx, id, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = e.DataSources.TryLockDataSource(ctx, id, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "held by the first run")
	unlock()

	// an expired lease is taken over, and the stale unlock leaves the new one alone
	stale, ok, err := e.DataSources.TryLockDataSource(ctx, id, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = e.db.Pool().Exec(ctx, `UPDATE data_sources SET locked_until = now() - interval '1 second' WHERE id = $1`, id)
	require.NoError(t, err)
	unlock, ok, err = e.DataSources.TryLockDataSource(ctx, id, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "expired lease")
	stale()
	_, ok, err = e.DataSources.TryLockDataSource(ctx, id, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "stale unlock must not release the new lease")
	unlock()
}
