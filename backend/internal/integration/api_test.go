//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uniq(prefix string) string {
	return fmt.Sprintf("%s-%s@example.com", prefix, uuid.NewString()[:8])
}

type obj = map[string]any

func TestPermissionsAndInvitations(t *testing.T) {
	e := newEnv(t)
	adminEmail := uniq("super")
	e.createUser(adminEmail, "super-password-1", true)
	super := e.login(adminEmail, "super-password-1")

	var sys obj
	super.do("POST", "/systems", obj{"name": "Test PV", "peak_power_kwp": 10}, 201, &sys)
	sid := sys["id"].(string)
	assert.Equal(t, "owner", sys["role"])

	// owner invites an admin and a viewer
	adminInv := uniq("admin")
	var inv obj
	super.do("POST", "/systems/"+sid+"/invitations", obj{"email": adminInv, "role": "admin"}, 201, &inv)
	assert.Equal(t, true, inv["email_sent"])
	assert.NotContains(t, inv, "token", "the link only goes to the invitee")
	assert.NotContains(t, inv, "accept_url")
	sent := e.outbox.last()
	assert.Contains(t, sent, "To: <"+adminInv+">")
	assert.Contains(t, sent, "Subject: You're invited to Test PV")
	adminToken := e.outbox.invitationToken(t)
	assert.Contains(t, sent, "http://app.test/invitations/"+adminToken, "mail contains the accept link")

	viewerEmail := uniq("viewer")
	super.do("POST", "/systems/"+sid+"/invitations", obj{"email": viewerEmail, "role": "viewer"}, 201, &inv)
	viewerToken := e.outbox.invitationToken(t)

	// public invitation info
	var info obj
	e.anon().do("GET", "/invitations/"+viewerToken, nil, 200, &info)
	assert.Equal(t, "Test PV", info["system_name"])
	assert.Equal(t, false, info["user_exists"])
	e.anon().do("GET", "/invitations/s0i_bogus", nil, 404, nil)

	// accept as new users (registration)
	var res obj
	e.anon().do("POST", "/invitations/"+viewerToken+"/accept", obj{"name": "V"}, 400, nil)
	e.anon().do("POST", "/invitations/"+viewerToken+"/accept", obj{"name": "V", "password": "short"}, 400, nil) // GoTrue password policy
	e.anon().do("POST", "/invitations/"+viewerToken+"/accept", obj{"name": "V", "password": "viewer-password-1"}, 200, &res)
	assert.Equal(t, true, res["account_created"])
	assert.Equal(t, "viewer", res["role"])
	assert.Equal(t, sid, res["system_id"])
	e.anon().do("POST", "/invitations/"+viewerToken+"/accept", obj{"name": "V", "password": "viewer-password-1"}, 404, nil)
	viewer := e.login(viewerEmail, "viewer-password-1") // the new account signs in with GoTrue

	e.anon().do("POST", "/invitations/"+adminToken+"/accept", obj{"name": "A", "password": "admin-password-1"}, 200, &res)
	admin := e.login(adminInv, "admin-password-1")

	// an existing account must sign in to accept, and only for its own address
	otherEmail := uniq("existing")
	e.createUser(otherEmail, "existing-password-1", false)
	super.do("POST", "/systems/"+sid+"/invitations", obj{"email": otherEmail, "role": "viewer"}, 201, &inv)
	otherToken := e.outbox.invitationToken(t)
	e.anon().do("POST", "/invitations/"+otherToken+"/accept", obj{"name": "X", "password": "whatever-password"}, 401, nil)
	viewer.do("POST", "/invitations/"+otherToken+"/accept", nil, 403, nil)
	existing := e.login(otherEmail, "existing-password-1")
	existing.do("POST", "/invitations/"+otherToken+"/accept", nil, 200, &res)
	assert.Equal(t, false, res["account_created"])
	existing.do("GET", "/systems/"+sid, nil, 200, nil) // no new token needed: roles are read per request

	strangerEmail := uniq("stranger")
	e.createUser(strangerEmail, "stranger-password", false)
	stranger := e.login(strangerEmail, "stranger-password")

	sys1 := "/systems/" + sid
	cases := []struct {
		name   string
		c      *client
		method string
		path   string
		body   any
		want   int
	}{
		{"viewer reads system", viewer, "GET", sys1, nil, 200},
		{"viewer reads overview", viewer, "GET", sys1 + "/overview?period=day", nil, 200},
		{"viewer reads events", viewer, "GET", sys1 + "/events", nil, 200},
		{"viewer cannot edit", viewer, "PATCH", sys1, obj{"name": "x"}, 403},
		{"viewer cannot invite", viewer, "POST", sys1 + "/invitations", obj{"email": uniq("x"), "role": "viewer"}, 403},
		{"viewer cannot list members", viewer, "GET", sys1 + "/members", nil, 403},
		{"viewer cannot ingest", viewer, "POST", sys1 + "/ingest/daily", obj{"days": []obj{{"inverter": "1", "date": "2026-01-01", "energy_wh": 1}}}, 403},
		{"viewer cannot manage keys", viewer, "GET", sys1 + "/api-keys", nil, 403},
		{"viewer cannot create system", viewer, "POST", "/systems", obj{"name": "x"}, 403},
		{"admin edits", admin, "PATCH", sys1, obj{"location": "Neusäß"}, 200},
		{"admin lists members", admin, "GET", sys1 + "/members", nil, 200},
		{"admin cannot delete", admin, "DELETE", sys1, nil, 403},
		{"admin cannot invite owner", admin, "POST", sys1 + "/invitations", obj{"email": uniq("o"), "role": "owner"}, 403},
		{"admin invites viewer", admin, "POST", sys1 + "/invitations", obj{"email": uniq("v2"), "role": "viewer"}, 201},
		{"stranger gets 404", stranger, "GET", sys1, nil, 404},
		{"stranger cannot see overview", stranger, "GET", sys1 + "/overview?period=day", nil, 404},
		{"stranger lists nothing", stranger, "GET", "/systems", nil, 200},
		{"anonymous is rejected", e.anon(), "GET", sys1, nil, 401},
		{"garbage token is rejected", &client{e: e, http: e.anon().http, token: "x.y.z"}, "GET", sys1, nil, 401},
		{"invalid timezone", admin, "PATCH", sys1, obj{"timezone": "Mars/Base"}, 400},
		{"invalid period", viewer, "GET", sys1 + "/overview?period=week", nil, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := tc.c.raw(tc.method, tc.path, tc.body)
			assert.Equal(t, tc.want, status, string(raw))
			if tc.want >= 400 {
				assert.Contains(t, string(raw), `"message"`, "JSON error body")
			}
		})
	}

	var members []obj
	super.do("GET", sys1+"/members", nil, 200, &members)
	require.Len(t, members, 4)
	byEmail := map[string]string{}
	for _, m := range members {
		byEmail[m["email"].(string)] = m["user_id"].(string)
	}
	viewerID, adminID, ownerID := byEmail[viewerEmail], byEmail[adminInv], byEmail[adminEmail]

	// admin cannot promote to owner, owner can
	admin.do("PATCH", sys1+"/members/"+viewerID, obj{"role": "owner"}, 403, nil)
	admin.do("PATCH", sys1+"/members/"+viewerID, obj{"role": "admin"}, 200, nil)
	// last owner cannot be demoted or removed
	super.do("PATCH", sys1+"/members/"+ownerID, obj{"role": "admin"}, 409, nil)
	super.do("DELETE", sys1+"/members/"+ownerID, nil, 409, nil)
	// members can leave
	viewer.do("DELETE", sys1+"/members/"+viewerID, nil, 204, nil)
	// admin cannot remove the owner
	admin.do("DELETE", sys1+"/members/"+ownerID, nil, 403, nil)
	_ = adminID

	// /me lists memberships with scopes
	var me obj
	admin.do("GET", "/me", nil, 200, &me)
	require.Len(t, me["systems"], 1)
	m0 := me["systems"].([]any)[0].(obj)
	assert.Equal(t, "admin", m0["role"])
	assert.Contains(t, m0["scopes"], "members:invite")
	assert.NotContains(t, m0["scopes"], "system:delete")

	super.do("DELETE", sys1, nil, 204, nil)
	super.do("GET", sys1, nil, 404, nil)
}

