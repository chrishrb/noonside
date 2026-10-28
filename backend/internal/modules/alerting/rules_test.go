package alerting

import (
	"maps"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSunElevation(t *testing.T) {
	// Berlin, summer solstice: ~61° at solar noon, below the horizon at midnight.
	noon := time.Date(2024, 6, 21, 11, 10, 0, 0, time.UTC)
	assert.InDelta(t, 60.9, SunElevation(noon, 52.52, 13.405), 1)
	assert.Less(t, SunElevation(time.Date(2024, 6, 21, 23, 0, 0, 0, time.UTC), 52.52, 13.405), 0.0)
	// Winter solstice noon: ~14°.
	assert.InDelta(t, 14.1, SunElevation(time.Date(2024, 12, 21, 11, 12, 0, 0, time.UTC), 52.52, 13.405), 1)
}

var (
	lat, lon = 52.52, 13.405
	// 12:00 local time in summer: the sun is high.
	summerNoon  = time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	summerNight = time.Date(2026, 6, 15, 22, 30, 0, 0, time.UTC)
)

func snapshot(now time.Time, invs ...systems.Inverter) *Snapshot {
	return &Snapshot{
		Now:       now,
		System:    systems.System{ID: uuid.New(), Name: "Home", Timezone: "Europe/Berlin", Latitude: &lat, Longitude: &lon},
		Inverters: invs, States: map[uuid.UUID]telemetry.InverterState{},
		OpenEvents: map[uuid.UUID]telemetry.InverterEvent{},
		Yesterday:  map[uuid.UUID]float64{}, DayBefore: map[uuid.UUID]float64{},
	}
}

func inverter(kwp float64) systems.Inverter {
	return systems.Inverter{ID: uuid.New(), Name: "WR", PeakPowerKWp: kwp}
}

func rules(t *testing.T, overrides ...AlertRule) []EffectiveRule {
	t.Helper()
	return Effective(overrides)
}

func evaluate(s *Snapshot, r []EffectiveRule) []Finding { return Evaluate(s, r).Findings }

func kinds(fs []Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func TestNoData(t *testing.T) {
	inv := inverter(5)
	s := snapshot(summerNoon, inv)
	s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-2 * time.Hour)}
	fs := evaluate(s, rules(t))
	require.Equal(t, []string{KindNoData}, kinds(fs))
	assert.Equal(t, 120, fs[0].Details["minutes"])

	t.Run("recent data", func(t *testing.T) {
		s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-10 * time.Minute)}
		ev := Evaluate(s, rules(t))
		assert.Empty(t, ev.Findings)
		assert.False(t, ev.Held[KindNoData], "data is back: an open alert resolves")
	})
	t.Run("night", func(t *testing.T) {
		s := snapshot(summerNight, inv)
		s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNight.Add(-5 * time.Hour)}
		ev := Evaluate(s, rules(t))
		assert.Empty(t, ev.Findings)
		assert.True(t, ev.Held[KindNoData], "night keeps an open alert")
	})
	t.Run("just after sunrise", func(t *testing.T) {
		// 05:30 local: the sun is up, but not for the whole window yet.
		now := time.Date(2026, 6, 15, 3, 30, 0, 0, time.UTC)
		s := snapshot(now, inv)
		s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: now.Add(-8 * time.Hour)}
		ev := Evaluate(s, rules(t))
		assert.Empty(t, ev.Findings)
		assert.True(t, ev.Held[KindNoData])
	})
	t.Run("never received data", func(t *testing.T) {
		assert.Empty(t, evaluate(snapshot(summerNoon, inv), rules(t)))
	})
	t.Run("disabled", func(t *testing.T) {
		s := snapshot(summerNoon, inv)
		s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-2 * time.Hour)}
		assert.Empty(t, evaluate(s, rules(t, AlertRule{Kind: KindNoData, Enabled: false})))
	})
	t.Run("without coordinates uses day hours", func(t *testing.T) {
		s := snapshot(summerNoon, inv)
		s.System.Latitude, s.System.Longitude = nil, nil
		s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-2 * time.Hour)}
		assert.Equal(t, []string{KindNoData}, kinds(evaluate(s, rules(t))))
		s.Now = summerNight
		assert.Empty(t, evaluate(s, rules(t)))
	})
}

