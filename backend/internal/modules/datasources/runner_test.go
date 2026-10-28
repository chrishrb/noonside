package datasources

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/adapter"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDue(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	base := DataSource{PollInterval: 5 * time.Minute, CreatedAt: now.Add(-48 * time.Hour)}

	ds := base
	assert.True(t, due(ds, now), "never ran")

	ds.LastRunAt = ago(time.Minute)
	assert.False(t, due(ds, now))
	ds.LastRunAt = ago(5 * time.Minute)
	assert.True(t, due(ds, now))

	// failing for 2h -> wait 30min between attempts
	ds.LastError = "boom"
	ds.LastSuccessAt = ago(2 * time.Hour)
	ds.LastRunAt = ago(10 * time.Minute)
	assert.False(t, due(ds, now))
	ds.LastRunAt = ago(31 * time.Minute)
	assert.True(t, due(ds, now))

	// backoff is capped at 1h
	ds.LastSuccessAt = nil
	ds.LastRunAt = ago(61 * time.Minute)
	assert.True(t, due(ds, now))
}

func TestDueHosted(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	base := DataSource{Kind: KindSolarLogHosted, PollInterval: 5 * time.Minute, CreatedAt: now.Add(-48 * time.Hour)}

	ds := base
	assert.False(t, due(ds, now), "no upload yet: no polling")
	ds.LastRunAt = ago(time.Hour)
	assert.False(t, due(ds, now), "not on the interval")

	ds.UploadAt = ago(10 * time.Second)
	assert.False(t, due(ds, now), "waits for the upload session to end")
	ds.UploadAt = ago(30 * time.Second)
	assert.True(t, due(ds, now))
	ds.LastRunAt = ago(time.Minute)
	assert.True(t, due(ds, now), "an upload after the last run runs at once, regardless of the interval")

	ds.ImportedUploadAt = ds.UploadAt
	assert.False(t, due(ds, now), "imported")
	ds.UploadAt = ago(40 * time.Second)
	ds.ImportedUploadAt = ago(2 * time.Minute)
	assert.True(t, due(ds, now), "a newer upload")

	// A failed import is retried with the backoff, not at every tick.
	ds.LastError = "boom"
	ds.LastSuccessAt = ago(2 * time.Hour)
	ds.LastRunAt = ago(10 * time.Minute)
	assert.False(t, due(ds, now))
	ds.LastRunAt = ago(31 * time.Minute)
	assert.True(t, due(ds, now))
	ds.ImportedUploadAt = ds.UploadAt
	assert.False(t, due(ds, now), "nothing pending")
}

// fakeRunStore implements the data source methods the runner uses.
type fakeRunStore struct {
	sources  []DataSource
	mu       sync.Mutex
	recorded map[uuid.UUID]error
	imported map[uuid.UUID]time.Time
}

func (f *fakeRunStore) ListEnabledDataSources(context.Context) ([]DataSource, error) {
	return f.sources, nil
}

func (f *fakeRunStore) TryLockDataSource(context.Context, uuid.UUID, time.Duration) (func(), bool, error) {
	return func() {}, true, nil
}

func (f *fakeRunStore) RecordDataSourceRun(_ context.Context, id uuid.UUID, _ time.Time, runErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded[id] = runErr
	return nil
}

func (f *fakeRunStore) MarkUploadImported(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imported[id] = at
	return nil
}

type pollerFunc func(ctx context.Context) error

func (p pollerFunc) Poll(ctx context.Context) error { return p(ctx) }

func TestStartDue(t *testing.T) {
	now := time.Now()
	recent := now.Add(-time.Minute)
	upload := now.Add(-time.Minute)
	dueSrc := DataSource{ID: uuid.New(), PollInterval: 5 * time.Minute}
	notDue := DataSource{ID: uuid.New(), PollInterval: 5 * time.Minute, LastRunAt: &recent}
	hosted := DataSource{ID: uuid.New(), Kind: KindSolarLogHosted, PollInterval: 5 * time.Minute, LastRunAt: &recent, UploadAt: &upload}
	failing := DataSource{ID: uuid.New(), Kind: KindSolarLogHosted, PollInterval: 5 * time.Minute, UploadAt: &upload}
	st := &fakeRunStore{sources: []DataSource{dueSrc, notDue, hosted, failing}, recorded: map[uuid.UUID]error{}, imported: map[uuid.UUID]time.Time{}}

	var polled atomic.Int32
	r := newRunner(st, func(ds DataSource) (adapter.Poller, error) {
		return pollerFunc(func(context.Context) error {
			time.Sleep(10 * time.Millisecond)
			polled.Add(1)
			if ds.ID == failing.ID {
				return errors.New("boom")
			}
			return nil
		}), nil
	})
	r.startDue(context.Background())
	r.wg.Wait()

	assert.Equal(t, int32(3), polled.Load())
	assert.Contains(t, st.recorded, dueSrc.ID)
	assert.NotContains(t, st.recorded, notDue.ID)
	assert.Equal(t, upload, st.imported[hosted.ID], "the upload read at the start counts as imported")
	assert.NotContains(t, st.imported, failing.ID, "a failed import stays pending")
	assert.NotContains(t, st.imported, dueSrc.ID)
}
