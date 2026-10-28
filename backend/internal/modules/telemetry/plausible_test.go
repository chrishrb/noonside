package telemetry

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f(v float64) *float64 { return &v }

func reasons(rs []rejection) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.reason)
	}
	return out
}

func TestLimitsFor(t *testing.T) {
	sys := &systems.System{PeakPowerKWp: 20}
	assert.Equal(t, limits{powerW: 6500, energyWh: 60000}, limitsFor(systems.Inverter{PeakPowerKWp: 5}, sys))
	// the rated AC power wins when it is higher
	assert.Equal(t, limits{powerW: 7800, energyWh: 60000}, limitsFor(systems.Inverter{PeakPowerKWp: 5, MaxACPowerW: f(6000)}, sys))
	// no inverter kWp: the system's bounds it
	assert.Equal(t, limits{powerW: 26000, energyWh: 240000}, limitsFor(systems.Inverter{}, sys))
	assert.Equal(t, limits{}, limitsFor(systems.Inverter{}, &systems.System{}))
	assert.Equal(t, limits{powerW: 3900}, limitsFor(systems.Inverter{MaxACPowerW: f(3000)}, &systems.System{}))
}

func TestCheckPoint(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	l := limits{powerW: 6500, energyWh: 60000}

	t.Run("plausible values are unchanged", func(t *testing.T) {
		p := Point{Time: now, PacW: f(6400), PdcW: f(0), EdayWh: f(30000), UdcV: f(-1)}
		rs, drop := checkPoint(&p, l, now)
		assert.Empty(t, rs)
		assert.False(t, drop)
		assert.Equal(t, 6400.0, *p.PacW)
		assert.Equal(t, 0.0, *p.PdcW)
		assert.Equal(t, 30000.0, *p.EdayWh)
	})
	t.Run("future", func(t *testing.T) {
		p := Point{Time: now.Add(11 * time.Minute), PacW: f(100)}
		rs, drop := checkPoint(&p, l, now)
		assert.True(t, drop)
		assert.Equal(t, []string{RejectFuture}, reasons(rs))

		p = Point{Time: now.Add(9 * time.Minute), PacW: f(100)}
		rs, drop = checkPoint(&p, l, now)
		assert.False(t, drop, "clock slightly ahead")
		assert.Empty(t, rs)
	})
	t.Run("negative power", func(t *testing.T) {
		p := Point{Time: now, PacW: f(-5), PdcW: f(10), EdayWh: f(100)}
		rs, drop := checkPoint(&p, l, now)
		assert.False(t, drop)
		assert.Equal(t, []string{RejectNegativePower}, reasons(rs))
		assert.Nil(t, p.PacW)
		assert.Equal(t, 10.0, *p.PdcW, "the rest of the row is kept")
		assert.Equal(t, "pac_w", rs[0].sample["field"])
	})
	t.Run("power above the limit", func(t *testing.T) {
		p := Point{Time: now, PacW: f(6600), PdcW: f(90000)}
		rs, _ := checkPoint(&p, l, now)
		assert.Equal(t, []string{RejectPowerAboveCap, RejectPowerAboveCap}, reasons(rs))
		assert.Nil(t, p.PacW)
		assert.Nil(t, p.PdcW)
	})
	t.Run("no upper bound without kWp", func(t *testing.T) {
		p := Point{Time: now, PacW: f(1e6), EdayWh: f(1e7)}
		rs, _ := checkPoint(&p, limits{}, now)
		assert.Empty(t, rs)
		assert.NotNil(t, p.PacW)
	})
	t.Run("eday above the limit", func(t *testing.T) {
		p := Point{Time: now, PacW: f(100), EdayWh: f(60001)}
		rs, _ := checkPoint(&p, l, now)
		assert.Equal(t, []string{RejectEdayAboveCap}, reasons(rs))
		assert.Nil(t, p.EdayWh)
		assert.NotNil(t, p.PacW)
	})
}

func TestCheckDay(t *testing.T) {
	l := limits{powerW: 6500, energyWh: 60000}
	d := Day{Date: time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC), EnergyWh: 30000, PacMaxW: f(5000)}
	_, bad := checkDay(&d, l)
	assert.False(t, bad)
	assert.NotNil(t, d.PacMaxW)

	d.EnergyWh = 60001
	r, bad := checkDay(&d, l)
	require.True(t, bad)
	assert.Equal(t, RejectDailyAboveCap, r.reason)
	assert.Equal(t, "2026-06-14", r.sample["date"])

	d.EnergyWh = -1
	r, bad = checkDay(&d, l)
	require.True(t, bad)
	assert.Equal(t, RejectNegativeEnergy, r.reason)

	d.EnergyWh, d.PacMaxW = 1000, f(9000)
	_, bad = checkDay(&d, l)
	assert.False(t, bad)
	assert.Nil(t, d.PacMaxW, "an implausible peak is dropped, the day kept")
}
