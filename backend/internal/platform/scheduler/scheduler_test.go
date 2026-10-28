package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunDue(t *testing.T) {
	var order []string
	var bg sync.WaitGroup
	var done atomic.Bool
	s := New()
	s.Add(
		Job{Name: "starts", Run: func(context.Context, time.Time) error {
			order = append(order, "starts")
			bg.Add(1)
			go func() {
				defer bg.Done()
				time.Sleep(10 * time.Millisecond)
				done.Store(true)
			}()
			return nil
		}, Wait: bg.Wait},
		Job{Name: "second", Run: func(_ context.Context, deadline time.Time) error {
			order = append(order, "second")
			assert.True(t, deadline.After(time.Now()))
			return nil
		}},
	)
	s.RunDue(context.Background())

	assert.Equal(t, []string{"starts", "second"}, order)
	// RunDue returns only after the background work has finished.
	assert.True(t, done.Load())
}