func TestAdminUsers(t *testing.T) {
	e := newEnv(t)
	superEmail := uniq("super")
	superID := e.createUser(superEmail, "super-password-1", true)
	super := e.login(superEmail, "super-password-1")
	userEmail := uniq("plain")
	e.createUser(userEmail, "plain-password-1", false)
	plain := e.login(userEmail, "plain-password-1")

	plain.do("GET", "/admin/users", nil, 403, nil)
	plain.do("POST", "/admin/users", obj{"email": uniq("x"), "name": "X", "password": "x-password-12"}, 403, nil)

	var sys obj
	super.do("POST", "/systems", obj{"name": "Admin PV"}, 201, &sys)

	// create a user, then find them in the list
	newEmail := uniq("created")
	var created obj
	super.do("POST", "/admin/users", obj{"email": strings.ToUpper(newEmail), "name": " New User ", "password": "created-password-1"}, 201, &created)
	assert.Equal(t, newEmail, created["email"])
	assert.Equal(t, "New User", created["name"])
	assert.Equal(t, false, created["is_superadmin"])
	assert.Empty(t, created["systems"])
	super.do("POST", "/admin/users", obj{"email": newEmail, "name": "Dup", "password": "created-password-1"}, 409, nil)
	super.do("POST", "/admin/users", obj{"email": uniq("short"), "name": "S", "password": "short"}, 400, nil)
	e.login(newEmail, "created-password-1") // the account is confirmed

	var users []obj
	super.do("GET", "/admin/users", nil, 200, &users)
	byEmail := map[string]obj{}
	for _, u := range users {
		byEmail[u["email"].(string)] = u
	}
	require.Contains(t, byEmail, newEmail)
	me := byEmail[superEmail]
	require.NotNil(t, me)
	assert.Equal(t, true, me["is_superadmin"])
	assert.NotEmpty(t, me["last_sign_in_at"])
	assert.Contains(t, me["systems"], obj{"system_id": sys["id"], "name": "Admin PV", "role": "owner"})

	// promote and revoke
	uid := created["id"].(string)
	var upd obj
	super.do("PATCH", "/admin/users/"+uid, obj{"is_superadmin": true}, 200, &upd)
	assert.Equal(t, true, upd["is_superadmin"])
	var promotedMe obj
	e.login(newEmail, "created-password-1").do("GET", "/me", nil, 200, &promotedMe)
	assert.Equal(t, true, promotedMe["is_superadmin"], "flag is in the next access token")
	super.do("PATCH", "/admin/users/"+uid, obj{"is_superadmin": false}, 200, &upd)
	assert.Equal(t, false, upd["is_superadmin"])

	super.do("PATCH", "/admin/users/"+superID.String(), obj{"is_superadmin": false}, 400, nil)
	super.do("PATCH", "/admin/users/"+uuid.NewString(), obj{"is_superadmin": true}, 404, nil)
}

