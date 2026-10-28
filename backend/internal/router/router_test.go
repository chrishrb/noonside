package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAPI implements no operation.
type stubAPI struct{ generated.UnimplementedHandler }

func (stubAPI) HandleBearerAuth(ctx context.Context, _ generated.OperationName, _ generated.BearerAuth) (context.Context, error) {
	return ctx, nil
}

func (stubAPI) HandleApiKeyAuth(ctx context.Context, _ generated.OperationName, _ generated.ApiKeyAuth) (context.Context, error) {
	return ctx, nil
}

func (stubAPI) HandleHostedFtpHookAuth(ctx context.Context, _ generated.OperationName, _ generated.HostedFtpHookAuth) (context.Context, error) {
	return ctx, nil
}

// API paths answer JSON 404s; nothing else is served for unknown paths.
func TestNewRouterNotFound(t *testing.T) {
	h, err := NewRouter(stubAPI{}, nil, nil, SPA{})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"message":"not found"}`, rec.Body.String())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/abc/overview", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// GoTrue fetches its email templates from the API.
func TestNewRouterAuthTemplates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "recovery.html"), []byte("<p>{{ .ConfirmationURL }}</p>"), 0o600))
	h, err := NewRouter(stubAPI{}, nil, nil, SPA{AuthTemplates: dir})
	require.NoError(t, err)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	rec := get("/auth-templates/recovery.html")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "{{ .ConfirmationURL }}")
	assert.Equal(t, http.StatusNotFound, get("/auth-templates/").Code, "no listing")
	assert.NotEqual(t, http.StatusOK, get("/auth-templates/../router.go").Code)
}

// The Sentry tunnel is mounted only with a DSN; it rejects envelopes of other projects.
func TestNewRouterSentryTunnel(t *testing.T) {
	post := func(h http.Handler) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/diag",
			strings.NewReader(`{"dsn":"https://k@o1.ingest.de.sentry.io/2"}`+"\n{}")))
		return rec.Code
	}
	h, err := NewRouter(stubAPI{}, nil, nil, SPA{SentryDSN: "https://k@o1.ingest.de.sentry.io/1"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, post(h))

	h, err = NewRouter(stubAPI{}, nil, nil, SPA{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, post(h))
}