func TestFailingDataSourceSuppressesNoData(t *testing.T) {
	inv := inverter(5)
	s := snapshot(summerNoon, inv)
	s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-3 * time.Hour)}
	last := summerNoon.Add(-3 * time.Hour)
	s.DataSources = []datasources.DataSource{{ID: uuid.New(), Kind: "solarlog_remote", Name: "FTP", Enabled: true, LastError: "dial tcp: timeout", LastSuccessAt: &last}}
	ev := Evaluate(s, rules(t))
	require.Equal(t, []string{KindDataSourceFailing}, kinds(ev.Findings))
	assert.Equal(t, "FTP", ev.Findings[0].Details["name"])
	assert.Equal(t, "solarlog_remote", ev.Findings[0].Details["source_kind"])
	assert.True(t, ev.Held[KindNoData], "an open no_data alert stays open")
}

func TestDataSourceFailingNeedsDuration(t *testing.T) {
	last := summerNoon.Add(-30 * time.Minute)
	s := snapshot(summerNoon)
	s.DataSources = []datasources.DataSource{
		{ID: uuid.New(), Enabled: true, LastError: "x", LastSuccessAt: &last},
		{ID: uuid.New(), Enabled: false, LastError: "x", CreatedAt: summerNoon.Add(-48 * time.Hour)},
	}
	assert.Empty(t, evaluate(s, rules(t)))
	// Override: fire after 10 minutes.
	fs := evaluate(s, rules(t, AlertRule{Kind: KindDataSourceFailing, Enabled: true, Params: map[string]float64{"hours": 0.1}}))
	assert.Equal(t, []string{KindDataSourceFailing}, kinds(fs))
}

func TestInverterOffline(t *testing.T) {
	a, b := inverter(5), inverter(5)
	s := snapshot(summerNoon, a, b)
	s.States[a.ID] = telemetry.InverterState{InverterID: a.ID, Time: summerNoon.Add(-time.Minute)}
	s.States[b.ID] = telemetry.InverterState{InverterID: b.ID, Time: summerNoon.Add(-90 * time.Minute)}
	fs := evaluate(s, rules(t))
	require.Equal(t, []string{KindInverterOffline}, kinds(fs))
	assert.Equal(t, b.ID, *fs[0].InverterID)
	assert.Equal(t, b.ID.String(), fs[0].SubjectKey)

	t.Run("night", func(t *testing.T) {
		s := snapshot(summerNight, a, b)
		s.States[a.ID] = telemetry.InverterState{InverterID: a.ID, Time: summerNight.Add(-time.Minute)}
		s.States[b.ID] = telemetry.InverterState{InverterID: b.ID, Time: summerNight.Add(-5 * time.Hour)}
		ev := Evaluate(s, rules(t))
		assert.Empty(t, ev.Findings)
		assert.True(t, ev.Held[KindInverterOffline])
	})
	t.Run("whole system silent", func(t *testing.T) {
		s := snapshot(summerNoon, a, b)
		s.States[a.ID] = telemetry.InverterState{InverterID: a.ID, Time: summerNoon.Add(-3 * time.Hour)}
		s.States[b.ID] = telemetry.InverterState{InverterID: b.ID, Time: summerNoon.Add(-3 * time.Hour)}
		ev := Evaluate(s, rules(t))
		assert.Equal(t, []string{KindNoData}, kinds(ev.Findings))
		assert.True(t, ev.Held[KindInverterOffline], "b isn't back, keep its alert")
	})
	t.Run("a missed upload keeps the system online", func(t *testing.T) {
		// Solar-Log uploads every 10 minutes: a 25-minute gap is no silence.
		s := snapshot(summerNoon, a, b)
		s.States[a.ID] = telemetry.InverterState{InverterID: a.ID, Time: summerNoon.Add(-25 * time.Minute)}
		s.States[b.ID] = telemetry.InverterState{InverterID: b.ID, Time: summerNoon.Add(-90 * time.Minute)}
		assert.Equal(t, []string{KindInverterOffline}, kinds(Evaluate(s, rules(t)).Findings))
	})
	t.Run("back online", func(t *testing.T) {
		s.States[b.ID] = telemetry.InverterState{InverterID: b.ID, Time: summerNoon.Add(-time.Minute)}
		ev := Evaluate(s, rules(t))
		assert.Empty(t, ev.Findings)
		assert.False(t, ev.Held[KindInverterOffline])
	})
}

