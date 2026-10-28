package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *transport) Configure(sentry.ClientOptions)        {}
func (t *transport) Flush(time.Duration) bool              { return true }
func (t *transport) FlushWithContext(context.Context) bool { return true }
func (t *transport) Close()                                {}
func (t *transport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	t.events = append(t.events, e)
	t.mu.Unlock()
}

func TestTracer(t *testing.T) {
	tr := &transport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Dsn:              "https://key@o1.ingest.de.sentry.io/1",
		Transport:        tr,
		EnableTracing:    true,
		TracesSampleRate: 1,
	}))
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	// Without a transaction nothing is recorded.
	ctx := tracer{}.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	assert.Nil(t, sentry.SpanFromContext(ctx))
	tracer{}.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	tx := sentry.StartTransaction(context.Background(), "job x")
	ctx = tracer{}.TraceQueryStart(tx.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT id\n\t  FROM systems\n\tWHERE id = $1"})
	tracer{}.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("boom")})
	ctx = tracer{}.TraceCopyFromStart(tx.Context(), nil, pgx.TraceCopyFromStartData{TableName: pgx.Identifier{"measurement_days"}})
	tracer{}.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})
	tx.Finish()

	require.Len(t, tr.events, 1)
	spans := tr.events[0].Spans
	require.Len(t, spans, 2)
	assert.Equal(t, "db.sql.query", spans[0].Op)
	assert.Equal(t, "SELECT id FROM systems WHERE id = $1", spans[0].Description)
	assert.Equal(t, sentry.SpanStatusInternalError, spans[0].Status)
	assert.Equal(t, "postgresql", spans[0].Data["db.system"])
	assert.Equal(t, `COPY "measurement_days"`, spans[1].Description)
	assert.Equal(t, sentry.SpanStatusOK, spans[1].Status)
}
