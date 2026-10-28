package analyticsapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

const (
	dayBucket     = 5 * time.Minute
	maxExportDays = 366 * 30
	// maxPowerExportDays bounds power exports (5min, minute), so they finish
	// well within the API's 60 s.
	maxPowerExportDays = 31
)

var (
	epoch   = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	farAway = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
)

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func efficiency(pac, pdc float64) generated.OptFloat64 {
	if pdc <= 0 {
		return generated.OptFloat64{}
	}
	return generated.NewOptFloat64(round(pac/pdc*100, 1))
}

// energyKWh sums the energy of the given inverters (all if nil) in [from, to)
// and reports whether it contains manually corrected days.
func (h *Handler) energyKWh(ctx context.Context, systemID uuid.UUID, from, to time.Time, only map[uuid.UUID]bool) (float64, map[uuid.UUID]float64, bool, error) {
	bs, err := h.telemetry.Energy(ctx, systemID, "year", from, to)
	if err != nil {
		return 0, nil, false, err
	}
	total := 0.0
	per := map[uuid.UUID]float64{}
	corrected := false
	for _, b := range bs {
		if only != nil && !only[b.InverterID] {
			continue
		}
		total += b.EnergyWh / 1000
		per[b.InverterID] += b.EnergyWh / 1000
		corrected = corrected || b.Corrected
	}
	return total, per, corrected, nil
}

// --- live ----------------------------------------------------------------

func (h *Handler) GetLive(ctx context.Context, params generated.GetLiveParams) (*generated.Live, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	return h.live(ctx, sys)
}