func TestInverterErrorDebounce(t *testing.T) {
	inv := inverter(5)
	code := 7
	s := snapshot(summerNoon, inv)
	s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon, ErrorCode: &code, ErrorText: "Isolation fault"}
	s.OpenEvents[inv.ID] = telemetry.InverterEvent{StartedAt: summerNoon.Add(-5 * time.Minute)}
	assert.Empty(t, evaluate(s, rules(t)), "transient errors don't alert")

	s.OpenEvents[inv.ID] = telemetry.InverterEvent{StartedAt: summerNoon.Add(-20 * time.Minute)}
	fs := evaluate(s, rules(t))
	require.Equal(t, []string{KindInverterError}, kinds(fs))
	assert.Equal(t, 7, fs[0].Details["code"])

	zero := 0
	s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon, ErrorCode: &zero}
	assert.Empty(t, evaluate(s, rules(t)))
}

func TestUnderperformance(t *testing.T) {
	a, b, c := inverter(5), inverter(5), inverter(10)
	s := snapshot(summerNoon, a, b, c)
	s.YesterdayDate = time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	// kWh/kWp: a 4.0, b 2.0 (50 %), c 4.0
	s.Yesterday = map[uuid.UUID]float64{a.ID: 20000, b.ID: 10000, c.ID: 40000}
	s.DayBefore = map[uuid.UUID]float64{a.ID: 20000, b.ID: 12000, c.ID: 40000}
	fs := evaluate(s, rules(t))
	require.Equal(t, []string{KindUnderperformance}, kinds(fs))
	assert.Equal(t, b.ID, *fs[0].InverterID)
	assert.Equal(t, 50, fs[0].Details["percent"])

	t.Run("held at night", func(t *testing.T) {
		night := *s
		night.Now = summerNight
		ev := Evaluate(&night, rules(t))
		assert.Empty(t, ev.Findings)
		assert.True(t, ev.Held[KindUnderperformance])
		assert.True(t, ev.Held[KindLowYield])
	})
	t.Run("only one bad day", func(t *testing.T) {
		s.DayBefore[b.ID] = 20000
		assert.Empty(t, evaluate(s, rules(t)))
	})
	t.Run("dark day", func(t *testing.T) {
		s.DayBefore[b.ID] = 1000
		s.Yesterday = map[uuid.UUID]float64{a.ID: 1000, b.ID: 100, c.ID: 2000}
		assert.Empty(t, evaluate(s, rules(t)))
	})
	t.Run("single inverter", func(t *testing.T) {
		s := snapshot(summerNoon, a)
		s.Yesterday = map[uuid.UUID]float64{a.ID: 100}
		s.DayBefore = map[uuid.UUID]float64{a.ID: 100}
		assert.Empty(t, evaluate(s, rules(t)))
	})
}

