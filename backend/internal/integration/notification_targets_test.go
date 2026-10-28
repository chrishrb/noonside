//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hit struct {
	path   string
	header http.Header
	body   []byte
}

// targetServer records requests to a fake ntfy / webhook receiver.
func targetServer(t *testing.T) (*httptest.Server, func() []hit) {
	t.Helper()
	var mu sync.Mutex
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, hit{r.URL.Path, r.Header.Clone(), b})
		mu.Unlock()
		if r.URL.Path == "/broken" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []hit { mu.Lock(); defer mu.Unlock(); return append([]hit(nil), hits...) }
}

func TestNotificationTargets(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ownerEmail, otherEmail := uniq("owner"), uniq("other")
	e.createUser(ownerEmail, "owner-password-1", true)
	e.createUser(otherEmail, "other-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	other := e.login(otherEmail, "other-password-1")
	srv, hits := targetServer(t)

	var tg obj
	owner.do("GET", "/me/notification-targets", nil, 200, &tg)
	assert.Equal(t, obj{"ntfy_url": "", "ntfy_has_token": false, "webhook_url": "", "webhook_has_secret": false}, tg)
	owner.do("POST", "/me/notification-targets/test?channel=ntfy", nil, 409, nil)

	// validation
	owner.do("PUT", "/me/notification-targets", obj{"ntfy_url": "ftp://ntfy.sh/x"}, 400, nil)
	owner.do("PUT", "/me/notification-targets", obj{"ntfy_url": "https://ntfy.sh/"}, 400, nil)
	owner.do("PUT", "/me/notification-targets", obj{"webhook_secret": "orphan"}, 400, nil)

	owner.do("PUT", "/me/notification-targets", obj{
		"ntfy_url": srv.URL + "/solar-topic", "ntfy_token": "tk_abc",
		"webhook_url": srv.URL + "/hook", "webhook_secret": "whsec",
	}, 200, &tg)
	assert.Equal(t, obj{"ntfy_url": srv.URL + "/solar-topic", "ntfy_has_token": true, "webhook_url": srv.URL + "/hook", "webhook_has_secret": true}, tg)

	// the secrets are stored sealed
	var tokenEnc, secretEnc string
	require.NoError(t, e.db.Pool().QueryRow(ctx, `
		SELECT ntfy_token_enc, webhook_secret_enc FROM notification_targets t JOIN profiles p ON p.id = t.user_id
		WHERE p.email = $1`, ownerEmail).Scan(&tokenEnc, &secretEnc))
	assert.NotContains(t, tokenEnc, "tk_abc")
	assert.NotContains(t, secretEnc, "whsec")

	// fields left out stay; another user sees only their own
	owner.do("PUT", "/me/notification-targets", obj{"ntfy_token": ""}, 200, &tg)
	assert.Equal(t, false, tg["ntfy_has_token"])
	assert.Equal(t, true, tg["webhook_has_secret"])
	other.do("GET", "/me/notification-targets", nil, 200, &tg)
	assert.Equal(t, "", tg["webhook_url"])
	other.do("POST", "/me/notification-targets/test?channel=webhook", nil, 409, nil)
	owner.do("PUT", "/me/notification-targets", obj{"ntfy_token": "tk_abc"}, 200, nil)

	// test sends
	var res obj
	owner.do("POST", "/me/notification-targets/test?channel=webhook", nil, 200, &res)
	assert.Equal(t, obj{"ok": true}, res)
	owner.do("POST", "/me/notification-targets/test?channel=ntfy", nil, 200, &res)
	assert.Equal(t, obj{"ok": true}, res)
	h := hits()
	require.Len(t, h, 2)
	assert.Equal(t, "/hook", h[0].path)
	assert.Equal(t, notify.Sign("whsec", h[0].body), h[0].header.Get(notify.SignatureHeader))
	var payload obj
	require.NoError(t, json.Unmarshal(h[0].body, &payload))
	assert.Equal(t, "test", payload["type"])
	assert.Equal(t, "/solar-topic", h[1].path)
	assert.Equal(t, "Bearer tk_abc", h[1].header.Get("Authorization"))

	owner.do("PUT", "/me/notification-targets", obj{"webhook_url": srv.URL + "/broken"}, 200, nil)
	owner.do("POST", "/me/notification-targets/test?channel=webhook", nil, 200, &res)
	assert.Equal(t, false, res["ok"])
	assert.Contains(t, res["error"], "403")
	owner.do("PUT", "/me/notification-targets", obj{"webhook_url": srv.URL + "/hook"}, 200, &tg)
	assert.Equal(t, true, tg["webhook_has_secret"], "changing the URL keeps the secret")

	// an alert queues deliveries to both targets; switching ntfy off for
	// the system drops it
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Targets PV"}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))
	var prefs obj
	owner.do("GET", "/me/notification-preferences", nil, 200, &prefs)
	assert.Contains(t, prefs["default"].(obj)["alerts"].(obj)["channels"], "ntfy", "on by default")
	def := pref([]string{"in_app", "ntfy", "webhook"}, "warning")
	owner.do("PUT", "/me/notification-preferences", obj{"default": def}, 200, nil)

	ds, err := e.DataSources.CreateDataSource(ctx, &datasources.DataSource{
		SystemID: sid, Kind: "solarlog_remote", Name: "Export", Config: []byte(`{}`), Enabled: true, PollInterval: time.Hour,
	})
	require.NoError(t, err)
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now().Add(-3*time.Hour), nil))
	require.NoError(t, e.DataSources.RecordDataSourceRun(ctx, ds.ID, time.Now(), errors.New("no such directory")))
	system, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)
	require.NoError(t, alerting.NewEngine(e.Alerting.Store()).EvaluateSystem(ctx, *system))

	channels := func() []string {
		rows, err := e.db.Pool().Query(ctx, `
			SELECT d.channel FROM notification_deliveries d JOIN notifications n ON n.id = d.notification_id
			WHERE n.system_id = $1 ORDER BY d.channel`, sid)
		require.NoError(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var c string
			require.NoError(t, rows.Scan(&c))
			out = append(out, c)
		}
		return out
	}
	assert.Equal(t, []string{"ntfy", "webhook"}, channels())

	// the dispatcher delivers them (it also sends other tests' due deliveries)
	before := len(hits())
	d := notifications.NewDispatcher(e.Notifications, nil, nil, "http://app.test").WithTargets(e.notify, e.box)
	require.NoError(t, d.Run(ctx, time.Now().Add(30*time.Second)))
	var paths []string
	var alertHook *hit
	for _, x := range hits()[before:] {
		paths = append(paths, x.path)
		if x.path == "/hook" {
			alertHook = &x
		}
	}
	assert.ElementsMatch(t, []string{"/solar-topic", "/hook"}, paths)
	require.NotNil(t, alertHook)
	require.NoError(t, json.Unmarshal(alertHook.body, &payload))
	assert.Equal(t, "alert_opened", payload["type"])
	assert.Equal(t, alerting.KindDataSourceFailing, payload["kind"])
	assert.Equal(t, sid.String(), payload["system_id"])
	assert.Equal(t, "http://app.test/s/"+sid.String()+"/settings/sources", payload["url"])
	assert.Equal(t, notify.Sign("whsec", alertHook.body), alertHook.header.Get(notify.SignatureHeader))
	var sent int
	require.NoError(t, e.db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM notification_deliveries d JOIN notifications n ON n.id = d.notification_id
		WHERE n.system_id = $1 AND d.status = 'sent'`, sid).Scan(&sent))
	assert.Equal(t, 2, sent)

	// per-system override: no ntfy
	off := pref([]string{"in_app", "webhook"}, "warning")
	owner.do("PUT", "/me/notification-preferences", obj{"default": def, "systems": []obj{withSystem(off, sid)}}, 200, nil)
	recipients, err := e.Notifications.ListRecipients(ctx, sid)
	require.NoError(t, err)
	require.Len(t, recipients, 1)
	assert.Equal(t, []string{"in_app", "webhook"}, recipients[0].Prefs.AlertChannels)
	assert.Equal(t, notifications.TargetsSet{Ntfy: true, Webhook: true}, recipients[0].Targets)
	ch, _, _ := notifications.Channels(recipients[0].Prefs, recipients[0].Targets, notifications.SeverityWarning, notifications.NotificationAlertOpened, time.Now())
	assert.Equal(t, []string{"webhook"}, ch)

	// removing a URL removes its secret
	owner.do("PUT", "/me/notification-targets", obj{"webhook_url": ""}, 200, &tg)
	assert.Equal(t, obj{"ntfy_url": srv.URL + "/solar-topic", "ntfy_has_token": true, "webhook_url": "", "webhook_has_secret": false}, tg)
}
