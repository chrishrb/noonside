package errtrack

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getsentry/sentry-go"
)

// tagKeys are log attributes that also become tags, to filter by in Sentry.
var tagKeys = map[string]bool{"job": true, "kind": true, "component": true, "namespace": true}

// Handler passes every record to base and reports records at Error and above
// to Sentry: with the "error" attribute as the exception, the message as its
// context, and the other attributes as the "log" context.
func Handler(base slog.Handler) slog.Handler { return &handler{base: base} }

type handler struct {
	base  slog.Handler
	attrs []slog.Attr
	group string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.base.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.report(ctx, r)
	}
	return h.base.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.base = h.base.WithAttrs(attrs)
	c.attrs = append(append([]slog.Attr{}, h.attrs...), h.prefixed(attrs)...)
	return &c
}

func (h *handler) WithGroup(name string) slog.Handler {
	c := *h
	c.base = h.base.WithGroup(name)
	c.group = h.group + name + "."
	return &c
}

func (h *handler) prefixed(attrs []slog.Attr) []slog.Attr {
	if h.group == "" {
		return attrs
	}
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = slog.Attr{Key: h.group + a.Key, Value: a.Value}
	}
	return out
}

func (h *handler) report(ctx context.Context, r slog.Record) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	if hub.Client() == nil {
		return
	}
	// A clone, so concurrent records don't share one scope.
	hub = hub.Clone()

	attrs := append([]slog.Attr{}, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, h.prefixed([]slog.Attr{a})...)
		return true
	})
	var err error
	extra := map[string]any{}
	for _, a := range attrs {
		v := a.Value.Resolve()
		if e, ok := v.Any().(error); ok && (a.Key == "error" || a.Key == "err") {
			err = e
			continue
		}
		if tagKeys[a.Key] {
			hub.Scope().SetTag(a.Key, v.String())
		}
		extra[a.Key] = v.String()
	}
	hub.Scope().SetContext("log", sentry.Context{"message": r.Message, "attributes": extra})

	if err == nil {
		hub.CaptureMessage(r.Message)
		return
	}
	// The message names where it failed ("request failed", "scheduler job
	// failed"); the wrapped error keeps its type for grouping.
	hub.CaptureException(&logError{msg: r.Message, err: err})
}

// logError is the exception of a log record: "<message>: <error>".
type logError struct {
	msg string
	err error
}

func (e *logError) Error() string { return fmt.Sprintf("%s: %v", e.msg, e.err) }
func (e *logError) Unwrap() error { return e.err }