func TestUnderperformancePerPlane(t *testing.T) {
	east, west := systems.Plane{ID: uuid.New(), Name: "Ost"}, systems.Plane{ID: uuid.New(), Name: "West"}
	on := func(pl systems.Plane, kwp float64) systems.Inverter {
		inv := inverter(kwp)
		inv.PlaneID = &pl.ID
		return inv
	}
	day := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	setup := func(invs ...systems.Inverter) *Snapshot {
		s := snapshot(summerNoon, invs...)
		s.System.Planes = []systems.Plane{east, west}
		s.YesterdayDate = day
		return s
	}

	t.Run("east/west without faults", func(t *testing.T) {
		e1, e2, w1, w2 := on(east, 5), on(east, 5), on(west, 5), on(west, 5)
		s := setup(e1, e2, w1, w2)
		// West yields only half of east, but both west inverters alike.
		s.Yesterday = map[uuid.UUID]float64{e1.ID: 20000, e2.ID: 20000, w1.ID: 10000, w2.ID: 10000}
		s.DayBefore = maps.Clone(s.Yesterday)
		assert.Empty(t, evaluate(s, rules(t)))
	})
	t.Run("weak inverter within a plane", func(t *testing.T) {
		e1, e2, e3, w1, w2 := on(east, 5), on(east, 5), on(east, 5), on(west, 5), on(west, 5)
		s := setup(e1, e2, e3, w1, w2)
		s.Yesterday = map[uuid.UUID]float64{e1.ID: 20000, e2.ID: 20000, e3.ID: 12000, w1.ID: 10000, w2.ID: 10000}
		s.DayBefore = maps.Clone(s.Yesterday)
		fs := evaluate(s, rules(t))
		require.Equal(t, []string{KindUnderperformance}, kinds(fs))
		assert.Equal(t, e3.ID, *fs[0].InverterID)
		assert.Equal(t, 60, fs[0].Details["percent"])
		assert.Equal(t, "Ost", fs[0].Details["plane"])
		m := RenderAlert("de", KindUnderperformance, "WR 3", fs[0].Details, time.UTC)
		assert.Contains(t, m.Body, "60 %")
		assert.Contains(t, m.Body, "auf „Ost“")
		assert.Contains(t, RenderAlert("en", KindUnderperformance, "WR 3", fs[0].Details, time.UTC).Body, "on “Ost”")
	})
	t.Run("alone on its plane", func(t *testing.T) {
		e1, e2, w1 := on(east, 5), on(east, 5), on(west, 5)
		s := setup(e1, e2, w1)
		s.Yesterday = map[uuid.UUID]float64{e1.ID: 20000, e2.ID: 20000, w1.ID: 2000}
		s.DayBefore = maps.Clone(s.Yesterday)
		assert.Empty(t, evaluate(s, rules(t)))
	})
	t.Run("mixed with and without plane", func(t *testing.T) {
		e1, e2 := on(east, 5), on(east, 5)
		n1, n2, n3 := inverter(5), inverter(5), inverter(5)
		s := setup(e1, e2, n1, n2, n3)
		// The planeless group is compared on its own; east is fine.
		s.Yesterday = map[uuid.UUID]float64{e1.ID: 10000, e2.ID: 10000, n1.ID: 20000, n2.ID: 20000, n3.ID: 8000}
		s.DayBefore = maps.Clone(s.Yesterday)
		fs := evaluate(s, rules(t))
		require.Equal(t, []string{KindUnderperformance}, kinds(fs))
		assert.Equal(t, n3.ID, *fs[0].InverterID)
		assert.NotContains(t, fs[0].Details, "plane")
		assert.NotContains(t, RenderAlert("en", KindUnderperformance, "WR", fs[0].Details, time.UTC).Body, "“")
	})
}

func TestImplausibleData(t *testing.T) {
	a, b := inverter(5), inverter(5)
	s := snapshot(summerNoon, a, b)
	s.YesterdayDate = time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	s.Rejections = []telemetry.IngestRejection{
		{InverterID: a.ID, Reason: "negative_power", Count: 6},
		{InverterID: a.ID, Reason: "future", Count: 4},
		{InverterID: b.ID, Reason: "future", Count: 9},
	}
	fs := evaluate(s, rules(t))
	require.Equal(t, []string{KindImplausibleData}, kinds(fs))
	assert.Equal(t, a.ID, *fs[0].InverterID)
	assert.Equal(t, 10, fs[0].Details["count"])
	assert.Equal(t, []string{"future", "negative_power"}, fs[0].Details["reasons"])
	m := RenderAlert("de", KindImplausibleData, "WR 1", fs[0].Details, time.UTC)
	assert.Contains(t, m.Body, "10 Werte")
	assert.Contains(t, m.Body, "Zeit in der Zukunft, negative Leistung")
	// after the JSON round trip through the notification payload
	m = RenderAlert("en", KindImplausibleData, "WR 1", map[string]any{"count": 10.0, "reasons": []any{"power_above_limit"}, "day": "2026-06-14"}, time.UTC)
	assert.Contains(t, m.Body, "power above the rated power")

	fs = evaluate(s, rules(t, AlertRule{Kind: KindImplausibleData, Enabled: true, Params: map[string]float64{"min_count": 9}}))
	assert.Len(t, fs, 2)
}

func TestZeroDay(t *testing.T) {
	inv := inverter(5)
	s := snapshot(summerNoon, inv)
	s.YesterdayDate = time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	s.States[inv.ID] = telemetry.InverterState{InverterID: inv.ID, Time: summerNoon.Add(-time.Minute)}
	first := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s.FirstDay = &first
	assert.Equal(t, []string{KindZeroDay}, kinds(evaluate(s, rules(t))))

	s.Yesterday[inv.ID] = 1500
	assert.Empty(t, evaluate(s, rules(t)))

	t.Run("new system", func(t *testing.T) {
		s.Yesterday = map[uuid.UUID]float64{}
		today := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		s.FirstDay = &today
		assert.Empty(t, evaluate(s, rules(t)))
	})
}

