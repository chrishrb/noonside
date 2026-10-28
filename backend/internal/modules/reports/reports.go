// Package reports sends periodic yield reports by email: daily, weekly or
// monthly as members chose (notification_prefs.digest), plus a yearly report
// in January. Reports go through the notification outbox, so the dispatcher
// retries failed emails.
package reports

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/google/uuid"
)

// Report kinds. Daily, weekly and monthly are digest settings; yearly goes to
// everyone with any of them.
const (
	KindDaily   = "daily"
	KindWeekly  = "weekly"
	KindMonthly = "monthly"
	KindYearly  = "yearly"
)

// Reports go out once per period between these local hours (the scheduler
// runs every minute; a missed window is skipped, not sent late).
const (
	sendFrom = 6
	sendTo   = 12
)

// Report is the content of one report, stored as the notification payload
// and rendered per recipient in their language.
type Report struct {
	Kind       string    `json:"kind"`
	SystemID   uuid.UUID `json:"system_id"`
	SystemName string    `json:"system_name"`
	Timezone   string    `json:"timezone"`
	Currency   string    `json:"currency"`
	// From and To are local dates (YYYY-MM-DD); To is exclusive.
	From string `json:"from"`
	To   string `json:"to"`

	EnergyKWh   float64  `json:"energy_kwh"`
	KWhPerKWp   *float64 `json:"kwh_per_kwp,omitempty"`
	ExpectedKWh *float64 `json:"expected_kwh,omitempty"`
	// ExpectedSource is "weather" when any day's expectation came from the
	// weather, else "targets".
	ExpectedSource string  `json:"expected_source,omitempty"`
	BestDay        string  `json:"best_day,omitempty"`
	BestDayKWh     float64 `json:"best_day_kwh,omitempty"`
	Revenue        float64 `json:"revenue"`
	// NegativePriceKWh is the energy produced while the day-ahead price was
	// negative (with a price zone); Revenue leaves it out when the system
	// is unpaid at negative prices.
	NegativePriceKWh *float64 `json:"negative_price_kwh,omitempty"`
	CO2Kg            float64  `json:"co2_kg"`
	PreviousKWh      *float64 `json:"previous_kwh,omitempty"` // yearly: the year before

	Inverters []InverterShare `json:"inverters"`
	Alerts    []AlertLine     `json:"alerts"`
	// Link is the app path of the period's overview.
	Link string `json:"link"`
}

type InverterShare struct {
	Name      string   `json:"name"`
	EnergyKWh float64  `json:"energy_kwh"`
	SharePct  float64  `json:"share_pct"`
	KWhPerKWp *float64 `json:"kwh_per_kwp,omitempty"`
}

// AlertLine is an alert opened in the period.
type AlertLine struct {
	Kind     string         `json:"kind"`
	Subject  string         `json:"subject"`
	Severity string         `json:"severity"`
	OpenedAt time.Time      `json:"opened_at"`
	Details  map[string]any `json:"details,omitempty"`
}

