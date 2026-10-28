package analyticsapi

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func TestNewPeriod(t *testing.T) {
	d := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	first := time.Date(2010, 6, 26, 0, 0, 0, 0, berlin)

	p, err := newPeriod("day", d, first, berlin)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 25, 0, 0, 0, 0, berlin), p.start)
	assert.Equal(t, time.Date(2026, 9, 26, 0, 0, 0, 0, berlin), p.end)

	p, _ = newPeriod("month", d, first, berlin)
	assert.Len(t, p.buckets(), 30)

	p, _ = newPeriod("year", d, first, berlin)
	assert.Len(t, p.buckets(), 12)

	p, _ = newPeriod("total", d, first, berlin)
	assert.Equal(t, 2010, p.start.Year())
	assert.Len(t, p.buckets(), 17)

	_, err = newPeriod("week", d, first, berlin)
	assert.Error(t, err)
}

func TestDSTDayIs23Hours(t *testing.T) {
	p, _ := newPeriod("day", time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), time.Time{}, berlin)
	assert.Equal(t, 23*time.Hour, p.end.Sub(p.start))
}

func TestTargetKWh(t *testing.T) {
	// Values from the current Solar-Log site: 78.36 kWp * 950 kWh/kWp * 10% in September.
	targets := map[int]float64{9: 7444.2, 10: 4466.52}
	sep1 := time.Date(2026, 9, 1, 0, 0, 0, 0, berlin)

	day := targetKWh(targets, time.Date(2026, 9, 25, 0, 0, 0, 0, berlin), time.Date(2026, 9, 26, 0, 0, 0, 0, berlin))
	assert.InDelta(t, 248.14, day, 0.01)

	// "Soll (auflaufend)" through the 25th
	toDate := targetKWh(targets, sep1, time.Date(2026, 9, 26, 0, 0, 0, 0, berlin))
	assert.InDelta(t, 6203.50, toDate, 0.01)

	// spans a month boundary
	span := targetKWh(targets, time.Date(2026, 9, 30, 0, 0, 0, 0, berlin), time.Date(2026, 10, 2, 0, 0, 0, 0, berlin))
	assert.InDelta(t, 7444.2/30+4466.52/31, span, 1e-9)

	assert.Zero(t, targetKWh(nil, sep1, sep1.AddDate(0, 1, 0)))
	assert.False(t, math.IsNaN(targetKWh(targets, sep1, sep1)))
}

func TestTargetKWhDSTMonth(t *testing.T) {
	targets := map[int]float64{3: 3100}
	mar := time.Date(2026, 3, 1, 0, 0, 0, 0, berlin)
	assert.InDelta(t, 3100, targetKWh(targets, mar, mar.AddDate(0, 1, 0)), 1e-9)
}
