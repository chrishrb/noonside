//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyEnergyCorrections(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	user := func(prefix string) (uuid.UUID, *client) {
		email := uniq(prefix)
		id := e.createUser(email, "corr-password-1", false)
		return id, e.login(email, "corr-password-1")
	}
	ownerID, owner := user("corr-owner")
	adminID, admin := user("corr-admin")
	viewerID, viewer := user("corr-viewer")
	_, stranger := user("corr-stranger")

	sid := e.systemOwnedBy(ownerID, "Corrections PV")
	sysID := uuid.MustParse(sid)
	require.NoError(t, e.Systems.UpsertMember(ctx, sysID, adminID, auth.RoleAdmin))
	require.NoError(t, e.Systems.UpsertMember(ctx, sysID, viewerID, auth.RoleViewer))
	var inv1, inv2 obj
	owner.do("POST", "/systems/"+sid+"/inverters", obj{"external_ref": "1", "name": "WR 1", "peak_power_kwp": 5}, 201, &inv1)
	owner.do("POST", "/systems/"+sid+"/inverters", obj{"external_ref": "2", "name": "WR 2", "peak_power_kwp": 5}, 201, &inv2)
	id1, id2 := inv1["id"].(string), inv2["id"].(string)

	var key obj
	owner.do("POST", "/systems/"+sid+"/api-keys", obj{"name": "gateway", "scopes": []string{"data:ingest", "data:read"}}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}
	device.do("POST", "/systems/"+sid+"/ingest/daily", obj{"days": []obj{
		{"inverter": "1", "date": "2026-03-10", "energy_wh": 40000}, // wrong
		{"inverter": "2", "date": "2026-03-10", "energy_wh": 12000},
		{"inverter": "1", "date": "2026-03-11", "energy_wh": 11000},
	}}, 200, nil)

	day := "/systems/" + sid + "/days/2026-03-10/energy"
	fix := obj{"inverters": []obj{{"inverter_id": id1, "energy_kwh": 11.5}}}
	viewer.do("PUT", day, fix, 403, nil)
	stranger.do("PUT", day, fix, 404, nil)
	device.do("PUT", day, fix, 401, nil) // bearer only
	stranger.do("GET", day, nil, 404, nil)

	var got obj
	viewer.do("GET", day, nil, 200, &got)
	rows := got["inverters"].([]any)
	require.Len(t, rows, 2)
	assert.InDelta(t, 40.0, rows[0].(obj)["device_kwh"], 1e-9)
	assert.Nil(t, rows[0].(obj)["manual_kwh"])

	admin.do("PUT", day, fix, 200, &got)
	row := got["inverters"].([]any)[0].(obj)
	assert.InDelta(t, 40.0, row["device_kwh"], 1e-9)
	assert.InDelta(t, 11.5, row["manual_kwh"], 1e-9)
	assert.NotNil(t, row["manual_at"])

	// Every view uses the correction.
	var ov obj
	viewer.do("GET", "/systems/"+sid+"/overview?period=day&date=2026-03-10", nil, 200, &ov)
	assert.InDelta(t, 23.5, ov["summary"].(obj)["energy_kwh"], 1e-9)
	assert.Equal(t, true, ov["summary"].(obj)["corrected"])
	viewer.do("GET", "/systems/"+sid+"/overview?period=day&date=2026-03-11", nil, 200, &ov)
	assert.NotContains(t, ov["summary"].(obj), "corrected")
	viewer.do("GET", "/systems/"+sid+"/overview?period=month&date=2026-03-15", nil, 200, &ov)
	assert.InDelta(t, 23.5, ov["total"].([]any)[9], 1e-9)
	assert.Equal(t, true, ov["corrected"].([]any)[9])
	assert.Equal(t, false, ov["corrected"].([]any)[10])
	var de obj
	viewer.do("GET", "/systems/"+sid+"/daily-energy?from=2026-03-10&to=2026-03-11", nil, 200, &de)
	assert.InDelta(t, 23.5, de["days"].([]any)[0].(obj)["energy_kwh"], 1e-9)

	// Imports update the device value only.
	device.do("POST", "/systems/"+sid+"/ingest/daily", obj{"days": []obj{
		{"inverter": "1", "date": "2026-03-10", "energy_wh": 45000},
	}}, 200, nil)
	viewer.do("GET", day, nil, 200, &got)
	row = got["inverters"].([]any)[0].(obj)
	assert.InDelta(t, 45.0, row["device_kwh"], 1e-9)
	assert.InDelta(t, 11.5, row["manual_kwh"], 1e-9)

	// Resetting brings the device value back.
	owner.do("PUT", day, obj{"inverters": []obj{{"inverter_id": id1, "energy_kwh": nil}}}, 200, &got)
	assert.Nil(t, got["inverters"].([]any)[0].(obj)["manual_kwh"])
	viewer.do("GET", "/systems/"+sid+"/overview?period=day&date=2026-03-10", nil, 200, &ov)
	assert.InDelta(t, 57.0, ov["summary"].(obj)["energy_kwh"], 1e-9)
	assert.NotContains(t, ov["summary"].(obj), "corrected")

	// A day without data can be filled in, and reset leaves nothing behind.
	empty := "/systems/" + sid + "/days/2026-03-01/energy"
	owner.do("PUT", empty, obj{"inverters": []obj{{"inverter_id": id2, "energy_kwh": 3}}}, 200, &got)
	row = got["inverters"].([]any)[1].(obj)
	assert.Nil(t, row["device_kwh"])
	assert.InDelta(t, 3.0, row["manual_kwh"], 1e-9)
	owner.do("PUT", empty, obj{"inverters": []obj{{"inverter_id": id2, "energy_kwh": nil}}}, 200, nil)
	var n int
	require.NoError(t, e.db.Pool().QueryRow(ctx, `SELECT count(*) FROM inverter_daily WHERE inverter_id = $1 AND day = '2026-03-01'`, id2).Scan(&n))
	assert.Equal(t, 0, n)

	// Invalid input.
	otherSid := e.systemOwnedBy(ownerID, "Other PV")
	var other obj
	owner.do("POST", "/systems/"+otherSid+"/inverters", obj{"external_ref": "1", "name": "X"}, 201, &other)
	future := time.Now().AddDate(0, 0, 3).Format(time.DateOnly)
	for name, tc := range map[string]struct {
		path string
		body obj
	}{
		"future day":      {"/systems/" + sid + "/days/" + future + "/energy", fix},
		"negative":        {day, obj{"inverters": []obj{{"inverter_id": id1, "energy_kwh": -1}}}},
		"above kWp bound": {day, obj{"inverters": []obj{{"inverter_id": id1, "energy_kwh": 61}}}}, // 5 kWp × 12 kWh
		"other system":    {day, obj{"inverters": []obj{{"inverter_id": other["id"], "energy_kwh": 1}}}},
		"twice":           {day, obj{"inverters": []obj{{"inverter_id": id1, "energy_kwh": 1}, {"inverter_id": id1, "energy_kwh": 2}}}},
		"empty":           {day, obj{"inverters": []obj{}}},
	} {
		status, raw := owner.raw("PUT", tc.path, tc.body)
		assert.Equal(t, 400, status, "%s: %s", name, raw)
	}
}
