//go:build integration

package integration

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertsAndNotifications(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	ownerEmail, viewerEmail, strangerEmail := uniq("owner"), uniq("viewer"), uniq("stranger")
	e.createUser(ownerEmail, "owner-password-1", true)
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	e.createUser(strangerEmail, "stranger-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	viewer := e.login(viewerEmail, "viewer-password-1")
	stranger := e.login(strangerEmail, "stranger-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Alerts PV"}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))
	base := "/systems/" + sid.String()
	require.NoError(t, e.Systems.UpsertMember(ctx, sid, viewerID, auth.RoleViewer))

	// rules: defaults, overrides, scopes
	var rules []obj
	owner.do("GET", base+"/alert-rules", nil, 200, &rules)
	require.Len(t, rules, len(alerting.Rules))
	viewer.do("GET", base+"/alert-rules", nil, 403, nil)
	stranger.do("GET", base+"/alert-rules", nil, 404, nil)
	owner.do("PUT", base+"/alert-rules", []obj{{"kind": "nope", "enabled": true}}, 400, nil)
	owner.do("PUT", base+"/alert-rules", []obj{{"kind": alerting.KindNoData, "enabled": true, "params": obj{"bogus": 1}}}, 400, nil)
	owner.do("PUT", base+"/alert-rules", []obj{
		{"kind": alerting.KindDataSourceFailing, "enabled": true, "params": obj{"hours": 0}},
		{"kind": alerting.KindZeroDay, "enabled": false},
	}, 200, &rules)
	for _, r := range rules {
		switch r["kind"] {
		case alerting.KindDataSourceFailing:
			assert.Equal(t, 0.0, r["params"].(obj)["hours"])
			assert.Equal(t, 2.0, r["defaults"].(obj)["hours"])
		case alerting.KindZeroDay:
			assert.Equal(t, false, r["enabled"])
		}
	}

	// preferences: the owner only wants errors, so a warning skips them
	var prefs obj
	viewer.do("GET", "/me/notification-preferences", nil, 200, &prefs)
	assert.Equal(t, "warning", prefs["default"].(obj)["alerts"].(obj)["min_severity"])
	assert.Equal(t, []any{"email"}, prefs["default"].(obj)["reports"].(obj)["channels"])
	assert.Equal(t, []any{"push"}, prefs["default"].(obj)["forecast"].(obj)["channels"])
	assert.Equal(t, true, prefs["email_available"])
	assert.Equal(t, false, prefs["push_available"])
	require.Len(t, prefs["member_systems"], 1)
	assert.Equal(t, sid.String(), prefs["member_systems"].([]any)[0].(obj)["system_id"])
	defaultPref := pref([]string{"in_app", "email", "push"}, "warning")
	stranger.do("PUT", "/me/notification-preferences", obj{"default": defaultPref, "systems": []obj{withSystem(defaultPref, sid)}}, 400, nil)
	owner.do("PUT", "/me/notification-preferences", obj{
		"default": defaultPref,
		"systems": []obj{withSystem(pref([]string{"in_app"}, "error"), sid)},
	}, 200, &prefs)
	require.Len(t, prefs["systems"], 1)

	// a failing data source opens an alert
	ds, err := e.DataSources.CreateDataSource(ctx, &datasources.DataSource{
		SystemID: sid, Kind: "solarlog_remote", Name: "Export", Config: []byte(`{}`), Enabled: true, PollInterval: time.Hour,
	})
	require.NoError(t, err)
	// failing since an hour ago (not since now: the host and database clocks may differ slightly)
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now().Add(-time.Hour), nil))
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now(), errors.New("no such directory")))
	engine := alerting.NewEngine(e.Alerting.Store())
	system, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)
	require.NoError(t, engine.EvaluateSystem(ctx, *system))
	require.NoError(t, engine.EvaluateSystem(ctx, *system), "evaluating again keeps one alert")

	var page obj
	viewer.do("GET", base+"/alerts", nil, 200, &page)
	items := page["items"].([]any)
	require.Len(t, items, 1)
	alert := items[0].(obj)
	assert.Equal(t, alerting.KindDataSourceFailing, alert["kind"])
	assert.Equal(t, "Data source Export is failing", alert["title"])
	assert.Equal(t, ds.ID.String(), alert["data_source_id"])
	stranger.do("GET", base+"/alerts", nil, 404, nil)
	alertPath := base + "/alerts/" + alert["id"].(string)

	var count obj
	viewer.do("GET", "/me/notifications/unread-count", nil, 200, &count)
	assert.Equal(t, 1.0, count["count"])
	owner.do("GET", "/me/notifications/unread-count", nil, 200, &count)
	assert.Equal(t, 0.0, count["count"], "below the owner's minimum severity")
	stranger.do("GET", "/me/notifications/unread-count", nil, 200, &count)
	assert.Equal(t, 0.0, count["count"])

	viewer.do("GET", "/me/notifications", nil, 200, &page)
	notes := page["items"].([]any)
	require.Len(t, notes, 1)
	note := notes[0].(obj)
	assert.Equal(t, "alert_opened", note["type"])
	assert.Equal(t, "Data source Export is failing", note["title"])
	assert.Equal(t, "/s/"+sid.String()+"/settings/sources", note["url"])

	// email and push deliveries are queued for the viewer
	var channels int
	require.NoError(t, e.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM notification_deliveries WHERE notification_id = $1`, note["id"]).Scan(&channels))
	assert.Equal(t, 2, channels)

	// the viewer's language applies to rendered text
	viewer.do("PATCH", "/me", obj{"locale": "de"}, 200, nil)
	viewer.do("GET", "/me/notifications?unread=true", nil, 200, &page)
	assert.Equal(t, "Datenquelle Export schlägt fehl", page["items"].([]any)[0].(obj)["title"])

	// others can't mark it read; the viewer can
	stranger.do("POST", "/me/notifications/read", obj{"ids": []string{note["id"].(string)}}, 200, nil)
	viewer.do("POST", "/me/notifications/read", obj{}, 400, nil)
	viewer.do("GET", "/me/notifications/unread-count", nil, 200, &count)
	assert.Equal(t, 1.0, count["count"])
	viewer.do("POST", "/me/notifications/read", obj{"ids": []string{note["id"].(string)}}, 200, &count)
	assert.Equal(t, 0.0, count["count"])

	// ack / snooze need alerts:manage
	viewer.do("POST", alertPath+"/ack", nil, 403, nil)
	var acked obj
	owner.do("POST", alertPath+"/ack", nil, 200, &acked)
	assert.NotEmpty(t, acked["acked_at"])
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	owner.do("POST", alertPath+"/snooze", obj{"until": until}, 200, &acked)
	assert.NotEmpty(t, acked["snoozed_until"])
	var unsnoozed obj
	owner.do("POST", alertPath+"/snooze", obj{"until": nil}, 200, &unsnoozed)
	assert.Nil(t, unsnoozed["snoozed_until"])
	owner.do("POST", base+"/alerts/"+uuid.NewString()+"/ack", nil, 404, nil)

	// the alert resolves after two evaluations without the condition, which
	// marks the notification about it opening read
	_, err = e.db.Pool().Exec(ctx, `UPDATE notifications SET read_at = NULL WHERE id = $1`, note["id"])
	require.NoError(t, err)
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now(), nil))
	require.NoError(t, engine.EvaluateSystem(ctx, *system))
	viewer.do("GET", base+"/alerts", nil, 200, &page)
	assert.Len(t, page["items"], 1, "still open after one miss")
	require.NoError(t, engine.EvaluateSystem(ctx, *system))
	viewer.do("GET", base+"/alerts", nil, 200, &page)
	assert.Empty(t, page["items"])
	viewer.do("GET", base+"/alerts?state=all", nil, 200, &page)
	require.Len(t, page["items"], 1)
	assert.NotEmpty(t, page["items"].([]any)[0].(obj)["resolved_at"])

	viewer.do("GET", "/me/notifications?unread=true", nil, 200, &page)
	require.Len(t, page["items"], 1)
	assert.Equal(t, "alert_resolved", page["items"].([]any)[0].(obj)["type"], "the opened notification is read")
	resolvedID := page["items"].([]any)[0].(obj)["id"].(string)

	// deleting: only the caller's own, by id or all read ones
	viewer.do("POST", "/me/notifications/delete", obj{}, 400, nil)
	stranger.do("POST", "/me/notifications/delete", obj{"ids": []string{resolvedID}}, 200, nil)
	stranger.do("POST", "/me/notifications/delete", obj{"read": true}, 200, nil)
	viewer.do("GET", "/me/notifications", nil, 200, &page)
	require.Len(t, page["items"], 2)
	viewer.do("POST", "/me/notifications/delete", obj{"read": true}, 200, &count)
	assert.Equal(t, 1.0, count["count"])
	viewer.do("GET", "/me/notifications", nil, 200, &page)
	require.Len(t, page["items"], 1, "the unread one is kept")
	assert.Equal(t, resolvedID, page["items"].([]any)[0].(obj)["id"])
	viewer.do("POST", "/me/notifications/delete", obj{"ids": []string{resolvedID}}, 200, &count)
	assert.Equal(t, 0.0, count["count"])
	viewer.do("GET", "/me/notifications", nil, 200, &page)
	assert.Empty(t, page["items"])
	require.NoError(t, e.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM notification_deliveries WHERE notification_id = $1`, resolvedID).Scan(&channels))
	assert.Zero(t, channels, "deliveries go with the notification")
}

