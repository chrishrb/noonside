// Package alerts evaluates health rules for each system, keeps alerts
// (incidents) open while their condition holds and turns them into
// notifications for the system's members.
package alerting

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/google/uuid"
)

// Rule kinds.
const (
	KindNoData            = "no_data"
	KindInverterOffline   = "inverter_offline"
	KindInverterError     = "inverter_error"
	KindDataSourceFailing = "data_source_failing"
	KindUnderperformance  = "underperformance"
	KindZeroDay           = "zero_day"
	KindLowYield          = "low_yield_vs_expected"
	KindClipping          = "clipping"
	KindFeedInCap         = "feed_in_cap"
	KindImplausibleData   = "implausible_data"
)

// RuleDef is a built-in rule with its default parameters. Systems override
// Enabled and Params through AlertRule.
type RuleDef struct {
	Kind     string
	Severity notifications.Severity
	Params   map[string]float64
	// eval returns the findings, or held if the rule can't judge right now
	// (e.g. at night), so its open alerts neither resolve nor update.
	eval func(s *Snapshot, p map[string]float64) (fs []Finding, held bool)
}

// Rules lists the built-in rules in display order.
var Rules = []RuleDef{
	{KindNoData, notifications.SeverityWarning, map[string]float64{"minutes": 60, "min_sun_elevation": 10}, evalNoData},
	{KindInverterOffline, notifications.SeverityWarning, map[string]float64{"minutes": 60, "min_sun_elevation": 10}, evalInverterOffline},
	{KindInverterError, notifications.SeverityError, map[string]float64{"minutes": 15}, evalInverterError},
	{KindDataSourceFailing, notifications.SeverityWarning, map[string]float64{"hours": 2}, evalDataSourceFailing},
	{KindUnderperformance, notifications.SeverityWarning, map[string]float64{"percent": 70, "min_kwh_per_kwp": 0.5}, evalUnderperformance},
	{KindZeroDay, notifications.SeverityWarning, map[string]float64{}, evalZeroDay},
	{KindLowYield, notifications.SeverityWarning, map[string]float64{"percent_of_expected": 60, "min_expected_kwh_per_kwp": 1.5}, evalLowYield},
	{KindClipping, notifications.SeverityInfo, map[string]float64{"minutes_per_day": 30, "days": 3}, evalClipping},
	{KindFeedInCap, notifications.SeverityInfo, map[string]float64{"minutes_per_day": 60, "days": 3}, evalFeedInCap},
	{KindImplausibleData, notifications.SeverityWarning, map[string]float64{"min_count": 10}, evalImplausibleData},
}

// needsCurves are the rules that look at the power curves of the last days.
var needsCurves = map[string]bool{KindClipping: true, KindFeedInCap: true}

// dayRules judge complete past days. They wait until the sun is up: right
// after midnight the last uploads of the day before may still be missing,
// and nobody wants an alert about yesterday in the middle of the night.
var dayRules = map[string]bool{
	KindUnderperformance: true, KindZeroDay: true, KindLowYield: true,
	KindClipping: true, KindFeedInCap: true, KindImplausibleData: true,
}

// Rule returns the built-in rule of the given kind.
func Rule(kind string) (RuleDef, bool) {
	i := slices.IndexFunc(Rules, func(r RuleDef) bool { return r.Kind == kind })
	if i < 0 {
		return RuleDef{}, false
	}
	return Rules[i], true
}

// EffectiveRule is a rule with a system's overrides applied.
type EffectiveRule struct {
	RuleDef
	Enabled bool
}

// Effective merges a system's overrides into the defaults. Unknown kinds and
// parameters are ignored.
func Effective(overrides []AlertRule) []EffectiveRule {
	out := make([]EffectiveRule, len(Rules))
	for i, def := range Rules {
		r := EffectiveRule{RuleDef: def, Enabled: true}
		r.Params = maps.Clone(def.Params)
		for _, o := range overrides {
			if o.Kind != def.Kind {
				continue
			}
			r.Enabled = o.Enabled
			for k, v := range o.Params {
				if _, ok := r.Params[k]; ok {
					r.Params[k] = v
				}
			}
		}
		out[i] = r
	}
	return out
}

