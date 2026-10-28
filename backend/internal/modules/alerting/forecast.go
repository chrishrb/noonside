package alerting

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
)

// The morning forecast goes out once a day between these local hours (the
// scheduler runs every minute; a missed morning is skipped, not sent late).
const (
	forecastFrom = 7
	forecastTo   = 10
)

// Forecaster pushes the day's expected yield to members who opted in
// (notification_prefs.forecast_push).
type Forecaster struct {
	store    Store
	expected ExpectedFunc
	advice   AdviceFunc
	now      func() time.Time
	logger   *slog.Logger
}

func NewForecaster(st Store, expected ExpectedFunc) *Forecaster {
	return &Forecaster{store: st, expected: expected, now: time.Now, logger: slog.Default().With("component", "forecast")}
}

// AdviceFunc returns today's advice for large loads (implemented by
// advice.Service.Today).
type AdviceFunc func(ctx context.Context, sys *systems.System, hours int) (*advice.Advice, error)

// WithAdvice adds the day's best window for large loads to the message.
func (f *Forecaster) WithAdvice(a AdviceFunc) *Forecaster {
	f.advice = a
	return f
}

// SetClock replaces the clock (tests).
func (f *Forecaster) SetClock(now func() time.Time) { f.now = now }

// Run sends the morning forecast of every system where it is morning and
// someone opted in, at most once a day per system.
func (f *Forecaster) Run(ctx context.Context, deadline time.Time) error {
	systems, err := f.store.ListSystems(ctx, nil)
	if err != nil {
		return err
	}
	for _, sw := range systems {
		if f.now().After(deadline) {
			return nil
		}
		if err := f.RunSystem(ctx, sw.System); err != nil {
			f.logger.Warn("morning forecast", "system_id", sw.ID, "error", err)
		}
	}
	return nil
}

// RunSystem sends the system's morning forecast if it is due.
func (f *Forecaster) RunSystem(ctx context.Context, sys systems.System) error {
	now := f.now()
	local := now.In(sys.Loc())
	if local.Hour() < forecastFrom || local.Hour() >= forecastTo || sys.Latitude == nil || sys.PeakPowerKWp <= 0 {
		return nil
	}
	recipients, err := f.store.ListRecipients(ctx, sys.ID)
	if err != nil {
		return err
	}
	var to []notifications.Recipient
	for _, r := range recipients {
		if r.Prefs.ForecastPush && !notifications.Muted(r.Prefs, now) {
			if ch, inApp := notifications.Deliveries(r.Prefs.ForecastChannels, r.Targets); inApp || len(ch) > 0 {
				to = append(to, r)
			}
		}
	}
	if len(to) == 0 {
		return nil
	}
	// Once a day: the lease records the run and blocks the rest of the morning.
	unlock, ok, err := f.store.TryLockJob(ctx, "forecast_push:"+sys.ID.String(), 5*time.Minute, (forecastTo-forecastFrom+1)*time.Hour)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sys.Loc())
	days, err := f.expected(ctx, &sys, today, today.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	day, ok := days[today.Format(time.DateOnly)]
	if !ok {
		return nil // no forecast (weather disabled or unavailable)
	}
	payload := map[string]any{
		"system_name": sys.Name, "date": today.Format(time.DateOnly), "timezone": sys.Timezone,
		"expected_kwh": float64(int(day.KWhPerKWp*sys.PeakPowerKWp*10+0.5)) / 10,
	}
	if f.advice != nil {
		// Without it the message only lacks the window.
		if a, err := f.advice(ctx, &sys, advice.DefaultHours); err != nil {
			f.logger.Warn("advice for the morning forecast", "system_id", sys.ID, "error", err)
		} else if a.Solar != nil {
			payload["window_start"] = a.Solar.Start.UTC().Format(time.RFC3339)
			payload["window_end"] = a.Solar.End.UTC().Format(time.RFC3339)
		}
	}
	for _, r := range to {
		ch, inApp := notifications.Deliveries(r.Prefs.ForecastChannels, r.Targets)
		n := &notifications.Notification{
			UserID: r.UserID, SystemID: &sys.ID, Type: notifications.NotificationForecast, Severity: notifications.SeverityInfo, Payload: payload,
		}
		if !inApp {
			n.ReadAt = &now // in the list, but not counted as unread
		}
		if _, err := f.store.CreateNotification(ctx, n, ch); err != nil {
			return err
		}
	}
	return nil
}
