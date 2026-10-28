//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/reports"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedHistory creates a system with two inverters and daily energy on
// 2024-09-10, 2025-09-10 and 2026-09-10..11.
func seedHistory(t *testing.T, e *env, name string) (*client, string) {
	t.Helper()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": name, "peak_power_kwp": 10, "tariff_per_kwh": 0.1}, 201, &sys)
	sid := sys["id"].(string)
	days := []obj{
		{"inverter": "1", "date": "2024-09-10", "energy_wh": 10000},
		{"inverter": "1", "date": "2025-09-10", "energy_wh": 20000},
		{"inverter": "2", "date": "2025-09-10", "energy_wh": 5000},
		{"inverter": "1", "date": "2026-09-10", "energy_wh": 30000},
		{"inverter": "1", "date": "2026-09-11", "energy_wh": 1000},
		{"inverter": "2", "date": "2026-09-11", "energy_wh": 2000},
	}
	owner.do("POST", "/systems/"+sid+"/ingest/daily", obj{"days": days, "create_inverters": true}, 200, nil)
	return owner, sid
}

func TestYearOverYearAndDailyEnergy(t *testing.T) {
	e := newEnv(t)
	owner, sid := seedHistory(t, e, "History PV")

	var ov obj
	owner.do("GET", "/systems/"+sid+"/overview?period=month&date=2026-09-01", nil, 200, &ov)
	assert.Nil(t, ov["comparison"], "only with compare")

	owner.do("GET", "/systems/"+sid+"/overview?period=month&date=2026-09-01&compare=previous_year", nil, 200, &ov)
	cmp := ov["comparison"].([]any)
	require.Len(t, cmp, 1)
	prev := cmp[0].(obj)
	assert.EqualValues(t, 2025, prev["year"])
	assert.InDelta(t, 25.0, prev["total_kwh"], 1e-9)
	vals := prev["values"].([]any)
	require.Len(t, vals, 30)
	assert.InDelta(t, 25.0, vals[9], 1e-9)
	assert.InDelta(t, 0.0, vals[10], 1e-9)

	owner.do("GET", "/systems/"+sid+"/overview?period=month&date=2026-09-01&compare=all_years", nil, 200, &ov)
	cmp = ov["comparison"].([]any)
	require.Len(t, cmp, 2)
	older := cmp[1].(obj)
	assert.EqualValues(t, 2024, older["year"])
	assert.Nil(t, older["values"].([]any)[8], "before the first data")
	assert.InDelta(t, 10.0, older["values"].([]any)[9], 1e-9)

	// Year view, one inverter.
	var invs []obj
	owner.do("GET", "/systems/"+sid+"/inverters", nil, 200, &invs)
	var inv1 string
	for _, inv := range invs {
		if inv["external_ref"] == "1" {
			inv1 = inv["id"].(string)
		}
	}
	owner.do("GET", "/systems/"+sid+"/overview?period=year&date=2026-01-01&compare=previous_year&inverters="+inv1, nil, 200, &ov)
	prev = ov["comparison"].([]any)[0].(obj)
	assert.InDelta(t, 20.0, prev["values"].([]any)[8], 1e-9)
	assert.InDelta(t, 0.0, prev["values"].([]any)[7], 1e-9, "after the first data: zero, not null")

	// The first data year has nothing to compare with.
	owner.do("GET", "/systems/"+sid+"/overview?period=year&date=2024-01-01&compare=previous_year", nil, 200, &ov)
	assert.Empty(t, ov["comparison"])

	var daily obj
	owner.do("GET", "/systems/"+sid+"/daily-energy?from=2025-01-01&to=2027-01-01", nil, 200, &daily)
	days := daily["days"].([]any)
	require.Len(t, days, 3)
	assert.Equal(t, obj{"date": "2025-09-10", "energy_kwh": 25.0}, days[0])
	assert.Equal(t, obj{"date": "2026-09-11", "energy_kwh": 3.0}, days[2])
	owner.do("GET", "/systems/"+sid+"/daily-energy?from=2026-01-01&to=2025-01-01", nil, 400, nil)
}

