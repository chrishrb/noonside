// Package httpapi holds what the modules' HTTP handlers share: the OpenAPI
// spec and its generated server, error responses, authorization and
// converters.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"
)

// Error is an error with an HTTP status and a client-safe message.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func BadRequest(msg string) error   { return &Error{http.StatusBadRequest, msg} }
func Unauthorized(msg string) error { return &Error{http.StatusUnauthorized, msg} }
func Forbidden(msg string) error    { return &Error{http.StatusForbidden, msg} }
func NotFound(msg string) error     { return &Error{http.StatusNotFound, msg} }
func Conflict(msg string) error     { return &Error{http.StatusConflict, msg} }
func BadGateway(msg string) error   { return &Error{http.StatusBadGateway, msg} }

// StatusError is a domain error that knows its status (e.g. invalid ingest
// input); its message is shown to the client.
type StatusError interface {
	error
	StatusCode() int
}

// logger is looked up per call: a package-level slog.Default() would be the
// one from before cmd installs its handler, which loses the level (and so
// Sentry reporting) on the way through the log package.
func logger() *slog.Logger { return slog.Default().With("namespace", "api.handler") }

// NewError maps handler and ogen errors to responses. Unknown errors are
// logged and returned as 500 without details.
func NewError(ctx context.Context, err error) *generated.ErrorStatusCode {
	status, msg := http.StatusInternalServerError, "internal server error"

	var (
		ae  *Error
		ste StatusError
		se  *ogenerrors.SecurityError
		de  *ogenerrors.DecodeRequestError
		dpe *ogenerrors.DecodeParamsError
		vle *validate.Error
	)
	switch {
	case errors.As(err, &ae):
		status, msg = ae.Status, ae.Msg
	case errors.As(err, &se):
		status, msg = http.StatusUnauthorized, "unauthorized"
	case errors.As(err, &ste):
		status, msg = ste.StatusCode(), ste.Error()
	case errors.As(err, &de), errors.As(err, &dpe), errors.As(err, &vle):
		status, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, db.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, db.ErrConflict):
		status, msg = http.StatusConflict, "conflict"
	default:
		logger().ErrorContext(ctx, "request failed", slog.Any("error", err))
	}
	return &generated.ErrorStatusCode{StatusCode: status, Response: generated.Error{Message: msg}}
}

// ErrorHandler renders errors raised outside handlers (e.g. while decoding
// parameters) in the same format as handler errors.
func ErrorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	e := NewError(ctx, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.StatusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": e.Response.Message})
}
