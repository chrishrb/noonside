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

func TestAdminSystems(t *testing.T) {
	e := newEnv(t)
	superEmail := uniq("admsys-super")
	superID := e.createUser(superEmail, "super-password-1", true)
	super := e.login(superEmail, "super-password-1")
	ownerEmail := uniq("admsys-owner")
	ownerID := e.createUser(ownerEmail, "owner-password-1", false)
	viewerEmail := uniq("admsys-viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	viewer := e.login(viewerEmail, "viewer-password-1")

	var sys obj
	super.do("POST", "/systems", obj{"name": "Admin list PV"}, 201, &sys)
	sid := sys["id"].(string)
	super.do("POST", "/systems/"+sid+"/inverters", obj{"external_ref": "1", "name": "WR 1"}, 201, nil)
	super.do("POST", "/systems/"+sid+"/inverters", obj{"external_ref": "2", "name": "WR 2"}, 201, nil)
	ctx := context.Background()
	require.NoError(t, e.Systems.UpsertMember(ctx, uuid.MustParse(sid), ownerID, auth.RoleOwner))
	require.NoError(t, e.Systems.UpsertMember(ctx, uuid.MustParse(sid), viewerID, auth.RoleViewer))

	viewer.do("GET", "/admin/systems", nil, 403, nil)

	var list []obj
	super.do("GET", "/admin/systems", nil, 200, &list)
	var got obj
	for _, a := range list {
		if a["system"].(obj)["id"] == sid {
			got = a
		}
	}
	require.NotNil(t, got, "system missing from the admin list")
	assert.EqualValues(t, 2, got["inverter_count"])
	assert.EqualValues(t, 3, got["member_count"]) // creator, owner, viewer
	var owners []string
	for _, o := range got["owners"].([]any) {
		owners = append(owners, o.(obj)["user_id"].(string))
	}
	assert.ElementsMatch(t, []string{superID.String(), ownerID.String()}, owners)
}