// ValidateParams checks overrides for one rule kind.
func ValidateParams(kind string, params map[string]float64) error {
	def, ok := Rule(kind)
	if !ok {
		return fmt.Errorf("unknown alert rule %q", kind)
	}
	for k, v := range params {
		if _, ok := def.Params[k]; !ok {
			return fmt.Errorf("unknown parameter %q for rule %q", k, kind)
		}
		if v < 0 {
			return fmt.Errorf("parameter %q must not be negative", k)
		}
		if (k == "percent" || k == "percent_of_expected") && v > 100 {
			return fmt.Errorf("parameter %q must be at most 100", k)
		}
		if k == "days" && v > CurveDays {
			return fmt.Errorf("parameter %q must be at most %d", k, CurveDays)
		}
		if k == "min_sun_elevation" && v > 60 {
			return fmt.Errorf("parameter %q must be at most 60", k)
		}
	}
	return nil
}

// Snapshot is everything the rules look at for one system.
type Snapshot struct {
	Now         time.Time
	System      systems.System
	Inverters   []systems.Inverter
	States      map[uuid.UUID]telemetry.InverterState
	OpenEvents  map[uuid.UUID]telemetry.InverterEvent // open events of inverters in error
	DataSources []datasources.DataSource
	// Daily energy (Wh) per inverter for the last two complete local days.
	Yesterday, DayBefore map[uuid.UUID]float64
	YesterdayDate        time.Time
	// FirstDay is the first local date with daily energy, if any.
	FirstDay *time.Time
	// Expected is the weather-based expectation by local date (yesterday);
	// nil if the system can't have one (no weather or coordinates).
	Expected map[string]weather.ExpectedDay
	// Curves is the 5-minute mean power per inverter over the last
	// CurveDays complete local days, loaded only for rules that need it.
	Curves []telemetry.PowerPoint
	// Rejections are yesterday's implausible values dropped at ingest,
	// loaded only when implausible_data is enabled.
	Rejections []telemetry.IngestRejection
}

// Finding is a condition a rule detected now.
type Finding struct {
	Kind         string
	SubjectKey   string
	InverterID   *uuid.UUID
	DataSourceID *uuid.UUID
	Details      map[string]any
}

func (f Finding) key() string { return f.Kind + "|" + f.SubjectKey }

// Evaluation is the result of running the rules once.
type Evaluation struct {
	Findings []Finding
	// Held are the kinds whose rule couldn't judge; their open alerts stay
	// as they are.
	Held map[string]bool
}

// Evaluate runs the enabled rules on s.
func Evaluate(s *Snapshot, rules []EffectiveRule) Evaluation {
	ev := Evaluation{Held: map[string]bool{}}
	night := !daylight(s, 0, 0)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if night && dayRules[r.Kind] {
			ev.Held[r.Kind] = true
			continue
		}
		fs, held := r.eval(s, r.Params)
		if held {
			ev.Held[r.Kind] = true
		}
		for _, f := range fs {
			f.Kind = r.Kind
			ev.Findings = append(ev.Findings, f)
		}
	}
	// A failing data source explains missing data; don't report both, but
	// keep an open no_data alert: the data is still missing.
	if slices.ContainsFunc(ev.Findings, func(f Finding) bool { return f.Kind == KindDataSourceFailing }) {
		ev.Findings = slices.DeleteFunc(ev.Findings, func(f Finding) bool { return f.Kind == KindNoData })
		ev.Held[KindNoData] = true
	}
	return ev
}

// daylight reports whether the sun has been above minElevation for the whole
// window before now.
func daylight(s *Snapshot, window time.Duration, minElevation float64) bool {
	return Daylight(&s.System, s.Now, window, minElevation)
}

// Daylight reports whether the sun has been above minElevation at the system
// for the whole window before now. Without coordinates it assumes
// 9:00-17:00 local time.
func Daylight(sys *systems.System, now time.Time, window time.Duration, minElevation float64) bool {
	up := func(t time.Time) bool {
		if sys.Latitude != nil && sys.Longitude != nil {
			return SunElevation(t, *sys.Latitude, *sys.Longitude) > minElevation
		}
		h := t.In(sys.Loc()).Hour()
		return h >= 9 && h < 17
	}
	return up(now) && up(now.Add(-window))
}

// minStaleAfter is the least age at which live data counts as stale.
const minStaleAfter = 30 * time.Minute

// StaleAfter is how old a system's newest reading may get before its live
// data counts as stale: 30 minutes, or two poll intervals of its fastest
// enabled data source if that is longer.
func StaleAfter(sources []datasources.DataSource) time.Duration {
	var fastest time.Duration
	for _, ds := range sources {
		if ds.Enabled && ds.PollInterval > 0 && (fastest == 0 || ds.PollInterval < fastest) {
			fastest = ds.PollInterval
		}
	}
	return max(minStaleAfter, 2*fastest)
}

