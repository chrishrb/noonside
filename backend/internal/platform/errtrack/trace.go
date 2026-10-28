package errtrack

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

// opJob is the operation of a scheduler job's or data source run's
// transaction; the sampler gives these their own rate.
const opJob = "function"

// sampler follows the caller's decision (the browser's trace, a parent span)
// and otherwise samples API requests at rate and jobs at jobRate: the
// scheduler runs every job each minute, mostly doing nothing.
func sampler(rate, jobRate float64) sentry.TracesSampler {
	return func(c sentry.SamplingContext) float64 {
		switch c.ParentSampled {
		case sentry.SampledTrue:
			return 1
		case sentry.SampledFalse:
			return 0
		}
		if c.Span.Op == opJob {
			return jobRate
		}
		return rate
	}
}

// StartJob starts the transaction of a background run (a scheduler job, a
// data source poll) in its own trace and with its own scope, also when ctx
// belongs to another run. finish records err as the run's status. Errors
// logged with the returned context carry the job tag and the trace.
//
//	ctx, finish := errtrack.StartJob(ctx, "alerting")
//	err := run(ctx)
//	finish(err)
func StartJob(ctx context.Context, name string, tags ...string) (context.Context, func(error)) {
	ctx = detached{ctx}
	hub := sentry.CurrentHub().Clone()
	hub.Scope().SetTag("job", name)
	for i := 0; i+1 < len(tags); i += 2 {
		hub.Scope().SetTag(tags[i], tags[i+1])
	}
	ctx = sentry.SetHubOnContext(ctx, hub)
	tx := sentry.StartTransaction(ctx, "job "+name, sentry.WithOpName(opJob), sentry.WithTransactionSource(sentry.SourceTask))
	return tx.Context(), func(err error) {
		tx.Status = sentry.SpanStatusOK
		if err != nil {
			tx.Status = sentry.SpanStatusInternalError
		}
		tx.Finish()
	}
}

// detached keeps ctx's cancellation and deadline but none of its values, so
// a job started from another job's context is not a span of it.
type detached struct{ context.Context }

func (detached) Value(any) any { return nil }

// HTTPClient is an http.Client with the given timeout whose requests are
// spans of the request or job in their context (Transport).
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport(nil)}
}

// Transport records outgoing requests as "http.client" spans. The span
// names only method and host: paths and query strings of third-party URLs
// carry tokens (webhooks, ntfy topics, API keys). It adds no trace headers,
// since the requests leave our system. base nil means
// http.DefaultTransport.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base}
}

type roundTripper struct{ base http.RoundTripper }

func (t roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if sentry.SpanFromContext(r.Context()) == nil {
		return t.base.RoundTrip(r)
	}
	span := sentry.StartSpan(r.Context(), "http.client", sentry.WithDescription(r.Method+" "+r.URL.Scheme+"://"+r.URL.Host))
	span.SetData("http.request.method", r.Method)
	span.SetData("server.address", r.URL.Hostname())
	defer span.Finish()
	res, err := t.base.RoundTrip(r.WithContext(span.Context()))
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return nil, err
	}
	span.Status = sentry.HTTPtoSpanStatus(res.StatusCode)
	span.SetData("http.response.status_code", res.StatusCode)
	return res, nil
}