func TestProfileAndLogin(t *testing.T) {
	e := newEnv(t)
	email := uniq("user")
	e.createUser(email, "user-password-1", false)
	status, _ := signIn(email, "wrong-password-1")
	assert.Equal(t, 400, status)

	c := e.login(email, "user-password-1")
	var me obj
	c.do("GET", "/me", nil, 200, &me)
	assert.Equal(t, email, me["email"])
	assert.Equal(t, false, me["is_superadmin"])
	assert.Empty(t, me["systems"])

	c.do("PATCH", "/me", obj{"name": "Renamed"}, 200, &me)
	assert.Equal(t, "Renamed", me["name"])

	// open sign-up is disabled (GOTRUE_DISABLE_SIGNUP)
	body := strings.NewReader(`{"email":"` + uniq("open") + `","password":"open-password-1"}`)
	req, _ := http.NewRequest(http.MethodPost, authURL+"/signup", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 422, resp.StatusCode)
}

func TestIngestAndOverview(t *testing.T) {
	e := newEnv(t)
	email := uniq("super")
	e.createUser(email, "super-password-1", true)
	owner := e.login(email, "super-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Ingest PV", "peak_power_kwp": 10, "tariff_per_kwh": 0.1}, 201, &sys)
	sid := sys["id"].(string)
	owner.do("PUT", "/systems/"+sid+"/targets", obj{"target_kwh": []float64{0, 0, 0, 0, 0, 0, 0, 0, 3000, 0, 0, 0}}, 200, nil)
	owner.do("PUT", "/systems/"+sid+"/targets", obj{"target_kwh": []float64{1}}, 400, nil)

	var key obj
	owner.do("POST", "/systems/"+sid+"/api-keys", obj{"name": "gateway"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}
	assert.Equal(t, []any{"data:ingest"}, key["api_key"].(obj)["scopes"])

	day := time.Date(2026, 9, 1, 0, 0, 0, 0, mustLoc(t))
	var ms []obj
	for i := 0; i < 12; i++ { // 10:00..10:55
		ts := day.Add(10*time.Hour + time.Duration(i)*5*time.Minute)
		for inv, pac := range map[string]float64{"1": 1000, "2": 2000} {
			ms = append(ms, obj{"inverter": inv, "time": ts.Format(time.RFC3339), "pac_w": pac, "pdc_w": pac * 1.04,
				"eday_wh": pac * float64(i+1) / 12, "status_code": 3, "status_text": "MPP"})
		}
	}
	body := obj{"measurements": ms}
	device.do("POST", "/systems/"+sid+"/ingest/measurements", body, 400, nil) // unknown inverters
	body["create_inverters"] = true
	var res obj
	device.do("POST", "/systems/"+sid+"/ingest/measurements", body, 200, &res)
	assert.EqualValues(t, 24, res["accepted"])
	assert.EqualValues(t, 2, res["inverters_created"])
	// idempotent
	device.do("POST", "/systems/"+sid+"/ingest/measurements", body, 200, &res)
	assert.EqualValues(t, 0, res["inverters_created"])

	// ingest keys cannot read, and are bound to their system
	device.do("GET", "/systems/"+sid+"/overview?period=day", nil, 403, nil)
	var other obj
	owner.do("POST", "/systems", obj{"name": "Other"}, 201, &other)
	device.do("POST", "/systems/"+other["id"].(string)+"/ingest/measurements", body, 404, nil)

	var ov obj
	owner.do("GET", "/systems/"+sid+"/overview?period=day&date=2026-09-01", nil, 200, &ov)
	assert.Equal(t, "W", ov["unit"])
	assert.Len(t, ov["timestamps"], 12)
	assert.Equal(t, "2026-09-01", ov["first_date"]) // first data day, not the (later) creation day
	assert.EqualValues(t, 3000, ov["total"].([]any)[0])
	s := ov["summary"].(obj)
	assert.InDelta(t, 3.0, s["energy_kwh"], 1e-9) // 1 kWh + 2 kWh
	assert.InDelta(t, 0.3, s["revenue"], 1e-9)
	assert.InDelta(t, 0.3, s["specific_yield_kwh_kwp"], 1e-9)
	assert.InDelta(t, 100.0, s["target_kwh"], 1e-9) // 3000 / 30 days
	assert.InDelta(t, 3.0, s["actual_pct"], 1e-9)
	invs := ov["inverters"].([]any)
	require.Len(t, invs, 2)
	assert.Equal(t, "MPP", invs[0].(obj)["status_text"])
	for _, inv := range invs { // pdc = pac * 1.04
		assert.InDelta(t, inv.(obj)["energy_kwh"].(float64)*1.04, inv.(obj)["dc_energy_kwh"], 0.01)
		assert.InDelta(t, 96.2, inv.(obj)["efficiency_pct"], 1e-9)
	}

	owner.do("GET", "/systems/"+sid+"/overview?period=month&date=2026-09-15", nil, 200, &ov)
	assert.Equal(t, "kWh", ov["unit"])
	assert.Len(t, ov["timestamps"], 30)
	assert.InDelta(t, 3.0, ov["total"].([]any)[0], 1e-9)
	assert.InDelta(t, 100.0, ov["target"].([]any)[0], 1e-9)

	// daily backfill; lower values never overwrite measured energy
	device.do("POST", "/systems/"+sid+"/ingest/daily", obj{"days": []obj{
		{"inverter": "1", "date": "2026-08-31", "energy_wh": 5000},
		{"inverter": "1", "date": "2026-09-01", "energy_wh": 10},
	}}, 200, nil)
	owner.do("GET", "/systems/"+sid+"/overview?period=year&date=2026-09-15", nil, 200, &ov)
	total := ov["total"].([]any)
	assert.InDelta(t, 5.0, total[7], 1e-9)
	assert.InDelta(t, 3.0, total[8], 1e-9)
	assert.InDelta(t, 8.0, ov["summary"].(obj)["lifetime_energy_kwh"], 1e-9)
	assert.Equal(t, "2026-08-31", ov["first_date"])

	// status change creates events
	device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": []obj{
		{"inverter": "1", "time": day.Add(20 * time.Hour).Format(time.RFC3339), "pac_w": 0, "status_code": 1, "status_text": "Standby", "error_code": 2, "error_text": "Isolationsfehler"},
	}}, 200, nil)
	var evs obj
	owner.do("GET", "/systems/"+sid+"/events?errors_only=true", nil, 200, &evs)
	items := evs["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "Isolationsfehler", items[0].(obj)["error_text"])
	owner.do("GET", "/systems/"+sid+"/events", nil, 200, &evs)
	assert.Len(t, evs["items"], 3) // MPP x2 inverters, then Standby

	var dash obj
	owner.do("GET", "/dashboard", nil, 200, &dash) // superadmin: every system in the database
	ids := map[string]bool{}
	for _, d := range dash["systems"].([]any) {
		ids[d.(obj)["system_id"].(string)] = true
	}
	assert.True(t, ids[sid] && ids[other["id"].(string)])

	// revoked keys stop working
	owner.do("DELETE", "/systems/"+sid+"/api-keys/"+key["api_key"].(obj)["id"].(string), nil, 204, nil)
	device.do("POST", "/systems/"+sid+"/ingest/measurements", body, 401, nil)
}

