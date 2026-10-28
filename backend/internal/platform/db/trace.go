package db

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
)

// tracer records queries, batches, copies and new connections as Sentry
// spans of the request or job in their context (none without one). The
// description is the SQL text, which holds no values: queries take them as
// parameters.
type tracer struct{}

var (
	_ pgx.QueryTracer    = tracer{}
	_ pgx.BatchTracer    = tracer{}
	_ pgx.CopyFromTracer = tracer{}
	_ pgx.ConnectTracer  = tracer{}
)

func startSpan(ctx context.Context, op, description string) context.Context {
	if sentry.SpanFromContext(ctx) == nil {
		return ctx
	}
	span := sentry.StartSpan(ctx, op, sentry.WithDescription(description))
	span.SetData("db.system", "postgresql")
	return span.Context()
}

// endSpan finishes the span startSpan put into ctx. Callers pass the ctx
// startSpan returned, so the current span is that one.
func endSpan(ctx context.Context, err error) {
	span := sentry.SpanFromContext(ctx)
	if span == nil || span.IsTransaction() {
		return
	}
	span.Status = sentry.SpanStatusOK
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	}
	span.Finish()
}

func (tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return startSpan(ctx, "db.sql.query", oneLine(d.SQL))
}

func (tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	endSpan(ctx, d.Err)
}

func (tracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceBatchStartData) context.Context {
	desc := "batch"
	if d.Batch != nil && len(d.Batch.QueuedQueries) > 0 {
		desc = oneLine(d.Batch.QueuedQueries[0].SQL)
	}
	return startSpan(ctx, "db.sql.batch", desc)
}

func (tracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}

func (tracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceBatchEndData) {
	endSpan(ctx, d.Err)
}

func (tracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceCopyFromStartData) context.Context {
	return startSpan(ctx, "db.sql.copy", "COPY "+d.TableName.Sanitize())
}

func (tracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceCopyFromEndData) {
	endSpan(ctx, d.Err)
}

func (tracer) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	return startSpan(ctx, "db.connect", "connect")
}

func (tracer) TraceConnectEnd(ctx context.Context, d pgx.TraceConnectEndData) {
	endSpan(ctx, d.Err)
}

// oneLine collapses the whitespace of hand-written multi-line SQL.
func oneLine(sql string) string { return strings.Join(strings.Fields(sql), " ") }
