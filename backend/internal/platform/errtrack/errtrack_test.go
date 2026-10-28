package errtrack

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transport records the events instead of sending them.
type transport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *transport) Configure(sentry.ClientOptions)        {}
func (t *transport) Flush(time.Duration) bool              { return true }
func (t *transport) FlushWithContext(context.Context) bool { return true }
func (t *transport) Close()                                {}
func (t *transport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	t.events = append(t.events, e)
	t.mu.Unlock()
}
func (t *transport) errors() (out []*sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.events {
		if e.Type != "transaction" {
			out = append(out, e)
		}
	}
	return out
}

func setup(t *testing.T) *transport {
	tr := &transport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:        "https://key@o1.ingest.de.sentry.io/1",
		Transport:  tr,
		BeforeSend: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event { return scrub(e) },
	}))
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })
	prev := slog.Default()
	slog.SetDefault(slog.New(Handler(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return tr
}

func TestInitWithoutDSN(t *testing.T) {
	flush, err := Init(Config{})
	require.NoError(t, err)
	flush()
	assert.False(t, Enabled())
}

func TestMiddlewareRecoversPanic(t *testing.T) {
	tr := setup(t)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetUser(r.Context(), "user-1")
		panic("boom")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/s0p_secret/live?token=x", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"message":"internal server error"}`, rec.Body.String())
	events := tr.errors()
	require.Len(t, events, 1)
	e := events[0]
	assert.Equal(t, "user-1", e.User.ID)
	require.NotEmpty(t, e.Exception)
	assert.Contains(t, e.Exception[len(e.Exception)-1].Value+e.Exception[0].Value, "boom")
	require.NotNil(t, e.Request)
	assert.NotContains(t, e.Request.URL, "s0p_secret")
	assert.NotContains(t, e.Request.URL, "token=")
	assert.Empty(t, e.Request.QueryString)
	for k := range e.Request.Headers {
		assert.NotContains(t, []string{"Authorization", "X-Api-Key"}, http.CanonicalHeaderKey(k))
	}
}

func TestHandlerReportsErrors(t *testing.T) {
	tr := setup(t)
	logger := slog.Default().With("component", "scheduler")
	logger.Warn("data source run failed", "error", errors.New("ftp down"))
	assert.Empty(t, tr.errors(), "Warn is not reported")

	logger.Error("scheduler job failed", "job", "alerts", "error", errors.New("db gone"))
	logger.Error("plain message")
	events := tr.errors()
	require.Len(t, events, 2)
	assert.Equal(t, "alerts", events[0].Tags["job"])
	assert.Equal(t, "scheduler", events[0].Tags["component"])
	var values []string
	for _, x := range events[0].Exception {
		values = append(values, x.Value)
	}
	assert.Contains(t, values, "scheduler job failed: db gone")
	assert.Equal(t, "plain message", events[1].Message)
}

func TestRecover(t *testing.T) {
	tr := setup(t)
	func() {
		defer Recover(context.Background(), slog.Default(), "job panicked", "job", "x")
		panic("oops")
	}()
	require.Len(t, tr.errors(), 1)
	assert.Equal(t, "x", tr.errors()[0].Tags["job"])
}

func TestScrubPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/v1/public/s0p_abc/overview": "/api/v1/public/[token]/overview",
		"/api/v1/invitations/abc/accept":  "/api/v1/invitations/[token]/accept",
		"/p/s0p_abc":                      "/p/[token]",
		"/api/v1/systems/123":             "/api/v1/systems/123",
	} {
		assert.Equal(t, want, ScrubPath(in), in)
	}
}