func (h *Handler) live(ctx context.Context, sys *systems.System) (*generated.Live, error) {
	invs, err := h.systems.ListInverters(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	states, err := h.telemetry.ListInverterStates(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	sources, err := h.sources.ListDataSources(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	onlineWindow := alerting.OnlineWindow(sources)
	loc := sys.Loc()
	today := dateOf(h.now(), loc)
	todayKWh, _, _, err := h.energyKWh(ctx, sys.ID, today, today.AddDate(0, 0, 1), nil)
	if err != nil {
		return nil, err
	}

	byID := map[uuid.UUID]telemetry.InverterState{}
	for _, s := range states {
		byID[s.InverterID] = s
	}
	out := &generated.Live{Inverters: make([]generated.LiveInverter, 0, len(invs)), TodayKwh: generated.NewOptFloat64(round(todayKWh, 2))}
	var latest *time.Time
	for _, inv := range invs {
		li := generated.LiveInverter{InverterID: inv.ID, Name: inv.Name}
		if st, ok := byID[inv.ID]; ok {
			t := st.Time
			li.Time = generated.NewOptDateTime(t)
			li.Online = h.now().Sub(t) <= onlineWindow
			li.StatusCode, li.StatusText = httpapi.OptInt(st.StatusCode), st.StatusText
			li.ErrorCode, li.ErrorText = httpapi.OptInt(st.ErrorCode), st.ErrorText
			li.PowerLimitPct = httpapi.OptInt(st.PowerLimitPct)
			if li.Online {
				li.PacW, li.PdcW = httpapi.OptFloat(st.PacW), httpapi.OptFloat(st.PdcW)
				if st.PacW != nil && st.PdcW != nil {
					li.EfficiencyPct = efficiency(*st.PacW, *st.PdcW)
				}
				out.PacW += deref(st.PacW)
				out.PdcW += deref(st.PdcW)
			}
			if latest == nil || t.After(*latest) {
				latest = &t
			}
		}
		out.Inverters = append(out.Inverters, li)
	}
	out.Time = httpapi.OptTime(latest)
	out.EfficiencyPct = efficiency(out.PacW, out.PdcW)
	if out.Stale, err = h.stale(ctx, sys, sources, latest); err != nil {
		return nil, err
	}
	return out, nil
}

// stale reports whether the system's live data from last is stale (see
// alerting.Stale), given its data sources.
func (h *Handler) stale(ctx context.Context, sys *systems.System, sources []datasources.DataSource, last *time.Time) (bool, error) {
	now := h.now()
	if last == nil || now.Sub(*last) <= alerting.StaleAfter(nil) {
		return false, nil // fresh either way; skip the queries
	}
	rules, err := h.alerting.ListAlertRules(ctx, sys.ID)
	if err != nil {
		return false, err
	}
	return alerting.Stale(sys, rules, sources, last, now), nil
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// --- overview ------------------------------------------------------------

func (h *Handler) GetOverview(ctx context.Context, params generated.GetOverviewParams) (*generated.Overview, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	return h.overview(ctx, sys, overviewQuery{
		period: params.Period, date: params.Date, inverters: params.Inverters, compare: params.Compare,
	})
}

// overviewQuery selects what an overview shows.
type overviewQuery struct {
	period    generated.Period
	date      generated.OptDate
	inverters []uuid.UUID // nil: all
	compare   generated.OptCompare
}

// overview builds the overview of sys; callers have authorized the request.
func (h *Handler) overview(ctx context.Context, sys *systems.System, q overviewQuery) (*generated.Overview, error) {
	loc := sys.Loc()
	today := dateOf(h.now(), loc)
	date := today
	if d, ok := q.date.Get(); ok {
		date = d
	}

	allInvs, err := h.systems.ListInverters(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	invs := allInvs
	var only map[uuid.UUID]bool
	if len(q.inverters) > 0 {
		only = map[uuid.UUID]bool{}
		known := map[uuid.UUID]systems.Inverter{}
		for _, inv := range allInvs {
			known[inv.ID] = inv
		}
		invs = nil
		for _, id := range q.inverters {
			inv, ok := known[id]
			if !ok {
				return nil, httpapi.BadRequest("unknown inverter " + id.String())
			}
			if !only[id] {
				only[id] = true
				invs = append(invs, inv)
			}
		}
	}

	first := today
	if f, err := h.telemetry.FirstDataDay(ctx, sys.ID); err != nil {
		return nil, err
	} else if f != nil {
		first = time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, loc)
	}
	if sys.CommissionedAt != nil {
		c := time.Date(sys.CommissionedAt.Year(), sys.CommissionedAt.Month(), sys.CommissionedAt.Day(), 0, 0, 0, 0, loc)
		if c.Before(first) {
			first = c
		}
	}

	p, err := newPeriod(string(q.period), date, first, loc)
	if err != nil {
		return nil, httpapi.BadRequest(err.Error())
	}
	targets, err := h.systems.GetTargets(ctx, sys.ID)
	if err != nil {
		return nil, err
	}

	out := &generated.Overview{
		Period:     q.period,
		Date:       time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc),
		FirstDate:  first,
		RangeStart: p.start,
		RangeEnd:   p.end,
		PrevDate:   p.start.AddDate(0, 0, -1),
		Series:     make([]generated.InverterSeries, len(invs)),
	}
	if !p.end.After(today) {
		out.NextDate = generated.NewOptDate(p.end)
	}
	for i, inv := range invs {
		out.Series[i] = generated.InverterSeries{InverterID: inv.ID, Values: []float64{}}
	}
	index := map[uuid.UUID]int{}
	for i, inv := range invs {
		index[inv.ID] = i
	}

	kwp := sys.PeakPowerKWp
	if only != nil {
		kwp = 0
		for _, inv := range invs {
			kwp += inv.PeakPowerKWp
		}
	}
	exp := h.expectation(ctx, sys, p, first, targets, kwp)

	energy, perInv, corrected, err := h.energyKWh(ctx, sys.ID, p.start, p.end, only)
	if err != nil {
		return nil, err
	}
	maxPac := map[uuid.UUID]float64{}
	sumPac, sumPdc := map[uuid.UUID]float64{}, map[uuid.UUID]float64{}
	var maxValue float64
	var maxAt *time.Time

	if p.kind == "day" {
		out.Unit = generated.OverviewUnitW
		points, err := h.telemetry.PowerCurve(ctx, sys.ID, p.start, p.end, dayBucket)
		if err != nil {
			return nil, err
		}
		slot := map[time.Time]int{}
		for _, pt := range points {
			if _, ok := index[pt.InverterID]; !ok {
				continue
			}
			if _, ok := slot[pt.Time]; !ok {
				slot[pt.Time] = len(out.Timestamps)
				out.Timestamps = append(out.Timestamps, pt.Time)
			}
		}
		sort.Slice(out.Timestamps, func(i, j int) bool { return out.Timestamps[i].Before(out.Timestamps[j]) })
		for i, t := range out.Timestamps {
			slot[t] = i
		}
		out.Total = make([]float64, len(out.Timestamps))
		for i := range out.Series {
			out.Series[i].Values = make([]float64, len(out.Timestamps))
		}
		for _, pt := range points {
			i, ok := index[pt.InverterID]
			if !ok {
				continue
			}
			sumPac[pt.InverterID] += pt.PacW
			sumPdc[pt.InverterID] += pt.PdcW
			v := round(pt.PacW, 0)
			out.Series[i].Values[slot[pt.Time]] = v
			out.Total[slot[pt.Time]] += v
			sr := &out.Series[i]
			setReading(&sr.UdcV, len(out.Timestamps), slot[pt.Time], pt.UdcV, 1)
			setReading(&sr.TemperatureC, len(out.Timestamps), slot[pt.Time], pt.TemperatureC, 1)
		}
		for i, v := range out.Total {
			if v > maxValue {
				maxValue = v
				t := out.Timestamps[i]
				maxAt = &t
			}
		}
		if maxPac, err = h.telemetry.PeakPower(ctx, sys.ID, p.start, p.end); err != nil {
			return nil, err
		}
		for _, sl := range exp.slots {
			out.Expected = append(out.Expected, generated.ExpectedPoint{Time: sl.Mid(), W: round(sl.Power(kwp), 0)})
		}
		if sys.PriceZone != nil {
			ps, err := h.prices.MarketPrices(ctx, *sys.PriceZone, p.start, p.end)
			if err != nil {
				return nil, err
			}
			for _, mp := range prices.Clip(ps, p.start, p.end) {
				out.Prices = append(out.Prices, generated.PricePoint{Start: mp.Start, End: mp.End, EurMwh: round(mp.EURMWh, 2)})
			}
			if len(out.Prices) > 0 && h.prices.SourceName() != "" {
				out.PricesSource = generated.NewOptString(h.prices.SourceName())
			}
		}
	} else {
		out.Unit = generated.OverviewUnitKWh
		starts := p.buckets()
		out.Timestamps = starts
		out.Total = make([]float64, len(starts))
		out.Target = make([]float64, len(starts))
		out.Corrected = make([]bool, len(starts))
		for i := range out.Series {
			out.Series[i].Values = make([]float64, len(starts))
		}
		slot := map[string]int{}
		for i, s := range starts {
			slot[s.Format(time.DateOnly)] = i
			end := p.end
			if i+1 < len(starts) {
				end = starts[i+1]
			}
			out.Target[i] = round(exp.kwh(maxTime(s, first), end), 2)
		}
		buckets, err := h.telemetry.Energy(ctx, sys.ID, p.granularity(), p.start, p.end)
		if err != nil {
			return nil, err
		}
		for _, b := range buckets {
			i, ok := index[b.InverterID]
			if !ok {
				continue
			}
			j, ok := slot[b.Start.Format(time.DateOnly)]
			if !ok {
				continue
			}
			v := b.EnergyWh / 1000
			out.Series[i].Values[j] += v
			out.Total[j] += v
			out.Corrected[j] = out.Corrected[j] || b.Corrected
			if b.PacMaxW != nil && *b.PacMaxW > maxPac[b.InverterID] {
				maxPac[b.InverterID] = *b.PacMaxW
			}
		}
		for i := range out.Series {
			for j, v := range out.Series[i].Values {
				out.Series[i].Values[j] = round(v, 2)
			}
		}
		for j, v := range out.Total {
			out.Total[j] = round(v, 2)
			if v > maxValue {
				maxValue = v
				t := starts[j]
				maxAt = &t
			}
		}
		if c, ok := q.compare.Get(); ok && (p.kind == "month" || p.kind == "year") {
			if out.Comparison, err = h.comparison(ctx, sys.ID, p, first, c, only); err != nil {
				return nil, err
			}
		}
	}

	lifetime, _, _, err := h.energyKWh(ctx, sys.ID, epoch, farAway, only)
	if err != nil {
		return nil, err
	}
	targetFrom := maxTime(p.start, first)
	target := exp.kwh(targetFrom, p.end)
	toDate := exp.kwh(targetFrom, minTime(p.end, today.AddDate(0, 0, 1)))
	if p.kind == "day" && exp.slots != nil && p.start.Equal(today) {
		// Today: compare with what was expected until now.
		toDate = weather.ExpectedKWh(exp.slots, p.start, h.now(), kwp)
	}

	neg, err := h.prices.NegativeDuring(ctx, sys, p.start, p.end, only)
	if err != nil {
		return nil, err
	}
	revenue := energy * sys.TariffPerKWh
	if neg != nil {
		revenue -= neg.UnpaidRevenue
	}
	sum := generated.OverviewSummary{
		EnergyKwh:            round(energy, 2),
		Revenue:              round(revenue, 2),
		Currency:             sys.Currency,
		Co2AvoidedKg:         round(energy*sys.CO2KgPerKWh, 1),
		LifetimeEnergyKwh:    round(lifetime, 2),
		LifetimeCo2AvoidedKg: round(lifetime*sys.CO2KgPerKWh, 1),
		MaxValue:             generated.NewOptFloat64(round(maxValue, 2)),
		MaxAt:                httpapi.OptTime(maxAt),
	}
	if corrected {
		sum.Corrected = generated.NewOptBool(true)
	}
	if kwp > 0 {
		sum.SpecificYieldKwhKwp = generated.NewOptFloat64(round(energy/kwp, 2))
	}
	if neg != nil {
		sum.NegativePriceKwh = generated.NewOptFloat64(round(neg.KWh, 2))
		sum.NegativePriceHours = generated.NewOptFloat64(round(neg.Hours, 2))
		if sys.UnpaidAtNegativePrices {
			sum.UnpaidRevenue = generated.NewOptFloat64(round(neg.UnpaidRevenue, 2))
		}
	}
	if exp.weather() {
		sum.TargetSource = generated.NewOptOverviewSummaryTargetSource(generated.OverviewSummaryTargetSourceWeather)
	} else if len(targets) > 0 {
		sum.TargetSource = generated.NewOptOverviewSummaryTargetSource(generated.OverviewSummaryTargetSourceTargets)
	}
	if exp.weather() || len(targets) > 0 {
		sum.TargetKwh = generated.NewOptFloat64(round(target, 2))
		sum.TargetToDateKwh = generated.NewOptFloat64(round(toDate, 2))
		if toDate > 0 {
			sum.ActualPct = generated.NewOptFloat64(round(energy/toDate*100, 1))
		}
	}
	out.Summary = sum
	var starts []time.Time
	if p.kind != "day" {
		starts = p.buckets()
	}
	pf, err := h.performance(ctx, sys.ID, p, targetFrom, today, starts, exp, energy, only)
	if err != nil {
		return nil, err
	}
	pf.apply(out, kwp)

	states, err := h.telemetry.ListInverterStates(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	stateOf := map[uuid.UUID]telemetry.InverterState{}
	for _, s := range states {
		stateOf[s.InverterID] = s
	}
	out.Inverters = make([]generated.InverterSummary, len(invs))
	for i, inv := range invs {
		e := perInv[inv.ID]
		row := generated.InverterSummary{
			InverterID:   inv.ID,
			Name:         inv.Name,
			Color:        inv.Color,
			PeakPowerKwp: inv.PeakPowerKWp,
			EnergyKwh:    round(e, 2),
			StatusText:   stateOf[inv.ID].StatusText,
		}
		if et := stateOf[inv.ID].ErrorText; et != "" {
			row.ErrorText = generated.NewOptString(et)
		}
		if inv.PeakPowerKWp > 0 {
			row.SpecificYieldKwhKwp = generated.NewOptFloat64(round(e/inv.PeakPowerKWp, 2))
		}
		if v, ok := maxPac[inv.ID]; ok {
			row.MaxPacW = generated.NewOptFloat64(round(v, 0))
		}
		if energy > 0 {
			row.SharePct = round(e/energy*100, 1)
		}
		// Only Eday is metered, so DC energy is scaled from AC by the day's
		// Pdc/Pac ratio.
		if pac, pdc := sumPac[inv.ID], sumPdc[inv.ID]; pac > 0 && pdc > 0 {
			row.DCEnergyKwh = generated.NewOptFloat64(round(e*pdc/pac, 2))
			row.EfficiencyPct = efficiency(pac, pdc)
		}
		out.Inverters[i] = row
	}
	return out, nil
}

// setReading stores v (rounded) at slot i of the per-bucket readings *arr,
// which is created with nulls on the first reading.
func setReading(arr *[]generated.NilFloat64, n, i int, v *float64, digits int) {
	if v == nil {
		return
	}
	if *arr == nil {
		*arr = make([]generated.NilFloat64, n)
		for j := range *arr {
			(*arr)[j].SetToNull()
		}
	}
	(*arr)[i] = generated.NewNilFloat64(round(*v, digits))
}

// comparison returns the energy of the period's buckets (month: days, year:
// months) in earlier years, newest first: the previous year or every year
// since the first data.
func (h *Handler) comparison(ctx context.Context, systemID uuid.UUID, p period, first time.Time, c generated.Compare, only map[uuid.UUID]bool) ([]generated.ComparisonSeries, error) {
	oldest := p.start.Year() - 1
	if c == generated.CompareAllYears {
		oldest = first.Year()
	}
	oldest = max(oldest, first.Year())
	n := len(p.buckets())
	out := []generated.ComparisonSeries{}
	for y := p.start.Year() - 1; y >= oldest; y-- {
		start := p.start.AddDate(y-p.start.Year(), 0, 0)
		end := p.end.AddDate(y-p.start.Year(), 0, 0)
		bs, err := h.telemetry.Energy(ctx, systemID, p.granularity(), start, end)
		if err != nil {
			return nil, err
		}
		sums := make([]float64, n)
		for _, b := range bs {
			if only != nil && !only[b.InverterID] {
				continue
			}
			i := b.Start.Day() - 1
			if p.kind == "year" {
				i = int(b.Start.Month()) - 1
			}
			if i >= 0 && i < n {
				sums[i] += b.EnergyWh / 1000
			}
		}
		cs := generated.ComparisonSeries{Year: y, Values: make([]generated.NilFloat64, n)}
		for i := range n {
			var bStart, bEnd time.Time
			if p.kind == "year" {
				bStart = time.Date(y, time.Month(i+1), 1, 0, 0, 0, 0, p.start.Location())
				bEnd = bStart.AddDate(0, 1, 0)
			} else {
				bStart = time.Date(y, start.Month(), i+1, 0, 0, 0, 0, p.start.Location())
				bEnd = bStart.AddDate(0, 0, 1)
			}
			// Days past the month's end (Feb 29) and buckets before the first data have no value.
			if (p.kind == "month" && bStart.Month() != start.Month()) || !bEnd.After(first) {
				cs.Values[i].SetToNull()
				continue
			}
			cs.Values[i] = generated.NewNilFloat64(round(sums[i], 2))
			cs.TotalKwh += sums[i]
		}
		cs.TotalKwh = round(cs.TotalKwh, 2)
		out = append(out, cs)
	}
	return out, nil
}

// GetDailyEnergy returns the system's energy per local day (calendar heatmap).
func (h *Handler) GetDailyEnergy(ctx context.Context, params generated.GetDailyEnergyParams) (*generated.DailyEnergy, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	if !params.From.Before(params.To) {
		return nil, httpapi.BadRequest("from must be before to")
	}
	if params.To.Sub(params.From) > maxExportDays*24*time.Hour {
		return nil, httpapi.BadRequest("range too large")
	}
	bs, err := h.telemetry.Energy(ctx, params.SystemId, "day", params.From, params.To)
	if err != nil {
		return nil, err
	}
	out := &generated.DailyEnergy{Days: []generated.DayEnergy{}}
	for _, b := range bs {
		// Buckets are ordered by day, then inverter.
		if n := len(out.Days); n > 0 && out.Days[n-1].Date.Equal(b.Start) {
			out.Days[n-1].EnergyKwh += b.EnergyWh / 1000
			continue
		}
		out.Days = append(out.Days, generated.DayEnergy{Date: b.Start, EnergyKwh: b.EnergyWh / 1000})
	}
	for i := range out.Days {
		out.Days[i].EnergyKwh = round(out.Days[i].EnergyKwh, 2)
	}
	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// --- dashboard -----------------------------------------------------------

func (h *Handler) GetDashboard(ctx context.Context) (*generated.Dashboard, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	var filter *uuid.UUID
	if !p.Superadmin {
		filter = &p.UserID
	}
	systems, err := h.systems.ListSystems(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := &generated.Dashboard{Systems: make([]generated.DashboardSystem, 0, len(systems))}
	for _, s := range systems {
		if !p.Superadmin && !slices.Contains(auth.RoleScopes(s.Role), auth.ScopeDataRead) {
			continue
		}
		ds, err := h.dashboardSystem(ctx, &s)
		if err != nil {
			return nil, err
		}
		out.Systems = append(out.Systems, *ds)
	}
	return out, nil
}

func (h *Handler) dashboardSystem(ctx context.Context, s *systems.SystemWithRole) (*generated.DashboardSystem, error) {
	loc := s.Loc()
	now := h.now()
	today := dateOf(now, loc)
	month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
	year := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, loc)
	tomorrow := today.AddDate(0, 0, 1)

	out := &generated.DashboardSystem{
		SystemID: s.ID, Name: s.Name, Role: generated.Role(s.Role), PeakPowerKwp: s.PeakPowerKWp,
		ImageURL: httpapi.OptMediaURL(s.ImageKey),
	}
	invs, err := h.systems.ListInverters(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	states, err := h.telemetry.ListInverterStates(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	sources, err := h.sources.ListDataSources(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	onlineWindow := alerting.OnlineWindow(sources)
	out.InvertersTotal = len(invs)
	var latest *time.Time
	for _, st := range states {
		t := st.Time
		if latest == nil || t.After(*latest) {
			latest = &t
		}
		if now.Sub(st.Time) <= onlineWindow {
			out.InvertersOnline++
			out.PacW += deref(st.PacW)
		}
		if st.ErrorCode != nil && *st.ErrorCode != 0 {
			out.InvertersWithError++
		}
	}
	out.LastUpdate = httpapi.OptTime(latest)
	if out.Stale, err = h.stale(ctx, &s.System, sources, latest); err != nil {
		return nil, err
	}

	bs, err := h.telemetry.Energy(ctx, s.ID, "day", year, tomorrow)
	if err != nil {
		return nil, err
	}
	monthByDay := map[string]float64{}
	for _, b := range bs {
		kwh := b.EnergyWh / 1000
		out.YearKwh += kwh
		d := time.Date(b.Start.Year(), b.Start.Month(), b.Start.Day(), 0, 0, 0, 0, loc)
		if !d.Before(month) {
			out.MonthKwh += kwh
			monthByDay[d.Format(time.DateOnly)] += kwh
		}
		if d.Equal(today) {
			out.TodayKwh += kwh
		}
	}
	lifetime, _, _, err := h.energyKWh(ctx, s.ID, epoch, farAway, nil)
	if err != nil {
		return nil, err
	}
	out.LifetimeKwh = round(lifetime, 2)
	out.TodayKwh, out.MonthKwh, out.YearKwh = round(out.TodayKwh, 2), round(out.MonthKwh, 2), round(out.YearKwh, 2)
	out.PacW = round(out.PacW, 0)

	targets, err := h.systems.GetTargets(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	exp := h.expectation(ctx, &s.System, period{kind: "year", start: year, end: year.AddDate(1, 0, 0)}, year, targets, s.PeakPowerKWp)
	if exp.weather() || len(targets) > 0 {
		out.MonthTargetToDateKwh = generated.NewOptFloat64(round(exp.kwh(month, tomorrow), 2))
		out.YearTargetToDateKwh = generated.NewOptFloat64(round(exp.kwh(year, tomorrow), 2))
	}
	// Performance ratio of the month's complete past days with weather data.
	var pr prSum
	for d, kwh := range monthByDay {
		if e, ok := exp.days[d]; ok && e.IrradiationKWhM2 > 0 && d < today.Format(time.DateOnly) {
			pr.add(kwh, e.IrradiationKWhM2)
		}
	}
	if v, ok := pr.pct(s.PeakPowerKWp); ok {
		out.MonthPerformanceRatioPct = generated.NewOptFloat64(round(v, 1))
	}
	if out.OpenAlerts, err = h.alerting.CountOpenAlerts(ctx, s.ID, now); err != nil {
		return nil, err
	}
	return out, nil
}

// --- export --------------------------------------------------------------

func (h *Handler) ExportEnergy(ctx context.Context, params generated.ExportEnergyParams) (generated.ExportEnergyOK, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataExport); err != nil {
		return generated.ExportEnergyOK{}, err
	}
	if !params.From.Before(params.To) {
		return generated.ExportEnergyOK{}, httpapi.BadRequest("from must be before to")
	}
	if params.To.Sub(params.From) > maxExportDays*24*time.Hour {
		return generated.ExportEnergyOK{}, httpapi.BadRequest("range too large")
	}
	invs, err := h.systems.ListInverters(ctx, params.SystemId)
	if err != nil {
		return generated.ExportEnergyOK{}, err
	}
	names := map[uuid.UUID]string{}
	for _, inv := range invs {
		names[inv.ID] = inv.Name
	}
	switch params.Granularity {
	case generated.ExportEnergyGranularity5min, generated.ExportEnergyGranularityMinute:
		return h.exportPower(ctx, params, names)
	}
	bs, err := h.telemetry.Energy(ctx, params.SystemId, string(params.Granularity), params.From, params.To)
	if err != nil {
		return generated.ExportEnergyOK{}, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"date", "inverter_id", "inverter", "energy_kwh"})
	for _, b := range bs {
		_ = w.Write([]string{
			b.Start.Format(time.DateOnly), b.InverterID.String(), csvSafe(names[b.InverterID]),
			strconv.FormatFloat(b.EnergyWh/1000, 'f', 3, 64),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return generated.ExportEnergyOK{}, err
	}
	return generated.ExportEnergyOK{Data: &buf}, nil
}

// exportPower writes power samples (minute) or 5-minute means as CSV. The
// dates are local days of the system; times are written in its time zone.
func (h *Handler) exportPower(ctx context.Context, params generated.ExportEnergyParams, names map[uuid.UUID]string) (generated.ExportEnergyOK, error) {
	if calendarDays(params.From, params.To) > maxPowerExportDays {
		return generated.ExportEnergyOK{}, httpapi.BadRequest("power exports cover at most 31 days")
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return generated.ExportEnergyOK{}, err
	}
	loc := sys.Loc()
	from := time.Date(params.From.Year(), params.From.Month(), params.From.Day(), 0, 0, 0, 0, loc)
	to := time.Date(params.To.Year(), params.To.Month(), params.To.Day(), 0, 0, 0, 0, loc)
	var rows []telemetry.Measurement
	if params.Granularity == generated.ExportEnergyGranularityMinute {
		if rows, err = h.telemetry.Samples(ctx, sys.ID, from, to); err != nil {
			return generated.ExportEnergyOK{}, err
		}
	} else {
		points, err := h.telemetry.PowerCurve(ctx, sys.ID, from, to, dayBucket)
		if err != nil {
			return generated.ExportEnergyOK{}, err
		}
		rows = make([]telemetry.Measurement, len(points))
		for i, pt := range points {
			rows[i] = telemetry.Measurement{Time: pt.Time, InverterID: pt.InverterID, PacW: &pt.PacW, PdcW: &pt.PdcW, UdcV: pt.UdcV, TemperatureC: pt.TemperatureC}
		}
	}
	cell := func(v *float64, digits int) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(round(*v, digits), 'f', -1, 64)
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time", "inverter_id", "inverter", "pac_w", "pdc_w", "udc_v", "temperature_c"})
	for _, m := range rows {
		_ = w.Write([]string{
			m.Time.In(loc).Format(time.RFC3339), m.InverterID.String(), csvSafe(names[m.InverterID]),
			cell(m.PacW, 0), cell(m.PdcW, 0), cell(m.UdcV, 1), cell(m.TemperatureC, 1),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return generated.ExportEnergyOK{}, err
	}
	return generated.ExportEnergyOK{Data: &buf}, nil
}

// csvSafe prevents spreadsheet formula injection via user-controlled names.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