func TestTemperatureAndPowerExport(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Temp PV", "peak_power_kwp": 5}, 201, &sys)
	sid := sys["id"].(string)
	day := time.Date(2026, 9, 2, 0, 0, 0, 0, mustLoc(t))
	var ms []obj
	for i := range 10 { // 12:00..12:09, one reading every minute
		m := obj{"inverter": "1", "time": day.Add(12*time.Hour + time.Duration(i)*time.Minute).Format(time.RFC3339), "pac_w": 1000, "pdc_w": 1050, "udc_v": 400 + i}
		if i < 5 {
			m["temperature_c"] = 40
		}
		ms = append(ms, m)
	}
	owner.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": ms, "create_inverters": true}, 200, nil)

	var ov obj
	owner.do("GET", "/systems/"+sid+"/overview?period=day&date=2026-09-02", nil, 200, &ov)
	s := ov["series"].([]any)[0].(obj)
	assert.Equal(t, []any{402.0, 407.0}, s["udc_v"]) // means of 12:00-12:04 and 12:05-12:09
	assert.Equal(t, []any{40.0, nil}, s["temperature_c"])

	status, raw := owner.raw("GET", "/systems/"+sid+"/export?granularity=minute&from=2026-09-02&to=2026-09-03", nil)
	require.Equal(t, 200, status, string(raw))
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 11)
	assert.Equal(t, "time,inverter_id,inverter,pac_w,pdc_w,udc_v,temperature_c", lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "2026-09-02T12:00:00+02:00,"), lines[1])
	assert.True(t, strings.HasSuffix(lines[1], ",1000,1050,400,40"), lines[1])
	assert.True(t, strings.HasSuffix(lines[10], ",1000,1050,409,"), lines[10])

	status, raw = owner.raw("GET", "/systems/"+sid+"/export?granularity=5min&from=2026-09-02&to=2026-09-03", nil)
	require.Equal(t, 200, status, string(raw))
	lines = strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 3)
	assert.True(t, strings.HasSuffix(lines[2], ",1000,1050,407,"), lines[2])

	owner.do("GET", "/systems/"+sid+"/export?granularity=minute&from=2026-08-01&to=2026-09-03", nil, 400, nil) // > 31 days
	owner.do("GET", "/systems/"+sid+"/export?granularity=day&from=2026-08-01&to=2026-09-03", nil, 200, nil)
}

