package analyticsapi

import (
	"context"
	"sort"
	"time"

	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

// The performance ratio (PR) is the measured energy over the energy a
// loss-free system would have produced: irradiation on the panel planes
// (kWh/m²) × peak power (kWp). It is independent of the weather, so a
// falling yearly PR shows degradation or soiling.

// prMinDays is how many days with weather data a year needs for the trend.
const prMinDays = 300

// prSum accumulates energy and irradiation over days.
type prSum struct {
	kwh, irr float64
	days     int
}

func (a *prSum) add(kwh, irr float64) {
	a.kwh += kwh
	a.irr += irr
	a.days++
}

// pct is the performance ratio in % for kwp, false without irradiation.
func (a prSum) pct(kwp float64) (float64, bool) {
	if a.irr <= 0 || kwp <= 0 {
		return 0, false
	}
	return a.kwh / (a.irr * kwp) * 100, true
}

// performance holds the performance ratio of an overview.
type performance struct {
	total   prSum
	buckets []prSum // month/year/total: per bucket
	// trend is the change of the yearly PR in percentage points per year.
	trend *float64
}

// performance computes the performance ratio of the shown inverters: for a
// day from its irradiance curve (today until now), otherwise over the
// complete past days that have weather data and energy.
func (h *Handler) performance(ctx context.Context, systemID uuid.UUID, p period, from, today time.Time, starts []time.Time,
	exp *expectation, energy float64, only map[uuid.UUID]bool,
) (*performance, error) {
	out := &performance{}
	if p.kind == "day" {
		if exp.slots == nil {
			return out, nil
		}
		to := p.end
		if p.start.Equal(today) {
			to = h.now()
		}
		if irr := weather.IrradiationKWhM2(exp.slots, p.start, to); irr > 0 {
			out.total.add(energy, irr)
		}
		return out, nil
	}
	if len(exp.days) == 0 {
		return out, nil
	}
	end := minTime(p.end, today)
	if !from.Before(end) {
		return out, nil
	}
	bs, err := h.telemetry.Energy(ctx, systemID, "day", from, end)
	if err != nil {
		return nil, err
	}
	kwhByDay := map[string]float64{}
	for _, b := range bs {
		if only == nil || only[b.InverterID] {
			kwhByDay[b.Start.Format(time.DateOnly)] += b.EnergyWh / 1000
		}
	}
	out.buckets = make([]prSum, len(starts))
	years := map[int]*prSum{}
	for d, kwh := range kwhByDay {
		e, ok := exp.days[d]
		if !ok || e.IrradiationKWhM2 <= 0 {
			continue
		}
		day, err := time.ParseInLocation(time.DateOnly, d, exp.loc)
		if err != nil {
			continue
		}
		out.total.add(kwh, e.IrradiationKWhM2)
		// The bucket is the last one starting at or before the day.
		if i := sort.Search(len(starts), func(i int) bool { return starts[i].After(day) }) - 1; i >= 0 {
			out.buckets[i].add(kwh, e.IrradiationKWhM2)
		}
		if years[day.Year()] == nil {
			years[day.Year()] = &prSum{}
		}
		years[day.Year()].add(kwh, e.IrradiationKWhM2)
	}
	if p.kind == "total" {
		out.trend = prTrend(years, exp.kwp)
	}
	return out, nil
}

// prTrend is the least-squares slope of the yearly PR in percentage points
// per year over the years with enough data; nil for fewer than three.
func prTrend(years map[int]*prSum, kwp float64) *float64 {
	var xs, ys []float64
	for y, a := range years {
		if a.days < prMinDays {
			continue
		}
		if pr, ok := a.pct(kwp); ok {
			xs, ys = append(xs, float64(y)), append(ys, pr)
		}
	}
	if len(xs) < 3 {
		return nil
	}
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	n := float64(len(xs))
	mx, my = mx/n, my/n
	var num, den float64
	for i := range xs {
		num += (xs[i] - mx) * (ys[i] - my)
		den += (xs[i] - mx) * (xs[i] - mx)
	}
	slope := num / den
	return &slope
}

// apply writes the performance ratio into the overview.
func (pf *performance) apply(out *generated.Overview, kwp float64) {
	if pr, ok := pf.total.pct(kwp); ok {
		out.Summary.PerformanceRatioPct = generated.NewOptFloat64(round(pr, 1))
		out.Summary.IrradiationKwhM2 = generated.NewOptFloat64(round(pf.total.irr, 1))
	}
	if pf.trend != nil {
		out.Summary.PerformanceRatioTrendPct = generated.NewOptFloat64(round(*pf.trend, 2))
	}
	if pf.buckets == nil {
		return
	}
	out.PerformanceRatio = make([]generated.NilFloat64, len(pf.buckets))
	for i, b := range pf.buckets {
		if pr, ok := b.pct(kwp); ok {
			out.PerformanceRatio[i] = generated.NewNilFloat64(round(pr, 1))
		} else {
			out.PerformanceRatio[i].SetToNull()
		}
	}
}
