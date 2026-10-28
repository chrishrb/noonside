package errtrack

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (t *transport) transactions() (out []*sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.events {
		if e.Type == "transaction" {
			out = append(out, e)
		}
	}
	return out
}

// setupTracing is setup with every transaction sampled.
func setupTracing(t *testing.T) *transport {
	tr := setup(t)
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:           "https://key@o1.ingest.de.sentry.io/1",
		Transport:     tr,
		EnableTracing: true,
		TracesSampler: sampler(1, 1),
	}))
	return tr
}

func TestSampler(t *testing.T) {
	s := sampler(0.1, 0.01)
	req := sentry.StartSpan(context.Background(), "http.server")
	job := sentry.StartSpan(context.Background(), opJob)
	assert.InDelta(t, 0.1, s.Sample(sentry.SamplingContext{Span: req}), 1e-9)
	assert.InDelta(t, 0.01, s.Sample(sentry.SamplingContext{Span: job}), 1e-9)
	assert.InDelta(t, 1, s.Sample(sentry.SamplingContext{Span: req, ParentSampled: sentry.SampledTrue}), 1e-9)
	assert.InDelta(t, 0, s.Sample(sentry.SamplingContext{Span: req, ParentSampled: sentry.SampledFalse}), 1e-9)
}

func TestStartJob(t *testing.T) {
	tr := setupTracing(t)

	outer, finishOuter := StartJob(context.Background(), "data_sources")
	ctx, finish := StartJob(outer, "data_source solarlog_remote", "kind", "solarlog_remote")
	assert.NotSame(t, sentry.TransactionFromContext(outer), sentry.TransactionFromContext(ctx), "own transaction")
	slog.ErrorContext(ctx, "poll broke", "error", errors.New("x"))
	finish(errors.New("x"))
	finishOuter(nil)

	txs := tr.transactions()
	require.Len(t, txs, 2)
	assert.Equal(t, "job data_source solarlog_remote", txs[0].Transaction)
	assert.Equal(t, sentry.SpanStatusInternalError, txs[0].Contexts["trace"]["status"])
	assert.Equal(t, "job data_sources", txs[1].Transaction)
	assert.NotEqual(t, txs[0].Contexts["trace"]["trace_id"], txs[1].Contexts["trace"]["trace_id"], "own trace")

	events := tr.errors()
	require.Len(t, events, 1)
	assert.Equal(t, "data_source solarlog_remote", events[0].Tags["job"])
	assert.Equal(t, "solarlog_remote", events[0].Tags["kind"])
	assert.Equal(t, txs[0].Contexts["trace"]["trace_id"], events[0].Contexts["trace"]["trace_id"])
}

func TestTransport(t *testing.T) {
	tr := setupTracing(t)
	var traceHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceHeader = r.Header.Get(sentry.SentryTraceHeader)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	client := HTTPClient(0)

	// Without a transaction: no span, no new transaction.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/hook/secret?token=x", nil)
	res, err := client.Do(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Empty(t, tr.transactions())

	ctx, finish := StartJob(context.Background(), "webhooks")
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/hook/secret?token=x", nil)
	res, err = client.Do(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	finish(nil)

	assert.Empty(t, traceHeader, "no trace headers to third parties")
	txs := tr.transactions()
	require.Len(t, txs, 1)
	require.Len(t, txs[0].Spans, 1)
	span := txs[0].Spans[0]
	assert.Equal(t, "http.client", span.Op)
	assert.Equal(t, "POST "+srv.URL, span.Description, "no path or query")
	assert.Equal(t, http.StatusTeapot, span.Data["http.response.status_code"])
}
