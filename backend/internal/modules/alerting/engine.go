package alerting

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
)

const (
	// Every is how often the rules are evaluated.
	Every = 5 * time.Minute
	// resolveAfter evaluations without the condition resolve an alert, so a
	// condition that flickers doesn't open and resolve it repeatedly.
	resolveAfter = 2
	// remindAfter re-notifies about an alert that is still open, unacked and
	// not snoozed.
	remindAfter = 24 * time.Hour
)

// ExpectedFunc returns the weather-based expectation by local date in
// [from, to), or nil if the system can't have one (implemented by
// weather.Service.ExpectedDays).
type ExpectedFunc func(ctx context.Context, sys *systems.System, from, to time.Time) (map[string]weather.ExpectedDay, error)

// AlertHook is told when an alert opens or resolves (typ is the
// notification type), e.g. to queue the system's webhooks.
type AlertHook func(ctx context.Context, sys systems.System, a Alert, typ, subject string) error

type Engine struct {
	store    Store
	expected ExpectedFunc
	hook     AlertHook
	now      func() time.Time
	logger   *slog.Logger

	// curves caches each system's power curves of the last days, which only
	// change when late data arrives, for curveTTL.
	mu     sync.Mutex
	curves map[uuid.UUID]curveCache
}

type curveCache struct {
	from    time.Time
	fetched time.Time
	points  []telemetry.PowerPoint
}

const curveTTL = time.Hour

func NewEngine(st Store) *Engine {
	return &Engine{store: st, now: time.Now, logger: slog.Default().With("component", "alerts"), curves: map[uuid.UUID]curveCache{}}
}

// WithExpected enables the rules that compare with the weather.
func (e *Engine) WithExpected(f ExpectedFunc) *Engine {
	e.expected = f
	return e
}

// SetClock replaces the clock (tests).
func (e *Engine) SetClock(now func() time.Time) { e.now = now }

// WithHook calls hook when an alert opens or resolves.
func (e *Engine) WithHook(hook AlertHook) *Engine {
	e.hook = hook
	return e
}

// Run evaluates all systems unless another run did within Every. It stops
// starting new systems after deadline.
func (e *Engine) Run(ctx context.Context, deadline time.Time) error {
	// Slightly shorter than Every so a run driven by a one-minute timer
	// doesn't slip by a tick.
	unlock, ok, err := e.store.TryLockJob(ctx, "alerts", 5*time.Minute, Every-20*time.Second)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	systems, err := e.store.ListSystems(ctx, nil)
	if err != nil {
		return err
	}
	for _, sys := range systems {
		if e.now().After(deadline) {
			e.logger.Info("alert evaluation paused, continuing next run")
			break
		}
		if err := e.EvaluateSystem(ctx, sys.System); err != nil {
			e.logger.Error("evaluate alerts", "system_id", sys.ID, "error", err)
		}
	}
	return nil
}

