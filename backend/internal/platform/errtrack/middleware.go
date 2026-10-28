package errtrack

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/ogen-go/ogen/middleware"
)

// Middleware gives each request its own Sentry scope and transaction
// (continuing the browser's trace) and turns a panic into a logged error
// and a 500, instead of a dropped connection.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.CurrentHub().Clone()
		ctx := sentry.SetHubOnContext(r.Context(), hub)
		hub.Scope().SetRequest(r)
		tx := sentry.StartTransaction(ctx, r.Method+" "+routeName(r.URL.Path),
			sentry.ContinueTrace(hub, r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(sentry.SourceURL),
		)
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel passed to panic
					panic(v)
				}
				slog.ErrorContext(tx.Context(), "panic", "error", fmt.Errorf("panic: %v", v))
				if sw.status == 0 {
					sw.Header().Set("Content-Type", "application/json")
					sw.WriteHeader(http.StatusInternalServerError)
					_, _ = sw.Write([]byte(`{"message":"internal server error"}`))
				}
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			tx.Status = sentry.HTTPtoSpanStatus(status)
			tx.SetData("http.response.status_code", status)
			tx.Finish()
		}()
		next.ServeHTTP(sw, r.WithContext(tx.Context()))
	})
}

// routeName is the transaction name until the API names it after its
// operation (Operation): API paths carry IDs and tokens.
func routeName(p string) string {
	if strings.HasPrefix(p, "/api/") {
		return "/api/*"
	}
	return ScrubPath(p)
}

// Operation is an ogen middleware that names the transaction and the
// request's events after the API operation (e.g. "getOverview").
func Operation(req middleware.Request, next middleware.Next) (middleware.Response, error) {
	if tx := sentry.TransactionFromContext(req.Context); tx != nil {
		tx.Name = req.Raw.Method + " " + req.OperationID
		tx.Source = sentry.SourceRoute
	}
	if hub := sentry.GetHubFromContext(req.Context); hub != nil {
		hub.Scope().SetTag("operation", req.OperationID)
	}
	return next(req)
}

// Recover, deferred directly, logs a panic as an error (and so reports it)
// instead of crashing the process. Use it in goroutines and jobs:
//
//	defer errtrack.Recover(ctx, logger, "data source run panicked", "data_source_id", id)
func Recover(ctx context.Context, logger *slog.Logger, msg string, args ...any) {
	if v := recover(); v != nil {
		logger.ErrorContext(ctx, msg, append(args, "error", fmt.Errorf("panic: %v", v))...)
	}
}

// statusWriter records the response status. Unwrap keeps
// http.ResponseController (flush, deadlines) working for streamed responses.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