func TestValidateParams(t *testing.T) {
	assert.NoError(t, ValidateParams(KindNoData, map[string]float64{"minutes": 30}))
	assert.Error(t, ValidateParams("nope", nil))
	assert.Error(t, ValidateParams(KindNoData, map[string]float64{"hours": 1}))
	assert.Error(t, ValidateParams(KindUnderperformance, map[string]float64{"percent": 150}))
	assert.Error(t, ValidateParams(KindNoData, map[string]float64{"minutes": -1}))
}

func TestRenderNotification(t *testing.T) {
	sys := uuid.New()
	n := notifications.Notification{
		Type: notifications.NotificationAlertOpened, SystemID: &sys, Severity: notifications.SeverityError,
		Payload: map[string]any{
			"kind": KindInverterError, "system_name": "Home", "subject": "WR 2", "timezone": "Europe/Berlin",
			"details": map[string]any{"code": float64(7), "text": "Isolation fault", "since": "2026-06-15T08:42:00Z"},
		},
	}
	m := RenderNotification("en", n)
	assert.Equal(t, "WR 2 reports an error", m.Title)
	assert.Equal(t, "Home · Isolation fault since Jun 15, 2026 10:42.", m.Body)

	m = RenderNotification("de-DE", n)
	assert.Equal(t, "WR 2 meldet einen Fehler", m.Title)
	assert.Equal(t, "Home · Isolation fault seit 15.06.2026 10:42.", m.Body)

	n.Type = notifications.NotificationAlertResolved
	m = RenderNotification("en", n)
	assert.Equal(t, "Resolved: WR 2 reports an error", m.Title)
	assert.Equal(t, "Home · The problem is no longer detected.", m.Body)

	n.Payload["kind"] = KindNoData
	n.Payload["subject"] = "Home"
	n.Payload["details"] = map[string]any{"minutes": float64(185), "last_seen": "2026-06-15T07:00:00Z"}
	n.Type = notifications.NotificationAlertOpened
	m = RenderNotification("en", n)
	assert.Equal(t, "No data from Home", m.Title)
	assert.Equal(t, "No data has arrived for 3 hours (last data Jun 15, 2026 09:00).", m.Body)
}

func TestStale(t *testing.T) {
	sys := &systems.System{Timezone: "Europe/Berlin", Latitude: &lat, Longitude: &lon}
	at := func(d time.Duration) *time.Time { v := summerNoon.Add(-d); return &v }
	src := func(every time.Duration, enabled bool) datasources.DataSource {
		return datasources.DataSource{Enabled: enabled, PollInterval: every}
	}

	assert.Equal(t, 30*time.Minute, StaleAfter(nil))
	assert.Equal(t, 30*time.Minute, OnlineWindow(nil))
	assert.Equal(t, 30*time.Minute, StaleAfter([]datasources.DataSource{src(5*time.Minute, true)}))
	assert.Equal(t, 40*time.Minute, StaleAfter([]datasources.DataSource{src(time.Hour, true), src(20*time.Minute, true), src(time.Minute, false)}))

	assert.False(t, Stale(sys, nil, nil, nil, summerNoon), "no data yet")
	assert.False(t, Stale(sys, nil, nil, at(29*time.Minute), summerNoon))
	assert.True(t, Stale(sys, nil, nil, at(31*time.Minute), summerNoon))
	assert.False(t, Stale(sys, nil, []datasources.DataSource{src(30*time.Minute, true)}, at(31*time.Minute), summerNoon), "slow poller")

	night := time.Date(2026, 6, 15, 23, 0, 0, 0, time.UTC)
	last := night.Add(-5 * time.Hour)
	assert.False(t, Stale(sys, nil, nil, &last, night), "at night")

	// the no_data rule's elevation decides when the sun is up
	dusk := time.Date(2026, 6, 15, 18, 0, 0, 0, time.UTC) // about 13° in Germany
	last = dusk.Add(-time.Hour)
	assert.True(t, Stale(sys, nil, nil, &last, dusk))
	high := []AlertRule{{Kind: KindNoData, Enabled: true, Params: map[string]float64{"min_sun_elevation": 30}}}
	assert.False(t, Stale(sys, high, nil, &last, dusk))
}