func TestSolarLogDataSource(t *testing.T) {
	e := newEnv(t)
	email := uniq("super")
	e.createUser(email, "super-password-1", true)
	owner := e.login(email, "super-password-1")
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Solar-Log"}, 201, &sys)
	sid := sys["id"].(string)

	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_dir", "name": "dir", "config": obj{"directory": "testdata"}}, 400, nil)
	var ds obj
	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_remote", "name": "ftp",
		"config": obj{"host": "ftp.example.com"}}, 400, nil) // protocol missing
	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_remote", "name": "ftp",
		"config": obj{"protocol": "ftps", "host": "ftp.example.com", "username": "u", "password": "secret-value"}}, 201, &ds)
	cfg := ds["config"].(obj)
	assert.Equal(t, "ftps", cfg["protocol"])
	assert.Equal(t, true, cfg["has_password"])
	assert.NotContains(t, fmt.Sprint(ds), "secret-value")

	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_remote", "name": "sftp",
		"config": obj{"protocol": "sftp", "host": "sftp.example.com", "host_key": "not a key"}}, 400, nil)
	hostKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_remote", "name": "sftp",
		"config": obj{"protocol": "sftp", "host": "sftp.example.com", "port": 22, "username": "u", "password": "secret-value", "host_key": hostKey}}, 201, &ds)
	cfg = ds["config"].(obj)
	assert.Equal(t, "sftp", cfg["protocol"])
	assert.Equal(t, true, cfg["has_password"])
	assert.Equal(t, hostKey, cfg["host_key"])
	assert.NotContains(t, fmt.Sprint(ds), "secret-value")

	// switching to FTP keeps the password and drops the host key
	owner.do("PATCH", "/systems/"+sid+"/data-sources/"+ds["id"].(string), obj{"config": obj{"protocol": "ftp", "port": 21}}, 200, &ds)
	cfg = ds["config"].(obj)
	assert.Equal(t, "ftp", cfg["protocol"])
	assert.Equal(t, true, cfg["has_password"])
	assert.Nil(t, cfg["host_key"])
}

