package analyticsapi

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"

	"github.com/chrishrb/noonside/internal/modules/weather"
)

// expectation is the expected energy of a period: from the weather where
// known (per day, or the day's irradiance curve), otherwise from the monthly
// targets.
type expectation struct {
	loc     *time.Location
	kwp     float64
	targets map[int]float64
	// days holds the weather-based expectation by local date.
	days map[string]weather.ExpectedDay
	// slots is the day's irradiance (day periods only).
	slots []weather.Slot
	used  bool
}

func (h *Handler) expectation(ctx context.Context, sys *systems.System, p period, first time.Time, targets map[int]float64, kwp float64) *expectation {
	e := &expectation{loc: sys.Loc(), kwp: kwp, targets: targets}
	if kwp <= 0 {
		return e
	}
	if p.kind == "day" {
		slots, err := h.weather.Curve(ctx, sys, p.start)
		if err != nil {
			h.logger.WarnContext(ctx, "expected power curve", "system", sys.ID, "error", err)
		}
		// A partial curve would understate the day.
		if weather.ExpectedKWh(slots, p.start, p.end, 1) > 0 && coversDay(slots, p.start, p.end) {
			e.slots = slots
			e.days = map[string]weather.ExpectedDay{p.start.Format(time.DateOnly): {
				KWhPerKWp: weather.ExpectedKWh(slots, p.start, p.end, 1), IrradiationKWhM2: weather.IrradiationKWhM2(slots, p.start, p.end),
			}}
		}
		return e
	}
	days, err := h.weather.ExpectedDays(ctx, sys, maxTime(p.start, first), p.end)
	if err != nil {
		h.logger.WarnContext(ctx, "expected daily energy", "system", sys.ID, "error", err)
	}
	e.days = days
	return e
}

func coversDay(slots []weather.Slot, from, to time.Time) bool {
	var d time.Duration
	for _, s := range slots {
		d += s.Len()
	}
	return d >= to.Sub(from)
}

// kwh is the expected energy of the local days in [from, to).
func (e *expectation) kwh(from, to time.Time) float64 {
	total := 0.0
	for d := from; d.Before(to); {
		next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, e.loc)
		if next.After(to) {
			next = to
		}
		if v, ok := e.days[d.Format(time.DateOnly)]; ok {
			// A partial day (to before midnight) counts in proportion.
			total += v.KWhPerKWp * e.kwp * float64(next.Sub(d)) / float64(time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, e.loc).Sub(d))
			e.used = true
		} else {
			total += targetKWh(e.targets, d, next)
		}
		d = next
	}
	return total
}

// weather reports whether any weather-based value was used.
func (e *expectation) weather() bool { return e.used || e.slots != nil }
