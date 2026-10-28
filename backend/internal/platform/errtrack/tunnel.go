package errtrack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

// TunnelPath is where the SPA sends its Sentry envelopes (the SDK's tunnel
// option, web/src/lib/sentry.ts): same origin, so ad blockers that block
// Sentry's ingest host don't drop errors, replays and feedback. The name
// avoids "sentry" and "tunnel", which filter lists match.
const TunnelPath = "/api/v1/diag"

// maxEnvelopeBytes is above a replay segment or a feedback screenshot.
const maxEnvelopeBytes = 5 << 20

// Tunnel forwards envelopes for the project of dsn (the SPA's) to Sentry and
// refuses any other DSN, so it can't relay to other projects. The client's
// IP is not forwarded.
func Tunnel(dsn string, client *http.Client) (http.Handler, error) {
	want, err := sentry.NewDsn(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse sentry dsn: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	target := want.GetAPIURL().String()
	logger := slog.Default()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEnvelopeBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "envelope too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// The first line is the envelope header; with a tunnel the SDK puts
		// its DSN there.
		line, _, _ := bytes.Cut(body, []byte("\n"))
		var header struct {
			DSN string `json:"dsn"`
		}
		if json.Unmarshal(line, &header) != nil {
			http.Error(w, "bad envelope", http.StatusBadRequest)
			return
		}
		got, err := sentry.NewDsn(header.DSN)
		if err != nil || got.GetHost() != want.GetHost() || got.GetProjectID() != want.GetProjectID() {
			http.Error(w, "unknown dsn", http.StatusBadRequest)
			return
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/x-sentry-envelope")
		resp, err := client.Do(req)
		if err != nil {
			// Warn, not Error: an Error record would be sent to Sentry too.
			logger.WarnContext(r.Context(), "sentry tunnel: forward failed", "error", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// The SDK backs off on these.
		for _, h := range []string{"Retry-After", "X-Sentry-Rate-Limits"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<10))
	}), nil
}
