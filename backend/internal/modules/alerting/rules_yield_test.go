package alerting

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ofKind(fs []Finding, kind string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestLowYield(t *testing.T) {
	a, b := inverter(5), inverter(5)
	yesterday := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	first := yesterday.AddDate(0, 0, -30)
	setup := func(whA, whB float64) *Snapshot {
		s := snapshot(summerNoon, a, b)
		s.YesterdayDate, s.FirstDay = yesterday, &first
		s.Yesterday = map[uuid.UUID]float64{a.ID: whA, b.ID: whB}
		s.Expected = map[string]weather.ExpectedDay{"2026-06-14": {KWhPerKWp: 5}} // 25 kWh per inverter
		return s
	}
	eval := func(s *Snapshot) ([]Finding, bool) {
		ev := Evaluate(s, rules(t))
		return ofKind(ev.Findings, KindLowYield), ev.Held[KindLowYield]
	}

	t.Run("system below", func(t *testing.T) {
		fs, _ := eval(setup(10000, 10000)) // 40 %
		require.Len(t, fs, 1)
		assert.Empty(t, fs[0].SubjectKey)
		assert.Equal(t, map[string]any{"percent": 40, "day": "2026-06-14", "actual_kwh": 20.0, "expected_kwh": 50.0}, fs[0].Details)
	})
	t.Run("one inverter below", func(t *testing.T) {
		fs, _ := eval(setup(25000, 10000)) // system 70 %, b 40 %
		require.Len(t, fs, 1)
		assert.Equal(t, b.ID, *fs[0].InverterID)
	})
	t.Run("as expected", func(t *testing.T) {
		fs, held := eval(setup(24000, 22000))
		assert.Empty(t, fs)
		assert.False(t, held)
	})
	t.Run("dark day", func(t *testing.T) {
		s := setup(1000, 1000)
		s.Expected["2026-06-14"] = weather.ExpectedDay{KWhPerKWp: 1}
		fs, _ := eval(s)
		assert.Empty(t, fs)
	})
	t.Run("weather not in yet", func(t *testing.T) {
		s := setup(0, 0)
		s.Expected = map[string]weather.ExpectedDay{}
		fs, held := eval(s)
		assert.Empty(t, fs)
		assert.True(t, held)
	})
	t.Run("no weather", func(t *testing.T) {
		s := setup(0, 0)
		s.Expected = nil
		fs, held := eval(s)
		assert.Empty(t, fs)
		assert.False(t, held)
	})
}

// curve adds a day of 5-minute points for inv: power w(t) from 06:00 to 20:00 UTC.
func curve(s *Snapshot, inv uuid.UUID, day time.Time, w func(h float64) float64) {
	for t := day.Add(6 * time.Hour); t.Before(day.Add(20 * time.Hour)); t = t.Add(curveBucket) {
		h := float64(t.Hour()) + float64(t.Minute())/60
		s.Curves = append(s.Curves, telemetry.PowerPoint{Time: t, InverterID: inv, PacW: w(h)})
	}
}

// sunny is a clear-day curve peaking at peak W at 13:00.
func sunny(peak float64) func(h float64) float64 {
	return func(h float64) float64 { return max(0, peak*(1-(h-13)*(h-13)/49)) }
}

func TestClipping(t *testing.T) {
	inv := inverter(10)
	inv.MaxACPowerW = ref(8000.0)
	s := snapshot(summerNoon, inv)
	day0 := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	for d := range 4 {
		// 10 kW possible, cut at 8 kW for about 3 h a day.
		curve(s, inv.ID, day0.AddDate(0, 0, d), func(h float64) float64 { return min(8000, sunny(10000)(h)) })
	}
	fs := ofKind(evaluate(s, rules(t)), KindClipping)
	require.Len(t, fs, 1)
	assert.Equal(t, inv.ID, *fs[0].InverterID)
	assert.Equal(t, 4, fs[0].Details["days"])

	t.Run("not enough days", func(t *testing.T) {
		r := rules(t, AlertRule{Kind: KindClipping, Enabled: true, Params: map[string]float64{"days": 5}})
		assert.Empty(t, ofKind(evaluate(s, r), KindClipping))
	})
	t.Run("below the rating", func(t *testing.T) {
		inv.MaxACPowerW = ref(12000.0)
		s.Inverters = []systems.Inverter{inv}
		assert.Empty(t, ofKind(evaluate(s, rules(t)), KindClipping))
	})
	t.Run("unknown rating", func(t *testing.T) {
		inv.MaxACPowerW = nil
		s.Inverters = []systems.Inverter{inv}
		assert.Empty(t, ofKind(evaluate(s, rules(t)), KindClipping))
	})
}

func TestFeedInCap(t *testing.T) {
	a, b := inverter(5), inverter(5)
	day0 := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	capped := func(peaks ...float64) *Snapshot {
		s := snapshot(summerNoon, a, b)
		for d, p := range peaks {
			// Each inverter gets half of a system curve cut at 70 % of 10 kWp.
			half := func(h float64) float64 { return min(7000, sunny(p)(h)) / 2 }
			curve(s, a.ID, day0.AddDate(0, 0, d), half)
			curve(s, b.ID, day0.AddDate(0, 0, d), half)
		}
		return s
	}

	fs := ofKind(evaluate(capped(9000, 8500, 9500), rules(t)), KindFeedInCap)
	require.Len(t, fs, 1)
	assert.Equal(t, map[string]any{"percent": 70, "days": 3, "limit_w": 7000.0}, fs[0].Details)

	t.Run("natural clear days peak differently", func(t *testing.T) {
		s := snapshot(summerNoon, a, b)
		for d, p := range []float64{6900, 7050, 6800} {
			curve(s, a.ID, day0.AddDate(0, 0, d), func(h float64) float64 { return sunny(p)(h) / 2 })
			curve(s, b.ID, day0.AddDate(0, 0, d), func(h float64) float64 { return sunny(p)(h) / 2 })
		}
		assert.Empty(t, ofKind(evaluate(s, rules(t)), KindFeedInCap))
	})
	t.Run("cloudy days", func(t *testing.T) {
		assert.Empty(t, ofKind(evaluate(capped(5000, 6000, 9000), rules(t)), KindFeedInCap))
	})
	t.Run("above the limit", func(t *testing.T) {
		s := capped(9000, 9000, 9000)
		curve(s, a.ID, day0.AddDate(0, 0, 3), sunny(9000))
		fs := ofKind(evaluate(s, rules(t)), KindFeedInCap)
		require.Len(t, fs, 1, "one uncapped day doesn't matter")
		assert.Equal(t, 3, fs[0].Details["days"])
	})
}

func TestNewRuleTexts(t *testing.T) {
	loc := time.UTC
	m := RenderAlert("de", KindLowYield, "Dach", map[string]any{"percent": 40, "day": "2026-06-14", "actual_kwh": 20.0, "expected_kwh": 50.5}, loc)
	assert.Equal(t, "Nur 40 % des erwarteten Ertrags am 14.06.2026 (20,0 von 50,5 kWh).", m.Body)
	m = RenderAlert("en", KindClipping, "WR 1", map[string]any{"days": 4, "minutes": 30, "limit_w": 8000.0}, loc)
	assert.Equal(t, "WR 1 is clipping", m.Title)
	assert.Contains(t, m.Body, "(8.0 kW) for at least 30 minutes a day on 4 of the last 7 days")
	m = RenderAlert("en", KindFeedInCap, "Home", map[string]any{"percent": 70, "days": 3, "limit_w": 7000.0}, loc)
	assert.Equal(t, "Home: power capped at 70 %", m.Title)
	m = RenderAlert("es", KindLowYield, "Dach", map[string]any{"percent": 40, "day": "2026-06-14", "actual_kwh": 20.0, "expected_kwh": 50.5}, loc)
	assert.Equal(t, "Solo el 40 % de la producción esperada el 14 jun 2026 (20,0 de 50,5 kWh).", m.Body)
}

func TestLocaleTextsComplete(t *testing.T) {
	for l, texts := range alertTexts {
		assert.Len(t, texts, len(alertTexts["en"]), l)
		for k, v := range texts {
			assert.NotEmpty(t, v.title, "%s %s title", l, k)
			assert.NotEmpty(t, v.body, "%s %s body", l, k)
		}
	}
	for l, p := range phrases {
		assert.Len(t, p, len(phrases["en"]), l)
		for k := range phrases["en"] {
			assert.NotEmpty(t, p[k], "%s %s", l, k)
		}
	}
}
