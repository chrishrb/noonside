//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaneInverters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var sys, other obj
	owner.do("POST", "/systems", obj{"name": "Planes PV", "planes": []obj{
		{"name": "East", "kwp": 5, "tilt_deg": 30, "azimuth_deg": -90},
		{"name": "West", "kwp": 4, "tilt_deg": 30, "azimuth_deg": 90},
	}}, 201, &sys)
	owner.do("POST", "/systems", obj{"name": "Other PV", "planes": []obj{{"kwp": 1, "tilt_deg": 0, "azimuth_deg": 0}}}, 201, &other)
	sid := sys["id"].(string)
	base := "/systems/" + sid
	planeID := func(s obj, i int) string { return s["planes"].([]any)[i].(map[string]any)["id"].(string) }
	east, west, foreign := planeID(sys, 0), planeID(sys, 1), planeID(other, 0)

	// assign on create and update; null clears it
	var wr1, wr2 obj
	owner.do("POST", base+"/inverters", obj{"external_ref": "1", "name": "WR 1", "plane_id": east}, 201, &wr1)
	assert.Equal(t, east, wr1["plane_id"])
	owner.do("POST", base+"/inverters", obj{"external_ref": "2", "name": "WR 2"}, 201, &wr2)
	assert.Nil(t, wr2["plane_id"])
	wr1Path, wr2Path := base+"/inverters/"+wr1["id"].(string), base+"/inverters/"+wr2["id"].(string)
	planeOf := func(inv obj) any {
		var invs []obj
		owner.do("GET", base+"/inverters", nil, 200, &invs)
		for _, i := range invs {
			if i["id"] == inv["id"] {
				return i["plane_id"]
			}
		}
		t.Fatalf("inverter %v not listed", inv["id"])
		return nil
	}
	owner.do("PATCH", wr2Path, obj{"plane_id": west}, 200, &wr2)
	assert.Equal(t, west, wr2["plane_id"])
	owner.do("PATCH", wr2Path, obj{"name": "WR 2b"}, 200, &wr2)
	assert.Equal(t, west, wr2["plane_id"], "a patch without plane_id keeps it")
	var cleared obj // a fresh map: decoding into wr1 would keep its old plane_id
	owner.do("PATCH", wr1Path, obj{"plane_id": nil}, 200, &cleared)
	assert.Nil(t, cleared["plane_id"])
	assert.Nil(t, planeOf(wr1))
	owner.do("PATCH", wr1Path, obj{"plane_id": east}, 200, nil)

	// a plane of another system or an unknown plane is refused
	owner.do("PATCH", wr1Path, obj{"plane_id": foreign}, 400, nil)
	owner.do("POST", base+"/inverters", obj{"external_ref": "3", "name": "WR 3", "plane_id": uuid.NewString()}, 400, nil)
	_, err := e.db.Pool().Exec(ctx, `UPDATE inverters SET plane_id = $1 WHERE id = $2`, foreign, wr1["id"])
	require.Error(t, err)

	// saving the planes with their ids keeps them and their inverters
	var updated obj
	owner.do("PATCH", base, obj{"planes": []obj{
		{"id": west, "name": "West roof", "kwp": 4.5, "tilt_deg": 25, "azimuth_deg": 90},
		{"id": east, "name": "East", "kwp": 5, "tilt_deg": 30, "azimuth_deg": -90},
		{"name": "Garage", "kwp": 2, "tilt_deg": 10, "azimuth_deg": 0},
	}}, 200, &updated)
	require.Len(t, updated["planes"], 3)
	assert.Equal(t, west, planeID(updated, 0))
	assert.Equal(t, "West roof", updated["planes"].([]any)[0].(map[string]any)["name"])
	assert.Equal(t, east, planeID(updated, 1))
	assert.Equal(t, west, planeOf(wr2))

	// unknown or foreign plane ids are refused, duplicates too
	owner.do("PATCH", base, obj{"planes": []obj{{"id": uuid.NewString(), "kwp": 1, "tilt_deg": 0, "azimuth_deg": 0}}}, 400, nil)
	owner.do("PATCH", base, obj{"planes": []obj{{"id": foreign, "kwp": 1, "tilt_deg": 0, "azimuth_deg": 0}}}, 400, nil)
	owner.do("PATCH", base, obj{"planes": []obj{
		{"id": east, "kwp": 1, "tilt_deg": 0, "azimuth_deg": 0}, {"id": east, "kwp": 1, "tilt_deg": 0, "azimuth_deg": 0},
	}}, 400, nil)

	// dropping a plane unassigns its inverters
	owner.do("PATCH", base, obj{"planes": []obj{{"id": east, "name": "East", "kwp": 5, "tilt_deg": 30, "azimuth_deg": -90}}}, 200, nil)
	assert.Nil(t, planeOf(wr2))
	assert.Equal(t, east, planeOf(wr1))

	// viewers can't assign
	viewerEmail := uniq("viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	require.NoError(t, e.Systems.UpsertMember(ctx, uuid.MustParse(sid), viewerID, auth.RoleViewer))
	viewer := e.login(viewerEmail, "viewer-password-1")
	viewer.do("PATCH", wr1Path, obj{"plane_id": nil}, 403, nil)
}