// Stale reports whether the live data of a system whose newest reading is
// from last is stale at now: older than StaleAfter while the sun has been up
// (above the no_data rule's elevation) all that time. A system without any
// reading is not stale; it has nothing to show yet.
func Stale(sys *systems.System, overrides []AlertRule, sources []datasources.DataSource, last *time.Time, now time.Time) bool {
	if last == nil {
		return false
	}
	after := StaleAfter(sources)
	if now.Sub(*last) <= after {
		return false
	}
	elevation := 10.0
	for _, r := range Effective(overrides) {
		if r.Kind == KindNoData {
			elevation = r.Params["min_sun_elevation"]
		}
	}
	return Daylight(sys, now, after, elevation)
}

func minutes(p map[string]float64, k string) time.Duration {
	return time.Duration(p[k] * float64(time.Minute))
}

func ref[T any](v T) *T { return &v }

func evalNoData(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	window := minutes(p, "minutes")
	if len(s.Inverters) == 0 {
		return nil, false
	}
	if !daylight(s, window, p["min_sun_elevation"]) {
		return nil, true // no data at night is normal, but doesn't resolve anything
	}
	var last time.Time
	for _, st := range s.States {
		if st.Time.After(last) {
			last = st.Time
		}
	}
	// Never received anything: nothing to compare with yet.
	if last.IsZero() || s.Now.Sub(last) < window {
		return nil, false
	}
	return []Finding{{Details: map[string]any{"last_seen": last.Format(time.RFC3339), "minutes": int(s.Now.Sub(last).Minutes())}}}, false
}

// OnlineWindow is how long an inverter counts as online (and its last power
// as current) after its newest reading: as long as the system's data isn't
// stale (StaleAfter). Solar-Log devices upload every 10 minutes, so a single
// missed transfer already leaves a gap of 20 minutes or more.
func OnlineWindow(sources []datasources.DataSource) time.Duration {
	return StaleAfter(sources)
}

func evalInverterOffline(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	window := minutes(p, "minutes")
	if len(s.Inverters) < 2 {
		return nil, false
	}
	if !daylight(s, window, p["min_sun_elevation"]) {
		return nil, true
	}
	online := 0
	onlineWindow := OnlineWindow(s.DataSources)
	for _, st := range s.States {
		if s.Now.Sub(st.Time) < onlineWindow {
			online++
		}
	}
	if online == 0 {
		return nil, true // the whole system is silent: no_data, and the inverter isn't back
	}
	var out []Finding
	for _, inv := range s.Inverters {
		st, ok := s.States[inv.ID]
		if !ok || s.Now.Sub(st.Time) < window {
			continue
		}
		out = append(out, Finding{
			SubjectKey: inv.ID.String(), InverterID: ref(inv.ID),
			Details: map[string]any{"last_seen": st.Time.Format(time.RFC3339), "minutes": int(s.Now.Sub(st.Time).Minutes())},
		})
	}
	return out, false
}

func evalInverterError(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	var out []Finding
	for _, inv := range s.Inverters {
		st, ok := s.States[inv.ID]
		if !ok || st.ErrorCode == nil || *st.ErrorCode == 0 {
			continue
		}
		since := st.Time
		if ev, ok := s.OpenEvents[inv.ID]; ok {
			since = ev.StartedAt
		}
		if s.Now.Sub(since) < minutes(p, "minutes") {
			continue
		}
		out = append(out, Finding{
			SubjectKey: inv.ID.String(), InverterID: ref(inv.ID),
			Details: map[string]any{"code": *st.ErrorCode, "text": st.ErrorText, "since": since.Format(time.RFC3339)},
		})
	}
	return out, false
}

func evalDataSourceFailing(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	var out []Finding
	for _, ds := range s.DataSources {
		if !ds.Enabled || ds.LastError == "" {
			continue
		}
		since := ds.CreatedAt
		if ds.LastSuccessAt != nil {
			since = *ds.LastSuccessAt
		}
		if s.Now.Sub(since) < time.Duration(p["hours"]*float64(time.Hour)) {
			continue
		}
		msg := ds.LastError
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		// source_kind picks the settings section the notification opens.
		d := map[string]any{"error": msg, "name": ds.Name, "source_kind": ds.Kind}
		if ds.LastSuccessAt != nil {
			d["last_success"] = ds.LastSuccessAt.Format(time.RFC3339)
		}
		out = append(out, Finding{SubjectKey: ds.ID.String(), DataSourceID: ref(ds.ID), Details: d})
	}
	return out, false
}

