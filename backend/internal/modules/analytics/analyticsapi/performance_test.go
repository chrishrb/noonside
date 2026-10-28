package analyticsapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerformanceRatio(t *testing.T) {
	var a prSum
	a.add(40, 5) // 40 kWh from 5 kWh/m² on 10 kWp: 80 %
	a.add(0, 1)  // a dead day counts
	pr, ok := a.pct(10)
	require.True(t, ok)
	assert.InDelta(t, 40.0/60*100, pr, 1e-9)
	_, ok = prSum{kwh: 5}.pct(10)
	assert.False(t, ok, "no irradiation")
}

func TestPRTrend(t *testing.T) {
	year := func(pr float64, days int) *prSum {
		return &prSum{kwh: pr / 100 * 1000 * 10, irr: 1000, days: days}
	}
	// 82 → 81.5 → 81 % over complete years; the partial current year is left out.
	years := map[int]*prSum{2023: year(82, 365), 2024: year(81.5, 366), 2025: year(81, 365), 2026: year(70, 120)}
	tr := prTrend(years, 10)
	require.NotNil(t, tr)
	assert.InDelta(t, -0.5, *tr, 1e-9)
	delete(years, 2023)
	assert.Nil(t, prTrend(years, 10), "fewer than three years")
}