func TestShareLinks(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	owner, sid := seedHistory(t, e, "Shared PV")
	sysID := uuid.MustParse(sid)

	viewerEmail := uniq("viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	require.NoError(t, e.Systems.UpsertMember(ctx, sysID, viewerID, auth.RoleViewer))
	viewer := e.login(viewerEmail, "viewer-password-1")
	strangerEmail := uniq("stranger")
	e.createUser(strangerEmail, "stranger-password-1", false)
	stranger := e.login(strangerEmail, "stranger-password-1")

	base := "/systems/" + sid + "/share-links"
	viewer.do("GET", base, nil, 403, nil)
	viewer.do("POST", base, obj{"label": "x"}, 403, nil)
	stranger.do("GET", base, nil, 404, nil)
	e.anon().do("GET", base, nil, 401, nil)
	owner.do("POST", base, obj{"label": " "}, 400, nil)
	owner.do("POST", base, obj{"label": "x", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}, 400, nil)

	var created obj
	owner.do("POST", base, obj{"label": "Hallway screen"}, 201, &created)
	token := created["token"].(string)
	assert.True(t, strings.HasSuffix(created["url"].(string), "/p/"+token))
	link := created["share_link"].(obj)
	assert.Equal(t, false, link["show_financials"])
	assert.True(t, strings.HasPrefix(token, link["prefix"].(string)))
	assert.NotEmpty(t, link["created_by"])

	var list []obj
	owner.do("GET", base, nil, 200, &list)
	require.Len(t, list, 1)
	assert.NotContains(t, list[0], "token")

	pub := e.anon()
	var ps obj
	pub.do("GET", "/public/"+token, nil, 200, &ps)
	assert.Equal(t, "Shared PV", ps["name"])
	assert.Equal(t, "Hallway screen", ps["label"])
	assert.Nil(t, ps["tariff_per_kwh"])
	assert.Len(t, ps["inverters"], 2)
	for _, k := range []string{"location", "latitude", "role", "operator"} {
		assert.NotContains(t, ps, k)
	}

	var ov obj
	pub.do("GET", "/public/"+token+"/overview?period=month&date=2026-09-01&compare=previous_year", nil, 200, &ov)
	sum := ov["summary"].(obj)
	assert.InDelta(t, 33.0, sum["energy_kwh"], 1e-9)
	assert.EqualValues(t, 0, sum["revenue"], "no financials")
	assert.Len(t, ov["comparison"], 1)
	for _, inv := range ov["inverters"].([]any) {
		assert.Empty(t, inv.(obj)["status_text"])
		assert.NotContains(t, inv.(obj), "error_text")
	}
	var live obj
	pub.do("GET", "/public/"+token+"/live", nil, 200, &live)
	pub.do("GET", "/public/s0p_unknown", nil, 404, nil)
	pub.do("GET", "/public/garbage/live", nil, 404, nil)
	// The share token is no API credential.
	(&client{e: e, http: e.anon().http, token: token}).do("GET", "/systems/"+sid, nil, 401, nil)

	var fin obj
	owner.do("POST", base, obj{"label": "Investors", "show_financials": true}, 201, &fin)
	pub.do("GET", "/public/"+fin["token"].(string)+"/overview?period=month&date=2026-09-01", nil, 200, &ov)
	assert.InDelta(t, 3.3, ov["summary"].(obj)["revenue"], 1e-9)
	pub.do("GET", "/public/"+fin["token"].(string), nil, 200, &ps)
	assert.InDelta(t, 0.1, ps["tariff_per_kwh"], 1e-9)

	// Revoked and expired links are gone.
	viewer.do("DELETE", base+"/"+link["id"].(string), nil, 403, nil)
	owner.do("DELETE", base+"/"+link["id"].(string), nil, 204, nil)
	owner.do("DELETE", base+"/"+link["id"].(string), nil, 404, nil)
	pub.do("GET", "/public/"+token, nil, 404, nil)
	pub.do("GET", "/public/"+token+"/overview?period=day", nil, 404, nil)

	var soon obj
	owner.do("POST", base, obj{"label": "Soon", "expires_at": time.Now().Add(2 * time.Second).Format(time.RFC3339Nano)}, 201, &soon)
	pub.do("GET", "/public/"+soon["token"].(string), nil, 200, nil)
	time.Sleep(2500 * time.Millisecond)
	pub.do("GET", "/public/"+soon["token"].(string), nil, 404, nil)
	owner.do("GET", base, nil, 200, &list)
	require.Len(t, list, 1)
	assert.Equal(t, "Investors", list[0]["label"])
}

func TestDashboardFleetFields(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	owner, sid := seedHistory(t, e, "Fleet PV")
	sysID := uuid.MustParse(sid)
	_, err := e.Alerting.OpenAlert(ctx, &alerting.Alert{SystemID: sysID, Kind: alerting.KindNoData, Severity: notifications.SeverityWarning, OpenedAt: time.Now()})
	require.NoError(t, err)

	var dash obj
	owner.do("GET", "/dashboard", nil, 200, &dash)
	var row obj
	for _, s := range dash["systems"].([]any) {
		if s.(obj)["system_id"] == sid {
			row = s.(obj)
		}
	}
	require.NotNil(t, row)
	assert.EqualValues(t, 1, row["open_alerts"])
}

func TestYieldReports(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	owner, sid := seedHistory(t, e, "Report PV")
	sysID := uuid.MustParse(sid)
	var me obj
	owner.do("GET", "/me", nil, 200, &me)
	uid := uuid.MustParse(me["id"].(string))
	owner.do("PUT", "/systems/"+sid+"/targets", obj{"target_kwh": []float64{0, 0, 0, 0, 0, 0, 0, 0, 300, 0, 0, 0}}, 200, nil)
	_, err := e.Alerting.OpenAlert(ctx, &alerting.Alert{SystemID: sysID, Kind: alerting.KindNoData, Severity: notifications.SeverityWarning,
		OpenedAt: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC), Details: map[string]any{"minutes": 120}})
	require.NoError(t, err)

	sys, err := e.Systems.GetSystem(ctx, sysID)
	require.NoError(t, err)
	loc := mustLoc(t)
	svc := e.reportService()
	oct1 := time.Date(2026, 10, 1, 7, 0, 0, 0, loc)
	svc.SetClock(func() time.Time { return oct1 })
	reportsOf := func() []notifications.Notification {
		ns, err := e.Notifications.ListNotifications(ctx, uid, notifications.NotificationFilter{Limit: 50})
		require.NoError(t, err)
		var out []notifications.Notification
		for _, n := range ns {
			if n.Type == notifications.NotificationReport && *n.SystemID == sysID {
				out = append(out, n)
			}
		}
		return out
	}

	// No digest chosen: nothing.
	require.NoError(t, svc.RunSystem(ctx, *sys))
	assert.Empty(t, reportsOf())

	var prefs obj
	owner.do("GET", "/me/notification-preferences", nil, 200, &prefs)
	def := prefs["default"].(map[string]any)
	// Besides the email: unread in the app, push and webhook.
	def["reports"] = obj{"channels": []string{"email", "in_app", "push", "webhook"}, "digest": "monthly"}
	owner.do("PUT", "/me/notification-preferences", obj{"default": def}, 200, nil)
	require.NoError(t, e.Notifications.SetNotificationTargets(ctx, &notifications.NotificationTargets{UserID: uid, WebhookURL: "https://hook.test/noonside"}))

	// Outside the morning window: nothing.
	svc.SetClock(func() time.Time { return oct1.Add(-3 * time.Hour) })
	require.NoError(t, svc.RunSystem(ctx, *sys))
	assert.Empty(t, reportsOf())

	svc.SetClock(func() time.Time { return oct1 })
	require.NoError(t, svc.RunSystem(ctx, *sys))
	ns := reportsOf()
	require.Len(t, ns, 1)
	assert.Nil(t, ns[0].ReadAt, "counted as unread (in_app)")
	var channels []string
	require.NoError(t, e.db.Pool().QueryRow(ctx, `SELECT array_agg(channel ORDER BY channel) FROM notification_deliveries WHERE notification_id = $1`, ns[0].ID).Scan(&channels))
	assert.Equal(t, []string{"email", "push", "webhook"}, channels)
	assert.Equal(t, "Report PV: Monthly report September 2026", notifications.RenderNotification("en", ns[0]).Title, "push and webhook render the summary")
	rep, err := reports.FromPayload(ns[0].Payload)
	require.NoError(t, err)
	assert.Equal(t, reports.KindMonthly, rep.Kind)
	assert.Equal(t, "2026-09-01", rep.From)
	assert.InDelta(t, 33.0, rep.EnergyKWh, 1e-9)
	require.NotNil(t, rep.ExpectedKWh)
	assert.InDelta(t, 300.0, *rep.ExpectedKWh, 1e-9)
	assert.Equal(t, "targets", rep.ExpectedSource)
	assert.Equal(t, "2026-09-10", rep.BestDay)
	assert.InDelta(t, 3.3, rep.Revenue, 1e-9)
	require.Len(t, rep.Inverters, 2)
	assert.InDelta(t, 31.0/33*100, rep.Inverters[0].SharePct, 0.1)
	require.Len(t, rep.Alerts, 1)
	assert.Equal(t, "/s/"+sid+"/overview?period=month&date=2026-09-01", notifications.Link(ns[0]))

	var page obj
	owner.do("GET", "/me/notifications", nil, 200, &page)
	found := false
	for _, it := range page["items"].([]any) {
		if it.(obj)["type"] == "report" && it.(obj)["system_id"] == sid {
			found = true
			assert.Equal(t, "Report PV: Monthly report September 2026", it.(obj)["title"])
		}
	}
	assert.True(t, found)

	// Once per period.
	svc.SetClock(func() time.Time { return oct1.Add(2 * time.Hour) })
	require.NoError(t, svc.RunSystem(ctx, *sys))
	assert.Len(t, reportsOf(), 1)

	// The dispatcher sends it as its own email.
	d := notifications.NewDispatcher(e.Notifications, e.mailer, nil, "https://app.test").WithReports(reports.RenderEmail("https://app.test"))
	require.NoError(t, d.Run(ctx, time.Now().Add(10*time.Second)))
	msg := e.outbox.find("Monthly report September 2026")
	require.NotEmpty(t, msg)
	assert.Contains(t, msg, "Report PV produced 33.0 kWh in September 2026. That is 11 % of the target.")
	assert.Contains(t, msg, "https://app.test/s/"+sid+"/overview?period")
}
