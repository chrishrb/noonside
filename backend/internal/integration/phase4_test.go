//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/datasources/pvoutput"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/webhooks"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendWebhooks sends the queued webhook deliveries now.
func sendWebhooks(t *testing.T, e *env) {
	t.Helper()
	require.NoError(t, webhooks.NewSender(e.WebhookStore(), e.notify, e.box, "http://app.test").Run(context.Background(), time.Now().Add(time.Minute)))
}

func hitsFor(hits []hit, path string) []hit {
	var out []hit
	for _, h := range hits {
		if h.path == path {
			out = append(out, h)
		}
	}
	return out
}

func TestWebhooks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ownerEmail, viewerEmail, strangerEmail := uniq("owner"), uniq("viewer"), uniq("stranger")
	e.createUser(ownerEmail, "owner-password-1", true)
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	e.createUser(strangerEmail, "stranger-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	viewer := e.login(viewerEmail, "viewer-password-1")
	stranger := e.login(strangerEmail, "stranger-password-1")
	srv, hits := targetServer(t)

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Webhook PV", "peak_power_kwp": 10, "tariff_per_kwh": 0.1}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))
	base := "/systems/" + sid.String()
	require.NoError(t, e.Systems.UpsertMember(ctx, sid, viewerID, auth.RoleViewer))

	// authorization and validation
	viewer.do("GET", base+"/webhooks", nil, 403, nil)
	stranger.do("GET", base+"/webhooks", nil, 404, nil)
	hook := obj{"name": "Home Assistant", "url": srv.URL + "/ha", "secret": "whsec", "events": []string{"alert.opened", "alert.resolved", "daily.summary"}}
	viewer.do("POST", base+"/webhooks", hook, 403, nil)
	owner.do("POST", base+"/webhooks", obj{"name": "x", "url": "ftp://example.com/x", "events": []string{"alert.opened"}}, 400, nil)
	owner.do("POST", base+"/webhooks", obj{"name": "x", "url": srv.URL, "events": []string{}}, 400, nil)
	owner.do("POST", base+"/webhooks", obj{"name": "x", "url": srv.URL, "events": []string{"nope"}}, 400, nil)

	var w obj
	owner.do("POST", base+"/webhooks", hook, 201, &w)
	assert.Equal(t, true, w["has_secret"])
	assert.Equal(t, true, w["enabled"])
	assert.Equal(t, "en", w["locale"])
	assert.NotContains(t, w, "secret")
	wid := w["id"].(string)
	var secretEnc string
	require.NoError(t, e.db.Pool().QueryRow(ctx, `SELECT secret_enc FROM webhooks WHERE id = $1`, wid).Scan(&secretEnc))
	assert.NotEqual(t, "whsec", secretEnc, "stored sealed")

	// another system's webhook is not reachable through this one
	var other obj
	owner.do("POST", "/systems", obj{"name": "Other"}, 201, &other)
	owner.do("GET", "/systems/"+other["id"].(string)+"/webhooks/"+wid+"/deliveries", nil, 404, nil)
	owner.do("PATCH", "/systems/"+other["id"].(string)+"/webhooks/"+wid, obj{"name": "y"}, 404, nil)

	// test button: signed now, not queued
	var res obj
	owner.do("POST", base+"/webhooks/"+wid+"/test", nil, 200, &res)
	assert.Equal(t, true, res["ok"])
	got := hitsFor(hits(), "/ha")
	require.Len(t, got, 1)
	assert.Equal(t, "test", got[0].header.Get(webhooks.EventHeader))
	assert.Equal(t, notify.Sign("whsec", got[0].body), got[0].header.Get(notify.SignatureHeader))
	var deliveries []obj
	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	assert.Empty(t, deliveries)

	// an alert opens: one delivery, sent by the sender with the signature
	owner.do("PUT", base+"/alert-rules", []obj{{"kind": alerting.KindDataSourceFailing, "enabled": true, "params": obj{"hours": 0}}}, 200, nil)
	ds, err := e.DataSources.CreateDataSource(ctx, &datasources.DataSource{
		SystemID: sid, Kind: "solarlog_remote", Name: "Export", Config: []byte(`{}`), Enabled: true, PollInterval: time.Hour,
	})
	require.NoError(t, err)
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now().Add(-time.Hour), nil))
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now(), errors.New("no such directory")))
	svc := webhooks.NewService(e.WebhookStore(), e.reportService(), advice.NewService(nil, e.Prices), "http://app.test")
	engine := alerting.NewEngine(e.Alerting.Store()).WithHook(svc.Alert)
	system, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)
	require.NoError(t, engine.EvaluateSystem(ctx, *system))
	require.NoError(t, engine.EvaluateSystem(ctx, *system), "a second evaluation queues nothing")

	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	require.Len(t, deliveries, 1)
	assert.Equal(t, "alert.opened", deliveries[0]["event"])
	assert.Equal(t, "pending", deliveries[0]["status"])
	sendWebhooks(t, e)
	got = hitsFor(hits(), "/ha")
	require.Len(t, got, 2)
	h := got[1]
	assert.Equal(t, "alert.opened", h.header.Get(webhooks.EventHeader))
	assert.Equal(t, deliveries[0]["event_id"], h.header.Get(webhooks.DeliveryHeader))
	assert.Equal(t, notify.Sign("whsec", h.body), h.header.Get(notify.SignatureHeader))
	var p webhooks.Payload
	require.NoError(t, json.Unmarshal(h.body, &p))
	assert.Equal(t, "alert.opened", p.Event)
	assert.Equal(t, sid, p.SystemID)
	assert.Equal(t, "Webhook PV", p.SystemName)
	assert.Equal(t, "http://app.test/s/"+sid.String()+"/status", p.URL)
	assert.Equal(t, alerting.KindDataSourceFailing, p.Data["kind"])
	assert.Equal(t, "Data source Export is failing", p.Data["title"])
	assert.Equal(t, "Export", p.Data["subject"])

	owner.do("GET", base+"/webhooks", nil, 200, &deliveries)
	require.Len(t, deliveries, 1)
	assert.Equal(t, "sent", deliveries[0]["last_status"])

	// in German, disabled for alerts: the resolution goes nowhere
	owner.do("PATCH", base+"/webhooks/"+wid, obj{"locale": "de", "events": []string{"daily.summary"}}, 200, &w)
	assert.Equal(t, []any{"daily.summary"}, w["events"])
	assert.Equal(t, true, w["has_secret"], "kept")
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now(), nil))
	for range 2 {
		require.NoError(t, engine.EvaluateSystem(ctx, *system))
	}
	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	assert.Len(t, deliveries, 1)

	// daily summary from 06:00, once
	loc := mustLoc(t)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var key obj
	owner.do("POST", base+"/api-keys", obj{"name": "gw"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}
	yesterday := today.AddDate(0, 0, -1)
	device.do("POST", base+"/ingest/measurements", obj{"create_inverters": true, "measurements": []obj{
		{"inverter": "1", "time": yesterday.Add(12 * time.Hour).Format(time.RFC3339), "pac_w": 4000, "eday_wh": 20000},
		{"inverter": "1", "time": yesterday.Add(13 * time.Hour).Format(time.RFC3339), "pac_w": 3000, "eday_wh": 24000},
	}}, 200, nil)
	// The deadline is on the service's clock, not the real one.
	at := today.Add(5 * time.Hour)
	svc.SetClock(func() time.Time { return at })
	require.NoError(t, svc.Run(ctx, at.Add(time.Minute)))
	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	assert.Len(t, deliveries, 1, "too early")
	at = today.Add(7 * time.Hour)
	for range 2 {
		require.NoError(t, svc.Run(ctx, at.Add(time.Minute)))
	}
	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	require.Len(t, deliveries, 2)
	assert.Equal(t, "daily.summary", deliveries[0]["event"])
	sendWebhooks(t, e)
	got = hitsFor(hits(), "/ha")
	require.Len(t, got, 3)
	require.NoError(t, json.Unmarshal(got[2].body, &p))
	assert.Equal(t, "daily.summary", p.Event)
	assert.Equal(t, yesterday.Format(time.DateOnly), p.Data["date"])
	assert.Equal(t, 24.0, p.Data["energy_kwh"])
	assert.Equal(t, 2.4, p.Data["kwh_per_kwp"])
	assert.Equal(t, 2.4, p.Data["revenue"])
	assert.Equal(t, 4000.0, p.Data["peak_w"])
	assert.Equal(t, []any{map[string]any{"name": "WR 1", "energy_kwh": 24.0}}, p.Data["inverters"])

	// a receiver that fails is retried with backoff
	owner.do("PATCH", base+"/webhooks/"+wid, obj{"url": srv.URL + "/broken"}, 200, nil)
	owner.do("POST", base+"/webhooks/"+wid+"/test", nil, 200, &res)
	assert.Equal(t, false, res["ok"])
	assert.Contains(t, res["error"], "403")
	_, err = e.Webhooks.QueueWebhookDelivery(ctx, &webhooks.WebhookDelivery{WebhookID: uuid.MustParse(wid), Event: "daily.summary", DedupeKey: "retry", Payload: map[string]any{"path": "/", "data": map[string]any{}}})
	require.NoError(t, err)
	sendWebhooks(t, e)
	owner.do("GET", base+"/webhooks/"+wid+"/deliveries", nil, 200, &deliveries)
	assert.Equal(t, "pending", deliveries[0]["status"])
	assert.EqualValues(t, 1, deliveries[0]["attempts"])
	assert.Contains(t, deliveries[0]["last_error"], "403")
	assert.NotEmpty(t, deliveries[0]["next_attempt_at"])

	owner.do("DELETE", base+"/webhooks/"+wid, nil, 204, nil)
	owner.do("GET", base+"/webhooks", nil, 200, &deliveries)
	assert.Empty(t, deliveries)
	owner.do("DELETE", base+"/webhooks/"+wid, nil, 404, nil)
}

