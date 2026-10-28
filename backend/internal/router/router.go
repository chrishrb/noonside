package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/media"
)

const (
	apiPrefix    = "/api/v1"
	maxBodyBytes = 32 << 20
)

// SPA configures what the API serves for the web app, which runs in its own
// container (web/Dockerfile).
type SPA struct {
	// SentryDSN is the DSN of the app's Sentry project. If set, its
	// envelopes are forwarded from errtrack.TunnelPath.
	SentryDSN string
	// Media serves uploaded pictures under media.PathPrefix (nil: not
	// served).
	Media *media.Dir
	// AuthTemplates is the directory of the Auth server's email templates
	// (`noonside email supabase-templates`), served under authTemplatesPath
	// for GoTrue to fetch (GOTRUE_MAILER_TEMPLATES_*); empty: not served.
	AuthTemplates string
}

// authTemplatesPath serves SPA.AuthTemplates. The templates hold no secrets.
const authTemplatesPath = "/auth-templates/"

// NewRouter mounts the API under /api/v1 next to the operational endpoints.
// Browsers may call it cross-origin from allowedOrigins ("*" allows any).
// API is the composed handler of all API operations.
type API interface {
	generated.Handler
	generated.SecurityHandler
}

func NewRouter(handler API, ping func(ctx context.Context) error, allowedOrigins []string, web SPA) (http.Handler, error) {
	apiServer, err := generated.NewServer(
		handler,
		handler,
		generated.WithPathPrefix(apiPrefix),
		generated.WithErrorHandler(httpapi.ErrorHandler),
		generated.WithMiddleware(errtrack.Operation),
		generated.WithNotFound(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAPI server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.yaml", httpapi.HandleOpenAPISpec)
	mux.Handle(apiPrefix+"/", http.MaxBytesHandler(apiServer, maxBodyBytes))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// The deploy waits for the new commit here.
		if rel := errtrack.Release(); rel != "" {
			w.Header().Set("Noonside-Release", rel)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	if web.Media != nil {
		mux.Handle("GET "+media.PathPrefix, web.Media.Handler())
	}
	if web.AuthTemplates != "" {
		mux.Handle("GET "+authTemplatesPath, http.StripPrefix(authTemplatesPath, noListing(http.FileServer(http.Dir(web.AuthTemplates)))))
	}

	h := errtrack.Middleware(withCORS(mux, allowedOrigins))
	if web.SentryDSN == "" {
		return h, nil
	}
	tunnel, err := errtrack.Tunnel(web.SentryDSN, nil)
	if err != nil {
		return nil, err
	}
	// Outside errtrack.Middleware: forwarding an event must not create a
	// transaction (an event) of its own.
	outer := http.NewServeMux()
	outer.Handle("POST "+errtrack.TunnelPath, tunnel)
	outer.Handle("/", h)
	return outer, nil
}

// noListing answers 404 for directories instead of listing them.
func noListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