// Payload converts the report into a notification payload.
func (r *Report) Payload() (map[string]any, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

// FromPayload reads a report back from a notification payload.
func FromPayload(p map[string]any) (*Report, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var r Report
	return &r, json.Unmarshal(b, &r)
}

type Service struct {
	store    Store
	prices   Prices
	expected alerting.ExpectedFunc
	now      func() time.Time
	logger   *slog.Logger
}

// NewService builds reports; expected may be nil (targets only).
func NewService(st Store, pr Prices, expected alerting.ExpectedFunc) *Service {
	notifications.RegisterRenderer(notifications.NotificationReport, Notification)
	return &Service{store: st, prices: pr, expected: expected, now: time.Now, logger: slog.Default().With("component", "reports")}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Run queues the due reports of every system.
func (s *Service) Run(ctx context.Context, deadline time.Time) error {
	unlock, ok, err := s.store.TryLockJob(ctx, "reports", 5*time.Minute, 0)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	systems, err := s.store.ListSystems(ctx, nil)
	if err != nil {
		return err
	}
	for _, sw := range systems {
		if s.now().After(deadline) {
			return nil
		}
		if err := s.RunSystem(ctx, sw.System); err != nil {
			s.logger.Warn("yield report", "system_id", sw.ID, "error", err)
		}
	}
	return nil
}

// Due returns the report kinds due at local time t with their periods
// [from, to): yesterday, last week (Monday to Sunday), last month and, in
// January, last year.
func Due(t time.Time) map[string][2]time.Time {
	if t.Hour() < sendFrom || t.Hour() >= sendTo {
		return nil
	}
	today := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	out := map[string][2]time.Time{KindDaily: {today.AddDate(0, 0, -1), today}}
	if today.Weekday() == time.Monday {
		out[KindWeekly] = [2]time.Time{today.AddDate(0, 0, -7), today}
	}
	if today.Day() == 1 {
		out[KindMonthly] = [2]time.Time{today.AddDate(0, -1, 0), today}
		if today.Month() == time.January {
			out[KindYearly] = [2]time.Time{today.AddDate(-1, 0, 0), today}
		}
	}
	return out
}

// RunSystem queues the system's due reports for the members who want them,
// at most once per period.
func (s *Service) RunSystem(ctx context.Context, sys systems.System) error {
	now := s.now()
	due := Due(now.In(sys.Loc()))
	if len(due) == 0 {
		return nil
	}
	recipients, err := s.store.ListRecipients(ctx, sys.ID)
	if err != nil {
		return err
	}
	for _, kind := range []string{KindDaily, KindWeekly, KindMonthly, KindYearly} {
		period, ok := due[kind]
		if !ok {
			continue
		}
		var to []notifications.Recipient
		for _, r := range recipients {
			p := r.Prefs
			wants := p.Digest == kind || (kind == KindYearly && p.Digest != "" && p.Digest != "none")
			if !wants || notifications.Muted(p, now) {
				continue
			}
			if ch, inApp := notifications.Deliveries(p.ReportChannels, r.Targets); inApp || len(ch) > 0 {
				to = append(to, r)
			}
		}
		if len(to) == 0 {
			continue
		}
		if err := s.send(ctx, sys, kind, period[0], period[1], to); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) send(ctx context.Context, sys systems.System, kind string, from, to time.Time, recipients []notifications.Recipient) error {
	// Once per period: the lease records the run and blocks the rest of the
	// send window.
	unlock, ok, err := s.store.TryLockJob(ctx, "report:"+kind+":"+sys.ID.String(), 5*time.Minute, (sendTo-sendFrom+1)*time.Hour)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	rep, err := s.Build(ctx, &sys, kind, from, to)
	if err != nil {
		return err
	}
	payload, err := rep.Payload()
	if err != nil {
		return err
	}
	now := s.now()
	for _, r := range recipients {
		ch, inApp := notifications.Deliveries(r.Prefs.ReportChannels, r.Targets)
		n := &notifications.Notification{
			UserID: r.UserID, SystemID: &sys.ID, Type: notifications.NotificationReport, Severity: notifications.SeverityInfo, Payload: payload,
		}
		if !inApp {
			n.ReadAt = &now // in the list, but not counted as unread
		}
		if _, err := s.store.CreateNotification(ctx, n, ch); err != nil {
			return err
		}
	}
	return nil
}

// Build computes the report of the local days in [from, to).
func (s *Service) Build(ctx context.Context, sys *systems.System, kind string, from, to time.Time) (*Report, error) {
	rep := &Report{
		Kind: kind, SystemID: sys.ID, SystemName: sys.Name, Timezone: sys.Timezone, Currency: sys.Currency,
		From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
		Inverters: []InverterShare{}, Alerts: []AlertLine{},
		Link: link(sys.ID, kind, from),
	}
	invs, err := s.store.ListInverters(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	bs, err := s.store.Energy(ctx, sys.ID, "day", from, to)
	if err != nil {
		return nil, err
	}
	perInv := map[uuid.UUID]float64{}
	perDay := map[string]float64{}
	for _, b := range bs {
		kwh := b.EnergyWh / 1000
		rep.EnergyKWh += kwh
		perInv[b.InverterID] += kwh
		perDay[b.Start.Format(time.DateOnly)] += kwh
	}
	if kind != KindDaily {
		for d, kwh := range perDay {
			if kwh > rep.BestDayKWh || (kwh == rep.BestDayKWh && d < rep.BestDay) {
				rep.BestDay, rep.BestDayKWh = d, round(kwh, 1)
			}
		}
	}
	if sys.PeakPowerKWp > 0 {
		rep.KWhPerKWp = ptr(round(rep.EnergyKWh/sys.PeakPowerKWp, 2))
	}
	rep.Revenue = round(rep.EnergyKWh*sys.TariffPerKWh, 2)
	neg, err := s.prices.NegativeDuring(ctx, sys, from, to, nil)
	if err != nil {
		return nil, err
	}
	if neg != nil {
		rep.NegativePriceKWh = ptr(round(neg.KWh, 1))
		rep.Revenue = round(rep.EnergyKWh*sys.TariffPerKWh-neg.UnpaidRevenue, 2)
	}
	rep.CO2Kg = round(rep.EnergyKWh*sys.CO2KgPerKWh, 1)

	sort.SliceStable(invs, func(i, j int) bool { return invs[i].SortOrder < invs[j].SortOrder })
	if len(invs) > 1 {
		for _, inv := range invs {
			e := perInv[inv.ID]
			sh := InverterShare{Name: inv.Name, EnergyKWh: round(e, 2)}
			if rep.EnergyKWh > 0 {
				sh.SharePct = round(e/rep.EnergyKWh*100, 1)
			}
			if inv.PeakPowerKWp > 0 {
				sh.KWhPerKWp = ptr(round(e/inv.PeakPowerKWp, 2))
			}
			rep.Inverters = append(rep.Inverters, sh)
		}
	}

	if err := s.expectation(ctx, sys, from, to, rep); err != nil {
		return nil, err
	}
	if kind == KindYearly {
		prev, err := s.energy(ctx, sys.ID, from.AddDate(-1, 0, 0), from)
		if err != nil {
			return nil, err
		}
		if prev > 0 {
			rep.PreviousKWh = ptr(round(prev, 1))
		}
	}

	as, err := s.store.ListAlertsOpenedIn(ctx, sys.ID, from, to)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, inv := range invs {
		names[inv.ID.String()] = inv.Name
	}
	for _, a := range as {
		subject := sys.Name
		if n, ok := names[a.SubjectKey]; ok {
			subject = n
		}
		rep.Alerts = append(rep.Alerts, AlertLine{Kind: a.Kind, Subject: subject, Severity: string(a.Severity), OpenedAt: a.OpenedAt, Details: a.Details})
	}
	rep.EnergyKWh = round(rep.EnergyKWh, 1)
	return rep, nil
}

// expectation sets the expected energy: from the weather per day where
// known, otherwise from the monthly targets.
func (s *Service) expectation(ctx context.Context, sys *systems.System, from, to time.Time, rep *Report) error {
	targets, err := s.store.GetTargets(ctx, sys.ID)
	if err != nil {
		return err
	}
	var days map[string]weather.ExpectedDay
	if s.expected != nil && sys.PeakPowerKWp > 0 {
		if days, err = s.expected(ctx, sys, from, to); err != nil {
			s.logger.Warn("expected daily energy", "system_id", sys.ID, "error", err)
		}
	}
	total, weather, known := 0.0, false, false
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		if e, ok := days[d.Format(time.DateOnly)]; ok {
			total += e.KWhPerKWp * sys.PeakPowerKWp
			weather, known = true, true
		} else if v, ok := targets[int(d.Month())]; ok {
			total += v / float64(daysIn(d.Year(), d.Month()))
			known = true
		}
	}
	if !known {
		return nil
	}
	rep.ExpectedKWh = ptr(round(total, 1))
	rep.ExpectedSource = "targets"
	if weather {
		rep.ExpectedSource = "weather"
	}
	return nil
}

// energy is the system's energy in [from, to).
func (s *Service) energy(ctx context.Context, systemID uuid.UUID, from, to time.Time) (float64, error) {
	bs, err := s.store.Energy(ctx, systemID, "year", from, to)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, b := range bs {
		total += b.EnergyWh / 1000
	}
	return total, nil
}

// link is the app path of the report period's overview. Weeks open the
// month of their first day.
func link(systemID uuid.UUID, kind string, from time.Time) string {
	period := map[string]string{KindDaily: "day", KindWeekly: "month", KindMonthly: "month", KindYearly: "year"}[kind]
	return "/s/" + systemID.String() + "/overview?period=" + period + "&date=" + from.Format(time.DateOnly)
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func ptr[T any](v T) *T { return &v }
