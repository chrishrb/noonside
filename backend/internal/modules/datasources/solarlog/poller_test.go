package solarlog

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeUp(t *testing.T) {
	now := time.Now()
	assert.False(t, timeUp(context.Background(), now), "no deadline (CLI import)")

	ctx, cancel := context.WithDeadline(context.Background(), now.Add(10*time.Minute))
	defer cancel()
	assert.False(t, timeUp(ctx, now))
	assert.False(t, timeUp(ctx, now.Add(8*time.Minute-time.Second)))
	assert.True(t, timeUp(ctx, now.Add(8*time.Minute+time.Second)))
}