// EvaluateSystem runs the system's rules once and updates its alerts.
func (e *Engine) EvaluateSystem(ctx context.Context, sys systems.System) error {
	overrides, err := e.store.ListAlertRules(ctx, sys.ID)
	if err != nil {
		return err
	}
	rules := Effective(overrides)
	snap, err := e.snapshot(ctx, sys, rules)
	if err != nil {
		return err
	}
	ev := Evaluate(snap, rules)
	open, err := e.store.ListAlerts(ctx, sys.ID, AlertFilter{OpenOnly: true, Limit: 500})
	if err != nil {
		return err
	}

	n := &notifier{e: e, sys: sys, snap: snap}
	byKey := map[string]Alert{}
	for _, a := range open {
		byKey[a.Kind+"|"+a.SubjectKey] = a
	}
	seen := map[string]bool{}
	for _, f := range ev.Findings {
		seen[f.key()] = true
		if a, ok := byKey[f.key()]; ok {
			if err := e.store.SeeAlert(ctx, a.ID, snap.Now, f.Details); err != nil {
				return err
			}
			a.Details = f.Details
			if err := n.maybeRemind(ctx, a); err != nil {
				return err
			}
			continue
		}
		def, _ := Rule(f.Kind)
		a, err := e.store.OpenAlert(ctx, &Alert{
			SystemID: sys.ID, Kind: f.Kind, SubjectKey: f.SubjectKey,
			InverterID: f.InverterID, DataSourceID: f.DataSourceID,
			Severity: def.Severity, Details: f.Details, OpenedAt: snap.Now,
		})
		if errors.Is(err, db.ErrConflict) {
			continue // opened concurrently
		}
		if err != nil {
			return err
		}
		e.logger.Info("alert opened", "system_id", sys.ID, "kind", a.Kind, "subject", a.SubjectKey)
		if err := n.notify(ctx, *a, notifications.NotificationAlertOpened); err != nil {
			return err
		}
		n.callHook(ctx, *a, notifications.NotificationAlertOpened)
	}

	enabled := map[string]bool{}
	for _, r := range rules {
		enabled[r.Kind] = r.Enabled
	}
	for key, a := range byKey {
		// A held rule couldn't judge (e.g. no_data at night): keep the alert
		// open without counting a miss.
		if seen[key] || ev.Held[a.Kind] {
			continue
		}
		// A disabled rule resolves its alerts at once.
		if enabled[a.Kind] {
			missed, err := e.store.MissAlert(ctx, a.ID)
			if err != nil {
				return err
			}
			if missed < resolveAfter {
				continue
			}
		}
		if err := e.store.ResolveAlert(ctx, a.ID, snap.Now); err != nil {
			return err
		}
		e.logger.Info("alert resolved", "system_id", sys.ID, "kind", a.Kind, "subject", a.SubjectKey)
		if err := n.notify(ctx, a, notifications.NotificationAlertResolved); err != nil {
			return err
		}
		resolved := snap.Now
		a.ResolvedAt = &resolved
		n.callHook(ctx, a, notifications.NotificationAlertResolved)
	}
	return nil
}