func withSystem(p obj, id uuid.UUID) obj {
	out := obj{"system_id": id.String()}
	for k, v := range p {
		out[k] = v
	}
	return out
}

func TestPushSubscriptions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("push")
	id := e.createUser(email, "push-password-1", false)
	c := e.login(email, "push-password-1")

	var cfg obj
	c.do("GET", "/push/config", nil, 200, &cfg)
	assert.Equal(t, false, cfg["enabled"])

	keys := obj{"p256dh": "BKey", "auth": "auth"}
	c.do("PUT", "/me/push-subscriptions", obj{"endpoint": "https://169.254.169.254/x", "keys": keys}, 400, nil)
	endpoint := "https://fcm.googleapis.com/fcm/send/" + uuid.NewString()
	c.do("PUT", "/me/push-subscriptions", obj{"endpoint": endpoint, "keys": keys, "user_agent": "test"}, 204, nil)
	c.do("PUT", "/me/push-subscriptions", obj{"endpoint": endpoint, "keys": keys}, 204, nil)
	subs, err := e.Notifications.ListPushSubscriptions(ctx, id)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	c.do("POST", "/me/push-subscriptions/test", nil, 409, nil)

	c.do("DELETE", "/me/push-subscriptions?endpoint="+url.QueryEscape(endpoint), nil, 204, nil)
	subs, err = e.Notifications.ListPushSubscriptions(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestPruneNotifications(t *testing.T) {
	e := newEnv(t)
	// Cutoffs in the past: the shared database keeps its rows, the SQL runs.
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	n, err := e.Notifications.PruneNotifications(context.Background(), old, old, 10)
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = e.Alerting.PruneResolved(context.Background(), old, 10)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// pref is a notification preference with the given alert channels and
// minimum severity; reports and the forecast are off.
func pref(alertChannels []string, minSeverity string) obj {
	return obj{
		"alerts":   obj{"channels": alertChannels, "min_severity": minSeverity, "notify_resolved": true},
		"reports":  obj{"channels": []string{"email"}, "digest": "none"},
		"forecast": obj{"channels": []string{"push"}, "enabled": false},
	}
}
