package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePruneStore records DeleteMeasurementsBefore calls and reports done
// once the system's pending batches are used up.
type fakePruneStore struct {
	systems []systems.SystemWithRole
	mu      sync.Mutex
	pruned  map[uuid.UUID][]time.Time
	batches map[uuid.UUID]int
}

func (f *fakePruneStore) ListSystems(context.Context, *uuid.UUID) ([]systems.SystemWithRole, error) {
	return f.systems, nil
}

func (f *fakePruneStore) DeleteMeasurementsBefore(_ context.Context, id uuid.UUID, before time.Time, _ int) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruned[id] = append(f.pruned[id], before)
	if f.batches[id] > 0 {
		f.batches[id]--
	}
	return 1, f.batches[id] == 0, nil
}

func TestPruneMinuteData(t *testing.T) {
	two := 2
	keep := systems.SystemWithRole{System: systems.System{ID: uuid.New(), Timezone: "Europe/Berlin"}}
	limited := systems.SystemWithRole{System: systems.System{ID: uuid.New(), Timezone: "Europe/Berlin", MinuteRetentionYears: &two}}
	st := &fakePruneStore{systems: []systems.SystemWithRole{keep, limited},
		pruned: map[uuid.UUID][]time.Time{}, batches: map[uuid.UUID]int{limited.ID: 3}}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	logger := slog.Default()

	// all batches in one run, then a single check per run
	pruneMinuteData(context.Background(), st, clock, now.Add(time.Minute), logger)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	require.Len(t, st.pruned[limited.ID], 3)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, berlin), st.pruned[limited.ID][0])
	assert.NotContains(t, st.pruned, keep.ID)
	pruneMinuteData(context.Background(), st, clock, now.Add(time.Minute), logger)
	assert.Len(t, st.pruned[limited.ID], 4)

	// a run stops at its deadline; the next one continues
	st.pruned[limited.ID], st.batches[limited.ID] = nil, 5
	clock = func() time.Time { now = now.Add(20 * time.Second); return now }
	deadline := clock().Add(time.Minute)
	pruneMinuteData(context.Background(), st, clock, deadline, logger)
	assert.Len(t, st.pruned[limited.ID], 2)
	assert.Equal(t, 3, st.batches[limited.ID])
}
