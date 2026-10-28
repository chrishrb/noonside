package alerting

import (
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

// CurveDays is how many complete local days clipping and feed_in_cap look at.
const CurveDays = 7

// curveBucket is the bucket of Snapshot.Curves.
const curveBucket = 5 * time.Minute

// evalLowYield reports yesterday's yield below percent_of_expected of the
// weather-based expectation, for the system or (if the system is fine) single
// inverters. Days expecting less than min_expected_kwh_per_kwp are skipped.
// It can't judge until yesterday's weather is in (early in the morning).
func evalLowYield(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	if s.Expected == nil || len(s.Inverters) == 0 || s.YesterdayDate.IsZero() || s.FirstDay == nil || s.FirstDay.After(s.YesterdayDate) {
		return nil, false
	}
	day := s.YesterdayDate.Format(time.DateOnly)
	exp, ok := s.Expected[day]
	if !ok {
		return nil, true
	}
	if exp.KWhPerKWp < p["min_expected_kwh_per_kwp"] {
		return nil, false
	}
	details := func(actualWh, kwp float64) map[string]any {
		expected := exp.KWhPerKWp * kwp
		return map[string]any{
			"percent": int(actualWh/1000/expected*100 + 0.5), "day": day,
			"actual_kwh": math.Round(actualWh/100) / 10, "expected_kwh": math.Round(expected*10) / 10,
		}
	}
	var total, kwp float64
	for _, inv := range s.Inverters {
		total += s.Yesterday[inv.ID]
		kwp += inv.PeakPowerKWp
	}
	if kwp <= 0 {
		kwp = s.System.PeakPowerKWp
	}
	if kwp <= 0 {
		return nil, false
	}
	if total/1000 < exp.KWhPerKWp*kwp*p["percent_of_expected"]/100 {
		return []Finding{{Details: details(total, kwp)}}, false
	}
	if len(s.Inverters) < 2 {
		return nil, false
	}
	var out []Finding
	for _, inv := range s.Inverters {
		if inv.PeakPowerKWp <= 0 {
			continue
		}
		if wh := s.Yesterday[inv.ID]; wh/1000 < exp.KWhPerKWp*inv.PeakPowerKWp*p["percent_of_expected"]/100 {
			out = append(out, Finding{SubjectKey: inv.ID.String(), InverterID: ref(inv.ID), Details: details(wh, inv.PeakPowerKWp)})
		}
	}
	return out, false
}

// dayCurves groups curve points by local date and inverter.
func dayCurves(s *Snapshot) map[string]map[uuid.UUID][]float64 {
	loc := s.System.Loc()
	out := map[string]map[uuid.UUID][]float64{}
	for _, pt := range s.Curves {
		d := pt.Time.In(loc).Format(time.DateOnly)
		if out[d] == nil {
			out[d] = map[uuid.UUID][]float64{}
		}
		out[d][pt.InverterID] = append(out[d][pt.InverterID], pt.PacW)
	}
	return out
}

// evalClipping reports inverters whose power sat at 98 % or more of their
// rated AC power for minutes_per_day on at least days of the last CurveDays:
// the modules could deliver more than the inverter converts.
func evalClipping(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	days := dayCurves(s)
	var out []Finding
	for _, inv := range s.Inverters {
		if inv.MaxACPowerW == nil {
			continue
		}
		limit := *inv.MaxACPowerW * 0.98
		n := 0
		for _, byInv := range days {
			buckets := 0
			for _, w := range byInv[inv.ID] {
				if w >= limit {
					buckets++
				}
			}
			if float64(buckets)*curveBucket.Minutes() >= p["minutes_per_day"] {
				n++
			}
		}
		if n > 0 && float64(n) >= p["days"] {
			out = append(out, Finding{SubjectKey: inv.ID.String(), InverterID: ref(inv.ID), Details: map[string]any{
				"days": n, "minutes": int(p["minutes_per_day"]), "limit_w": *inv.MaxACPowerW,
			}})
		}
	}
	return out, false
}

// feedInCaps are the usual German feed-in limits in % of the peak power.
var feedInCaps = []float64{70, 60, 50}

// evalFeedInCap reports a system whose total power plateaus at a usual
// feed-in limit (70/60/50 % of kWp): on at least days of the last CurveDays
// it stayed within -3/+2 % of the limit for minutes_per_day without going
// above it, and those days peaked at nearly the same power (a natural clear
// day peaks a little differently each day).
func evalFeedInCap(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	var kwp float64
	for _, inv := range s.Inverters {
		kwp += inv.PeakPowerKWp
	}
	if kwp <= 0 {
		kwp = s.System.PeakPowerKWp
	}
	if kwp <= 0 {
		return nil, false
	}
	// System power per bucket and day.
	loc := s.System.Loc()
	totals := map[string]map[int64]float64{}
	for _, pt := range s.Curves {
		d := pt.Time.In(loc).Format(time.DateOnly)
		if totals[d] == nil {
			totals[d] = map[int64]float64{}
		}
		totals[d][pt.Time.Unix()] += pt.PacW
	}
	for _, pct := range feedInCaps {
		limit := kwp * 1000 * pct / 100
		var peaks []float64
		for _, byTime := range totals {
			peak, buckets := 0.0, 0
			for _, w := range byTime {
				peak = max(peak, w)
				if w >= limit*0.97 && w <= limit*1.02 {
					buckets++
				}
			}
			if peak <= limit*1.02 && float64(buckets)*curveBucket.Minutes() >= p["minutes_per_day"] {
				peaks = append(peaks, peak)
			}
		}
		if len(peaks) == 0 || float64(len(peaks)) < p["days"] {
			continue
		}
		if slices.Max(peaks)-slices.Min(peaks) > limit*0.015 {
			continue
		}
		return []Finding{{Details: map[string]any{"percent": int(pct), "days": len(peaks), "limit_w": math.Round(limit)}}}, false
	}
	return nil, false
}
