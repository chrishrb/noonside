// Package errtrack reports errors and traces to Sentry. Without a DSN it is
// off: Init does nothing, and the middleware and log handler only recover
// panics and pass records on.
//
// Errors reach Sentry in one way: every slog record at level Error becomes an
// event (Handler), with the request's scope when logged with its context.
// Expected failures that the user can fix themselves (an unreachable data
// source, a wrong password) belong at Warn. Events carry only the user ID:
// headers with credentials, query strings, bodies and tokens in paths are
// removed (scrub).
package errtrack

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config selects the Sentry project. An empty DSN disables reporting.
type Config struct {
	DSN         string
	Environment string
	// TracesSampleRate is the share of API requests traced (unless the
	// browser decided), JobTracesSampleRate that of scheduler jobs and data
	// source runs.
	TracesSampleRate    float64
	JobTracesSampleRate float64
}

// flushTimeout bounds how long Flush blocks, e.g. on shutdown.
const flushTimeout = 2 * time.Second

// Init starts the Sentry client. The returned func sends what is queued; call
// it before the process exits.
func Init(cfg Config) (flush func(), err error) {
	if cfg.DSN == "" {
		return func() {}, nil
	}
	err = sentry.Init(sentry.ClientOptions{
		Dsn:                   cfg.DSN,
		Environment:           cfg.Environment,
		Release:               Release(),
		SendDefaultPII:        false,
		EnableTracing:         cfg.TracesSampleRate > 0 || cfg.JobTracesSampleRate > 0,
		TracesSampler:         sampler(cfg.TracesSampleRate, cfg.JobTracesSampleRate),
		BeforeSend:            func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event { return scrub(e) },
		BeforeSendTransaction: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event { return scrub(e) },
	})
	return Flush, err
}

// Enabled reports whether events are sent. The SDK's default hub has a
// client without a DSN (sentry-go 0.50+), which sends nothing.
func Enabled() bool {
	c := sentry.CurrentHub().Client()
	return c != nil && c.Options().Dsn != ""
}

// Flush waits until queued events are sent (at most flushTimeout).
func Flush() { sentry.Flush(flushTimeout) }

// version is set at link time where the build has no git checkout (the
// backend/Dockerfile: -X …/errtrack.version=$APP_VERSION).
var version string

// Release is the commit the binary was built from, the same SHA the SPA
// reports, or "" outside a git checkout.
func Release() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// SetUser sets the user of the request's events. Only the ID, never the
// email.
func SetUser(ctx context.Context, id string) {
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		hub.Scope().SetUser(sentry.User{ID: id})
	}
}

// SetTag tags the request's events.
func SetTag(ctx context.Context, key, value string) {
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		hub.Scope().SetTag(key, value)
	}
}
