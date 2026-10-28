package telemetry

import (
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/google/uuid"
)

// Reasons a value is rejected as implausible.
const (
	RejectFuture         = "future"
	RejectNegativePower  = "negative_power"
	RejectPowerAboveCap  = "power_above_limit"
	RejectEdayAboveCap   = "eday_above_limit"
	RejectDailyAboveCap  = "daily_above_limit"
	RejectNegativeEnergy = "negative_energy"
)

const (
	// futureSlack tolerates clocks that run a little ahead.
	futureSlack = 10 * time.Minute
	// powerHeadroom is how far above the rated power a reading may go.
	powerHeadroom = 1.3
	// maxWhPerKWpDay is well above the best specific yield on earth.
	maxWhPerKWpDay = 12000
)

// limits are an inverter's plausible maxima; 0 means unknown (no bound).
type limits struct {
	powerW   float64
	energyWh float64
}

// limitsFor derives the limits from the inverter's kWp and rated AC power,
// falling back to the system's kWp, which bounds any single inverter too.
func limitsFor(inv systems.Inverter, sys *systems.System) limits {
	kwp := inv.PeakPowerKWp
	if kwp <= 0 {
		kwp = sys.PeakPowerKWp
	}
	var l limits
	rated := kwp * 1000
	if inv.MaxACPowerW != nil && *inv.MaxACPowerW > rated {
		rated = *inv.MaxACPowerW
	}
	if rated > 0 {
		l.powerW = rated * powerHeadroom
	}
	if kwp > 0 {
		l.energyWh = kwp * maxWhPerKWpDay
	}
	return l
}

// rejection is one implausible value.
type rejection struct {
	reason string
	sample map[string]any
}

// checkPoint drops implausible values from p (sets them to nil) and returns
// why. drop means the whole point is unusable.
func checkPoint(p *Point, l limits, now time.Time) (rs []rejection, drop bool) {
	ts := p.Time.UTC().Format(time.RFC3339)
	if p.Time.After(now.Add(futureSlack)) {
		return []rejection{{RejectFuture, map[string]any{"time": ts}}}, true
	}
	power := func(field string, v **float64) {
		if *v == nil {
			return
		}
		x := **v
		switch {
		case x < 0:
			rs = append(rs, rejection{RejectNegativePower, map[string]any{"time": ts, "field": field, "value": x}})
		case l.powerW > 0 && x > l.powerW:
			rs = append(rs, rejection{RejectPowerAboveCap, map[string]any{"time": ts, "field": field, "value": x, "limit": l.powerW}})
		default:
			return
		}
		*v = nil
	}
	power("pac_w", &p.PacW)
	power("pdc_w", &p.PdcW)
	if p.EdayWh != nil && l.energyWh > 0 && *p.EdayWh > l.energyWh {
		rs = append(rs, rejection{RejectEdayAboveCap, map[string]any{"time": ts, "field": "eday_wh", "value": *p.EdayWh, "limit": l.energyWh}})
		p.EdayWh = nil
	}
	return rs, false
}

// checkDay reports whether d is implausible and why. It also drops an
// implausible PacMaxW.
func checkDay(d *Day, l limits) (rejection, bool) {
	date := d.Date.Format(time.DateOnly)
	switch {
	case d.EnergyWh < 0:
		return rejection{RejectNegativeEnergy, map[string]any{"date": date, "energy_wh": d.EnergyWh}}, true
	case l.energyWh > 0 && d.EnergyWh > l.energyWh:
		return rejection{RejectDailyAboveCap, map[string]any{"date": date, "energy_wh": d.EnergyWh, "limit": l.energyWh}}, true
	}
	if d.PacMaxW != nil && (*d.PacMaxW < 0 || l.powerW > 0 && *d.PacMaxW > l.powerW) {
		d.PacMaxW = nil
	}
	return rejection{}, false
}

// rejections collects a batch's rejections per inverter, day and reason.
type rejections map[rejKey]*IngestRejection

type rejKey struct {
	inv    uuid.UUID
	day    time.Time
	reason string
}

func (r rejections) add(inv uuid.UUID, day time.Time, rj rejection) {
	k := rejKey{inv, day, rj.reason}
	if x, ok := r[k]; ok {
		x.Count++
		return
	}
	r[k] = &IngestRejection{InverterID: inv, Day: day, Reason: rj.reason, Count: 1, Sample: rj.sample}
}

func (r rejections) list(systemID uuid.UUID) []IngestRejection {
	out := make([]IngestRejection, 0, len(r))
	for _, x := range r {
		x.SystemID = systemID
		out = append(out, *x)
	}
	return out
}