func TestReadAPIKey(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "HA PV", "peak_power_kwp": 5}, 201, &sys)
	base := "/systems/" + sys["id"].(string)

	owner.do("POST", base+"/api-keys", obj{"name": "x", "scopes": []string{"members:read"}}, 400, nil)
	var readKey, bothKey obj
	owner.do("POST", base+"/api-keys", obj{"name": "home assistant", "scopes": []string{"data:read"}}, 201, &readKey)
	assert.Equal(t, []any{"data:read"}, readKey["api_key"].(obj)["scopes"])
	owner.do("POST", base+"/api-keys", obj{"name": "gateway", "scopes": []string{"data:ingest", "data:read"}}, 201, &bothKey)
	reader := &client{e: e, http: e.anon().http, apiKey: readKey["key"].(string)}
	both := &client{e: e, http: e.anon().http, apiKey: bothKey["key"].(string)}

	both.do("POST", base+"/ingest/measurements", obj{"create_inverters": true, "measurements": []obj{
		{"inverter": "1", "time": time.Now().Add(-time.Minute).Format(time.RFC3339), "pac_w": 1500, "eday_wh": 900},
	}}, 200, nil)
	var live obj
	reader.do("GET", base+"/live", nil, 200, &live)
	assert.Equal(t, 1500.0, live["pac_w"])
	for _, path := range []string{"/overview?period=day", "/daily-energy?from=2026-01-01&to=2026-02-01", "/weather", "/prices", "/advice"} {
		reader.do("GET", base+path, nil, 200, nil)
	}
	reader.do("POST", base+"/ingest/measurements", obj{"measurements": []obj{{"inverter": "1", "time": time.Now().Format(time.RFC3339)}}}, 403, nil)
	// only the data endpoints take keys
	reader.do("GET", base, nil, 401, nil)
	reader.do("GET", base+"/events", nil, 401, nil)
	reader.do("GET", base+"/webhooks", nil, 401, nil)
	var other obj
	owner.do("POST", "/systems", obj{"name": "Other"}, 201, &other)
	reader.do("GET", "/systems/"+other["id"].(string)+"/live", nil, 404, nil)
}

