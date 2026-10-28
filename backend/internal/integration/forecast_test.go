//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMorningForecast(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	email := uniq("owner")
	uid := e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Forecast PV", "peak_power_kwp": 10, "latitude": 48.4, "longitude": 10.8}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))
	got, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)

	loc := mustLoc(t)
	now := time.Now().In(loc)
	morning := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, loc)
	f := alerting.NewForecaster(e.Alerting.Store(), e.Weather.ExpectedDays)
	f.SetClock(func() time.Time { return morning })
	forecasts := func() []notifications.Notification {
		ns, err := e.Notifications.ListNotifications(ctx, uid, notifications.NotificationFilter{Limit: 50})
		require.NoError(t, err)
		var out []notifications.Notification
		for _, n := range ns {
			if n.Type == notifications.NotificationForecast && *n.SystemID == sid {
				out = append(out, n)
			}
		}
		return out
	}

	// Not opted in: nothing.
	require.NoError(t, f.RunSystem(ctx, *got))
	assert.Empty(t, forecasts())

	var prefs obj
	owner.do("GET", "/me/notification-preferences", nil, 200, &prefs)
	def := prefs["default"].(map[string]any)
	assert.Equal(t, obj{"channels": []any{"push"}, "enabled": false}, def["forecast"])
	def["forecast"] = obj{"channels": []string{"push"}, "enabled": true}
	owner.do("PUT", "/me/notification-preferences", obj{"default": def}, 200, &prefs)
	assert.Equal(t, true, prefs["default"].(map[string]any)["forecast"].(obj)["enabled"])

	// Too early in the day: nothing yet.
	f.SetClock(func() time.Time { return morning.Add(-2 * time.Hour) })
	require.NoError(t, f.RunSystem(ctx, *got))
	assert.Empty(t, forecasts())

	f.SetClock(func() time.Time { return morning })
	require.NoError(t, f.RunSystem(ctx, *got))
	ns := forecasts()
	require.Len(t, ns, 1)
	assert.NotNil(t, ns[0].ReadAt, "not counted as unread")
	assert.InDelta(t, 50, ns[0].Payload["expected_kwh"], 6)
	m := notifications.RenderNotification("en", ns[0])
	assert.Contains(t, m.Title, "Forecast PV: about")
	assert.Equal(t, "/s/"+sid.String()+"/overview", notifications.Link(ns[0]))

	// Once a day.
	f.SetClock(func() time.Time { return morning.Add(30 * time.Minute) })
	require.NoError(t, f.RunSystem(ctx, *got))
	assert.Len(t, forecasts(), 1)
}