func (e *Engine) snapshot(ctx context.Context, sys systems.System, rules []EffectiveRule) (*Snapshot, error) {
	now := e.now()
	s := &Snapshot{
		Now: now, System: sys,
		States:     map[uuid.UUID]telemetry.InverterState{},
		OpenEvents: map[uuid.UUID]telemetry.InverterEvent{},
		Yesterday:  map[uuid.UUID]float64{}, DayBefore: map[uuid.UUID]float64{},
	}
	var err error
	if s.Inverters, err = e.store.ListInverters(ctx, sys.ID); err != nil {
		return nil, err
	}
	states, err := e.store.ListInverterStates(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	for _, st := range states {
		s.States[st.InverterID] = st
		if st.ErrorCode != nil && *st.ErrorCode != 0 {
			ev, err := e.store.OpenEvent(ctx, st.InverterID)
			if err != nil && !errors.Is(err, db.ErrNotFound) {
				return nil, err
			}
			if ev != nil {
				s.OpenEvents[st.InverterID] = *ev
			}
		}
	}
	if s.DataSources, err = e.store.ListDataSources(ctx, sys.ID); err != nil {
		return nil, err
	}

	// Local calendar days, encoded as midnight UTC like inverter_daily.
	local := now.In(sys.Loc())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	s.YesterdayDate = today.AddDate(0, 0, -1)
	buckets, err := e.store.Energy(ctx, sys.ID, "day", today.AddDate(0, 0, -2), today)
	if err != nil {
		return nil, err
	}
	for _, b := range buckets {
		if b.Start.Equal(s.YesterdayDate) {
			s.Yesterday[b.InverterID] += b.EnergyWh
		} else {
			s.DayBefore[b.InverterID] += b.EnergyWh
		}
	}
	if s.FirstDay, err = e.store.FirstDataDay(ctx, sys.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}

	loc := sys.Loc()
	localToday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	enabled := map[string]bool{}
	for _, r := range rules {
		enabled[r.Kind] = r.Enabled
	}
	if e.expected != nil && enabled[KindLowYield] {
		// A weather error only skips the rule (held until it can judge).
		if s.Expected, err = e.expected(ctx, &sys, localToday.AddDate(0, 0, -1), localToday); err != nil {
			e.logger.Warn("expected energy for alerts", "system_id", sys.ID, "error", err)
			s.Expected = map[string]weather.ExpectedDay{}
		}
	}
	if enabled[KindImplausibleData] {
		if s.Rejections, err = e.store.ListIngestRejections(ctx, sys.ID, s.YesterdayDate); err != nil {
			return nil, err
		}
	}
	for kind := range needsCurves {
		if enabled[kind] {
			if s.Curves, err = e.recentCurves(ctx, sys.ID, localToday.AddDate(0, 0, -CurveDays), localToday); err != nil {
				return nil, err
			}
			break
		}
	}
	return s, nil
}

// recentCurves returns the 5-minute power of the system in [from, to),
// cached for curveTTL.
func (e *Engine) recentCurves(ctx context.Context, systemID uuid.UUID, from, to time.Time) ([]telemetry.PowerPoint, error) {
	now := e.now()
	e.mu.Lock()
	c, ok := e.curves[systemID]
	e.mu.Unlock()
	if ok && c.from.Equal(from) && now.Sub(c.fetched) < curveTTL {
		return c.points, nil
	}
	pts, err := e.store.PowerCurve(ctx, systemID, from, to, curveBucket)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.curves[systemID] = curveCache{from: from, fetched: now, points: pts}
	e.mu.Unlock()
	return pts, nil
}

// notifier fans alerts out to a system's members.
type notifier struct {
	e          *Engine
	sys        systems.System
	snap       *Snapshot
	recipients []notifications.Recipient
	loaded     bool
}

func (n *notifier) maybeRemind(ctx context.Context, a Alert) error {
	now := n.snap.Now
	last := a.OpenedAt
	if a.RemindedAt != nil {
		last = *a.RemindedAt
	}
	if a.AckedAt != nil || (a.SnoozedUntil != nil && a.SnoozedUntil.After(now)) || now.Sub(last) < remindAfter {
		return nil
	}
	if err := n.e.store.MarkAlertReminded(ctx, a.ID, now); err != nil {
		return err
	}
	return n.notify(ctx, a, notifications.NotificationAlertReminder)
}

// SubjectName names what the alert is about.
func SubjectName(a Alert, sys systems.System, inverters []systems.Inverter, sources []datasources.DataSource) string {
	switch {
	case a.InverterID != nil:
		for _, inv := range inverters {
			if inv.ID == *a.InverterID {
				return inv.Name
			}
		}
	case a.DataSourceID != nil:
		for _, ds := range sources {
			if ds.ID == *a.DataSourceID {
				return ds.Name
			}
		}
	}
	return sys.Name
}

// callHook tells the engine's hook; a failure is logged, the alert stays.
func (n *notifier) callHook(ctx context.Context, a Alert, typ string) {
	if n.e.hook == nil {
		return
	}
	subject := SubjectName(a, n.sys, n.snap.Inverters, n.snap.DataSources)
	if err := n.e.hook(ctx, n.sys, a, typ, subject); err != nil {
		n.e.logger.Warn("alert hook", "system_id", n.sys.ID, "alert_id", a.ID, "error", err)
	}
}

func (n *notifier) notify(ctx context.Context, a Alert, typ string) error {
	if !n.loaded {
		var err error
		if n.recipients, err = n.e.store.ListRecipients(ctx, n.sys.ID); err != nil {
			return err
		}
		n.loaded = true
	}
	// A snoozed alert only reports its resolution.
	if typ != notifications.NotificationAlertResolved && a.SnoozedUntil != nil && a.SnoozedUntil.After(n.snap.Now) {
		return nil
	}
	payload := map[string]any{
		"kind":        a.Kind,
		"system_name": n.sys.Name,
		"subject":     SubjectName(a, n.sys, n.snap.Inverters, n.snap.DataSources),
		"details":     a.Details,
		"timezone":    n.sys.Timezone,
	}
	for _, r := range n.recipients {
		channels, inApp, ok := notifications.Channels(r.Prefs, r.Targets, a.Severity, typ, n.snap.Now)
		if !ok {
			continue
		}
		note := &notifications.Notification{
			UserID: r.UserID, SystemID: &n.sys.ID, AlertID: &a.ID,
			Type: typ, Severity: a.Severity, Payload: payload,
		}
		if !inApp {
			note.ReadAt = &n.snap.Now // not shown as unread in the app
		}
		if _, err := n.e.store.CreateNotification(ctx, note, channels); err != nil {
			return err
		}
	}
	return nil
}