// evalUnderperformance compares each inverter's specific yield (kWh/kWp) with
// the median of the other inverters on the same module plane on the last two
// days. Weather affects them alike, so this needs no forecast. Planes facing
// different ways differ naturally, so each plane with at least two inverters
// is compared on its own, and inverters without a plane form one group; an
// inverter alone on its plane is skipped.
func evalUnderperformance(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	groups := map[uuid.UUID][]systems.Inverter{} // uuid.Nil: no plane
	for _, inv := range s.Inverters {
		if inv.PeakPowerKWp <= 0 {
			continue
		}
		key := uuid.Nil
		if inv.PlaneID != nil {
			key = *inv.PlaneID
		}
		groups[key] = append(groups[key], inv)
	}
	ratios := func(invs []systems.Inverter, energy map[uuid.UUID]float64) map[uuid.UUID]float64 {
		specific := map[uuid.UUID]float64{}
		for _, inv := range invs {
			specific[inv.ID] = energy[inv.ID] / 1000 / inv.PeakPowerKWp
		}
		vals := slices.Collect(maps.Values(specific))
		sort.Float64s(vals)
		med := vals[len(vals)/2]
		if len(vals)%2 == 0 {
			med = (vals[len(vals)/2-1] + vals[len(vals)/2]) / 2
		}
		if med < p["min_kwh_per_kwp"] {
			return nil // dark or snowy day: not meaningful
		}
		out := map[uuid.UUID]float64{}
		for id, v := range specific {
			out[id] = v / med * 100
		}
		return out
	}
	planeNames := map[uuid.UUID]string{}
	for _, pl := range s.System.Planes {
		planeNames[pl.ID] = pl.Name
	}
	var out []Finding
	for _, inv := range s.Inverters { // in inverter order, for stable output
		key := uuid.Nil
		if inv.PlaneID != nil {
			key = *inv.PlaneID
		}
		group := groups[key]
		if len(group) < 2 || group[0].ID != inv.ID {
			continue // alone on its plane, or group already done
		}
		y, b := ratios(group, s.Yesterday), ratios(group, s.DayBefore)
		if y == nil || b == nil {
			continue
		}
		for _, g := range group {
			if y[g.ID] >= p["percent"] || b[g.ID] >= p["percent"] {
				continue
			}
			d := map[string]any{"percent": int(y[g.ID] + 0.5), "day": s.YesterdayDate.Format(time.DateOnly)}
			if name := planeNames[key]; key != uuid.Nil && name != "" {
				d["plane"] = name
			}
			out = append(out, Finding{SubjectKey: g.ID.String(), InverterID: ref(g.ID), Details: d})
		}
	}
	return out, false
}

// evalImplausibleData reports inverters that sent at least min_count
// implausible values yesterday (see ingest's plausibility checks).
func evalImplausibleData(s *Snapshot, p map[string]float64) ([]Finding, bool) {
	count := map[uuid.UUID]int{}
	reasons := map[uuid.UUID][]string{}
	for _, r := range s.Rejections {
		count[r.InverterID] += r.Count
		reasons[r.InverterID] = append(reasons[r.InverterID], r.Reason)
	}
	var out []Finding
	for _, inv := range s.Inverters {
		n := count[inv.ID]
		if n == 0 || float64(n) < p["min_count"] {
			continue
		}
		rs := reasons[inv.ID]
		sort.Strings(rs)
		out = append(out, Finding{
			SubjectKey: inv.ID.String(), InverterID: ref(inv.ID),
			Details: map[string]any{"count": n, "reasons": rs, "day": s.YesterdayDate.Format(time.DateOnly)},
		})
	}
	return out, false
}

// evalZeroDay reports a whole day without production although data kept
// arriving (e.g. a tripped breaker).
func evalZeroDay(s *Snapshot, _ map[string]float64) ([]Finding, bool) {
	// Only for systems with history before yesterday (not a new system).
	if len(s.Inverters) == 0 || s.YesterdayDate.IsZero() || s.FirstDay == nil || !s.FirstDay.Before(s.YesterdayDate) {
		return nil, false
	}
	loc := s.System.Loc()
	d := s.YesterdayDate
	end := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
	seenAfter := false
	for _, st := range s.States {
		if !st.Time.Before(end) {
			seenAfter = true
		}
	}
	if !seenAfter {
		return nil, false
	}
	total := 0.0
	for _, wh := range s.Yesterday {
		total += wh
	}
	if total > 0 {
		return nil, false
	}
	return []Finding{{Details: map[string]any{"day": d.Format(time.DateOnly)}}}, false
}