func TestPricesAndNegativeFeedIn(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Price PV", "peak_power_kwp": 10, "tariff_per_kwh": 0.1}, 201, &sys)
	base := "/systems/" + sys["id"].(string)
	assert.NotContains(t, sys, "price_zone")
	assert.Equal(t, false, sys["unpaid_at_negative_prices"])
	owner.do("PATCH", base, obj{"price_zone": "CH"}, 400, nil)

	// 1000 W from 11:00 to 14:00 on a day with negative prices 12:00–13:00
	loc := mustLoc(t)
	day := time.Date(2025, 5, 11, 0, 0, 0, 0, loc)
	var ps []prices.MarketPrice
	for h := 0; h < 24; h++ {
		p := 50.0
		if h == 12 {
			p = -25
		}
		s := day.Add(time.Duration(h) * time.Hour)
		ps = append(ps, prices.MarketPrice{Zone: prices.ZoneDELU, Start: s, End: s.Add(time.Hour), EURMWh: p})
	}
	require.NoError(t, e.Prices.UpsertMarketPrices(ctx, ps))
	var key obj
	owner.do("POST", base+"/api-keys", obj{"name": "gw"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}
	var ms []obj
	for m := 0; m < 180; m++ {
		ts := day.Add(11*time.Hour + time.Duration(m)*time.Minute)
		ms = append(ms, obj{"inverter": "1", "time": ts.Format(time.RFC3339), "pac_w": 1000, "eday_wh": float64(m+1) * 1000 / 60})
	}
	device.do("POST", base+"/ingest/measurements", obj{"create_inverters": true, "measurements": ms}, 200, nil)

	// without a zone: no prices
	var ov obj
	owner.do("GET", base+"/overview?period=day&date=2025-05-11", nil, 200, &ov)
	assert.NotContains(t, ov, "prices")
	assert.NotContains(t, ov["summary"], "negative_price_kwh")
	assert.InDelta(t, 0.3, ov["summary"].(obj)["revenue"], 1e-6)

	owner.do("PATCH", base, obj{"price_zone": "DE-LU"}, 200, &sys)
	assert.Equal(t, "DE-LU", sys["price_zone"])
	owner.do("GET", base+"/overview?period=day&date=2025-05-11", nil, 200, &ov)
	require.Len(t, ov["prices"], 24)
	assert.Equal(t, -25.0, ov["prices"].([]any)[12].(obj)["eur_mwh"])
	s := ov["summary"].(obj)
	assert.InDelta(t, 1.0, s["negative_price_kwh"], 1e-6)
	assert.Equal(t, 1.0, s["negative_price_hours"])
	assert.NotContains(t, s, "unpaid_revenue", "paid unless the system is unpaid at negative prices")
	assert.InDelta(t, 0.3, s["revenue"], 1e-6)

	// Solarspitzengesetz: no payment for that hour
	owner.do("PATCH", base, obj{"unpaid_at_negative_prices": true}, 200, &sys)
	for _, q := range []string{"period=day&date=2025-05-11", "period=month&date=2025-05-11", "period=total"} {
		owner.do("GET", base+"/overview?"+q, nil, 200, &ov)
		s = ov["summary"].(obj)
		assert.InDelta(t, 1.0, s["negative_price_kwh"], 1e-6, q)
		assert.InDelta(t, 0.1, s["unpaid_revenue"], 1e-6, q)
		assert.InDelta(t, 0.2, s["revenue"], 1e-6, q)
	}

	var prices obj
	owner.do("GET", base+"/prices?from=2025-05-11&to=2025-05-12", nil, 200, &prices)
	assert.Equal(t, "DE-LU", prices["zone"])
	assert.Len(t, prices["prices"], 24)
	owner.do("GET", base+"/prices?from=2025-05-11&to=2025-07-12", nil, 400, nil)
	owner.do("GET", base+"/prices?from=2025-05-12&to=2025-05-11", nil, 400, nil)

	// clearing the zone
	var cleared obj
	owner.do("PATCH", base, obj{"price_zone": nil}, 200, &cleared)
	assert.NotContains(t, cleared, "price_zone")
}

func TestAdviceEndpoint(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Advice PV", "peak_power_kwp": 10, "latitude": 48.4, "longitude": 10.8, "price_zone": "AT"}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))

	var a obj
	owner.do("GET", "/systems/"+sid.String()+"/advice", nil, 200, &a)
	assert.Equal(t, true, a["available"])
	assert.EqualValues(t, 3, a["hours"])
	assert.NotEmpty(t, a["slots"])
	owner.do("GET", "/systems/"+sid.String()+"/advice?hours=9", nil, 400, nil)

	// In the morning: the sun peaks at 13:00 in the fake forecast, the
	// cheapest prices are in the evening.
	loc := mustLoc(t)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var ps []prices.MarketPrice
	for h := 0; h < 24; h++ {
		s := today.Add(time.Duration(h) * time.Hour)
		p := 100.0 - float64(h)
		ps = append(ps, prices.MarketPrice{Zone: prices.ZoneAT, Start: s, End: s.Add(time.Hour), EURMWh: p})
	}
	require.NoError(t, e.Prices.UpsertMarketPrices(ctx, ps))
	svc := advice.NewService(e.Weather, e.Prices)
	svc.SetClock(func() time.Time { return today.Add(8 * time.Hour) })
	system, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)
	adv, err := svc.Today(ctx, system, 3)
	require.NoError(t, err)
	require.NotNil(t, adv.Solar)
	// Around the 13:00 peak (the fake labels irradiance with the slot start).
	assert.InDelta(t, 11.5, adv.Solar.Start.Sub(today).Hours(), 0.3)
	assert.Equal(t, 3*time.Hour, adv.Solar.End.Sub(adv.Solar.Start))
	require.NotNil(t, adv.Cheap)
	assert.Equal(t, today.Add(21*time.Hour), adv.Cheap.Start.In(loc))
	assert.Equal(t, 78.0, *adv.Cheap.MeanEURMWh)
}