func mustLoc(t *testing.T) *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

// Opening an invitation link creates an account for the invited address, so
// only the invitee (by email) or a superadmin may get it.
func TestInvitationLinkOnlyByEmail(t *testing.T) {
	e := newEnv(t)
	superEmail := uniq("super")
	e.createUser(superEmail, "super-password-1", true)
	super := e.login(superEmail, "super-password-1")
	var sys obj
	super.do("POST", "/systems", obj{"name": "Link PV"}, 201, &sys)
	sid := sys["id"].(string)
	adminEmail := uniq("admin")
	adminID := e.createUser(adminEmail, "admin-password-1", false)
	require.NoError(t, e.Systems.UpsertMember(context.Background(), uuid.MustParse(sid), adminID, "admin"))
	admin := e.login(adminEmail, "admin-password-1")

	var inv obj
	admin.do("POST", "/systems/"+sid+"/invitations", obj{"email": uniq("mailed"), "role": "viewer"}, 201, &inv)
	assert.Equal(t, true, inv["email_sent"])
	assert.NotContains(t, inv, "token")
	assert.NotContains(t, inv, "accept_url")

	// The email can't be sent: an admin gets an error, and the unusable
	// invitation is revoked; a superadmin gets the link to share it.
	e.outbox.setFail(true)
	t.Cleanup(func() { e.outbox.setFail(false) })
	victim := uniq("victim")
	admin.do("POST", "/systems/"+sid+"/invitations", obj{"email": victim, "role": "viewer"}, 502, nil)
	var pending []obj
	admin.do("GET", "/systems/"+sid+"/invitations", nil, 200, &pending)
	for _, p := range pending {
		assert.NotEqual(t, victim, p["email"], "unsent invitation is not pending")
	}

	super.do("POST", "/systems/"+sid+"/invitations", obj{"email": uniq("shared"), "role": "viewer"}, 201, &inv)
	assert.Equal(t, false, inv["email_sent"])
	token := inv["token"].(string)
	assert.Equal(t, "http://app.test/invitations/"+token, inv["accept_url"])
	e.anon().do("GET", "/invitations/"+token, nil, 200, nil)
}