func TestPowerLimitEvents(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Limit PV", "peak_power_kwp": 10}, 201, &sys)
	base := "/systems/" + sys["id"].(string)
	var key obj
	owner.do("POST", base+"/api-keys", obj{"name": "gw"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}

	t0 := time.Now().Add(-30 * time.Minute).Truncate(time.Minute)
	at := func(m int) string { return t0.Add(time.Duration(m) * time.Minute).Format(time.RFC3339) }
	device.do("POST", base+"/ingest/measurements", obj{"create_inverters": true, "measurements": []obj{
		{"inverter": "1", "time": at(0), "pac_w": 5000, "status_code": 3, "status_text": "MPP"},
		{"inverter": "1", "time": at(5), "pac_w": 3000, "power_limit_pct": 60},
		{"inverter": "1", "time": at(10), "pac_w": 3000, "power_limit_pct": 60},
		{"inverter": "1", "time": at(20), "pac_w": 5000, "power_limit_pct": 100},
	}}, 200, nil)
	device.do("POST", base+"/ingest/measurements", obj{"measurements": []obj{
		{"inverter": "1", "time": at(25), "pac_w": 2000, "power_limit_pct": 101},
	}}, 400, nil)

	var evs obj
	owner.do("GET", base+"/events", nil, 200, &evs)
	items := evs["items"].([]any)
	require.Len(t, items, 3, "MPP, MPP limited to 60 %, MPP again")
	limited := items[1].(obj)
	assert.Equal(t, 60.0, limited["power_limit_pct"])
	assert.Equal(t, "MPP", limited["status_text"], "a limit alone keeps the status")
	assert.NotContains(t, items[0].(obj), "power_limit_pct")

	var live obj
	owner.do("GET", base+"/live", nil, 200, &live)
	assert.NotContains(t, live["inverters"].([]any)[0].(obj), "power_limit_pct")
	device.do("POST", base+"/ingest/measurements", obj{"measurements": []obj{
		{"inverter": "1", "time": at(25), "pac_w": 2000, "power_limit_pct": 40},
	}}, 200, nil)
	owner.do("GET", base+"/live", nil, 200, &live)
	assert.Equal(t, 40.0, live["inverters"].([]any)[0].(obj)["power_limit_pct"])
}

func TestPVOutputExport(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "PVO PV", "peak_power_kwp": 10}, 201, &sys)
	base := "/systems/" + sys["id"].(string)

	create := obj{"kind": "pvoutput_export", "name": "PVOutput", "poll_interval_s": 300, "config": obj{"pvoutput_system_id": "abc", "api_key": "k"}}
	owner.do("POST", base+"/data-sources", create, 400, nil)
	create["config"] = obj{"pvoutput_system_id": "4711"}
	owner.do("POST", base+"/data-sources", create, 400, nil)
	create["config"] = obj{"pvoutput_system_id": "4711", "api_key": "pvo-key"}
	var ds obj
	owner.do("POST", base+"/data-sources", create, 201, &ds)
	assert.Equal(t, obj{"pvoutput_system_id": "4711", "has_api_key": true}, ds["config"])
	// the key is kept when only the ID changes
	owner.do("PATCH", base+"/data-sources/"+ds["id"].(string), obj{"config": obj{"pvoutput_system_id": "4712"}}, 200, &ds)
	assert.Equal(t, true, ds["config"].(obj)["has_api_key"])

	var key obj
	owner.do("POST", base+"/api-keys", obj{"name": "gw"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}
	var ms []obj
	start := time.Now().Add(-40 * time.Minute).Truncate(5 * time.Minute)
	for m := 0; m < 30; m++ {
		ms = append(ms, obj{"inverter": "1", "time": start.Add(time.Duration(m) * time.Minute).Format(time.RFC3339), "pac_w": 1200})
	}
	device.do("POST", base+"/ingest/measurements", obj{"create_inverters": true, "measurements": ms}, 200, nil)
	// The first run starts at local midnight; shortly after midnight the
	// samples are from the day before, so start the uploads at them.
	state, err := json.Marshal(pvoutput.State{StatusUntil: start})
	require.NoError(t, err)
	require.NoError(t, e.DataSources.SetDataSourceState(context.Background(), uuid.MustParse(ds["id"].(string)), state))

	owner.do("POST", base+"/data-sources/"+ds["id"].(string)+"/run", nil, 200, &ds)
	assert.Equal(t, "", ds["last_error"])
	assert.NotEmpty(t, ds["uploaded_until"])
	got := hitsFor(e.pvoutput(), "/service/r2/addbatchstatus.jsp")
	require.NotEmpty(t, got)
	assert.Equal(t, "pvo-key", got[0].header.Get("X-Pvoutput-Apikey"))
	assert.Equal(t, "4712", got[0].header.Get("X-Pvoutput-SystemId"))
	form, err := url.ParseQuery(string(got[0].body))
	require.NoError(t, err)
	rows := strings.Split(form.Get("data"), ";")
	require.NotEmpty(t, rows)
	assert.Regexp(t, `^\d{8},\d{2}:\d{2},\d+,1200$`, rows[0])
}
