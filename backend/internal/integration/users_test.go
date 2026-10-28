//go:build integration

package integration

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8))))
	return b.Bytes()
}

// mediaFile returns the stored file behind a /media/ URL.
func (e *env) mediaFile(url string) string {
	return filepath.Join(e.mediaRoot, filepath.FromSlash(strings.TrimPrefix(url, "/media/")))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestAdminEditUsers(t *testing.T) {
	e := newEnv(t)
	superEmail := uniq("super")
	superID := e.createUser(superEmail, "super-password-1", true)
	super := e.login(superEmail, "super-password-1")
	plainEmail := uniq("plain")
	plainID := e.createUser(plainEmail, "plain-password-1", false)
	plain := e.login(plainEmail, "plain-password-1")
	pid := "/admin/users/" + plainID.String()

	// superadmin only
	plain.do("GET", pid, nil, 403, nil)
	plain.do("PATCH", pid, obj{"name": "X"}, 403, nil)
	plain.do("DELETE", pid, nil, 403, nil)
	plain.do("POST", pid+"/password-reset", nil, 403, nil)
	plain.do("PUT", pid+"/avatar", testPNG(t), 403, nil)

	var u obj
	super.do("GET", pid, nil, 200, &u)
	assert.Equal(t, plainEmail, u["email"])
	assert.Equal(t, false, u["disabled"])
	super.do("GET", "/admin/users/"+uuid.NewString(), nil, 404, nil)

	// rename
	super.do("PATCH", pid, obj{"name": " Renamed User "}, 200, &u)
	assert.Equal(t, "Renamed User", u["name"])
	super.do("PATCH", pid, obj{"name": "  "}, 400, nil)

	// change the email: the name survives the profile trigger, the new
	// address signs in without confirmation
	newEmail := uniq("moved")
	super.do("PATCH", pid, obj{"email": strings.ToUpper(newEmail)}, 200, &u)
	assert.Equal(t, newEmail, u["email"])
	assert.Equal(t, "Renamed User", u["name"])
	var me obj
	e.login(newEmail, "plain-password-1").do("GET", "/me", nil, 200, &me)
	assert.Equal(t, "Renamed User", me["name"])
	super.do("PATCH", pid, obj{"email": superEmail}, 409, nil)

	// renaming yourself also survives a later email change
	self := e.login(newEmail, "plain-password-1")
	self.do("PATCH", "/me", obj{"name": "Own Name"}, 200, nil)
	other := uniq("moved-again")
	super.do("PATCH", pid, obj{"email": other}, 200, &u)
	assert.Equal(t, "Own Name", u["name"])

	// disable and enable
	super.do("PATCH", pid, obj{"disabled": true}, 200, &u)
	assert.Equal(t, true, u["disabled"])
	status, _ := signIn(other, "plain-password-1")
	assert.Equal(t, 400, status, "a disabled account cannot sign in")
	super.do("PATCH", pid, obj{"disabled": false}, 200, &u)
	assert.Equal(t, false, u["disabled"])
	e.login(other, "plain-password-1")
	super.do("PATCH", "/admin/users/"+superID.String(), obj{"disabled": true}, 400, nil)

	// password reset link
	super.do("POST", pid+"/password-reset", nil, 204, nil)

	// avatar
	super.do("PUT", pid+"/avatar", testPNG(t), 200, &u)
	require.NotEmpty(t, u["avatar_url"])
	assert.True(t, exists(e.mediaFile(u["avatar_url"].(string))))
	var cleared obj
	super.do("DELETE", pid+"/avatar", nil, 200, &cleared)
	assert.Nil(t, cleared["avatar_url"])

	// delete: not yourself, not the only owner of a system
	super.do("DELETE", "/admin/users/"+superID.String(), nil, 400, nil)
	sys, err := e.Systems.CreateSystem(t.Context(), &systems.System{Name: "Only Owner PV", Timezone: "Europe/Berlin", Currency: "EUR"})
	require.NoError(t, err)
	require.NoError(t, e.Systems.UpsertMember(t.Context(), sys.ID, plainID, auth.RoleOwner))
	status, raw := super.raw("DELETE", pid, nil)
	assert.Equal(t, 409, status)
	assert.Contains(t, string(raw), "Only Owner PV")
	e.login(other, "plain-password-1").do("DELETE", "/systems/"+sys.ID.String(), nil, 204, nil)
	super.do("DELETE", pid, nil, 204, nil)
	super.do("GET", pid, nil, 404, nil)
	status, _ = signIn(other, "plain-password-1")
	assert.Equal(t, 400, status)
}

func TestPictures(t *testing.T) {
	e := newEnv(t)
	ownerEmail := uniq("owner")
	ownerID := e.createUser(ownerEmail, "owner-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	viewerEmail := uniq("viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	viewer := e.login(viewerEmail, "viewer-password-1")
	strangerEmail := uniq("stranger")
	e.createUser(strangerEmail, "stranger-password-1", false)
	stranger := e.login(strangerEmail, "stranger-password-1")

	var me obj
	owner.do("GET", "/me", nil, 200, &me)
	assert.Equal(t, true, me["features"].(obj)["uploads"])

	// own avatar: replacing deletes the old file
	owner.do("PUT", "/me/avatar", testPNG(t), 200, &me)
	first := me["avatar_url"].(string)
	assert.True(t, strings.HasPrefix(first, "/media/avatars/"))
	assert.True(t, exists(e.mediaFile(first)))
	owner.do("PUT", "/me/avatar", testPNG(t), 200, &me)
	second := me["avatar_url"].(string)
	assert.NotEqual(t, first, second)
	assert.False(t, exists(e.mediaFile(first)))

	// served with an immutable cache header
	resp, err := http.Get(e.server.URL + second)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, testPNG(t), body)
	assert.Contains(t, resp.Header.Get("Cache-Control"), "immutable")

	// invalid uploads
	owner.do("PUT", "/me/avatar", []byte("not an image"), 400, nil)
	owner.do("PUT", "/me/avatar", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 400, nil)
	owner.do("PUT", "/me/avatar", append(testPNG(t), make([]byte, 3<<20)...), 400, nil)

	var cleared obj
	owner.do("DELETE", "/me/avatar", nil, 200, &cleared)
	assert.Nil(t, cleared["avatar_url"])
	assert.False(t, exists(e.mediaFile(second)))

	// system picture: system:write only, non-members get 404
	created, err := e.Systems.CreateSystem(t.Context(), &systems.System{Name: "Picture PV", Timezone: "Europe/Berlin", Currency: "EUR"})
	require.NoError(t, err)
	sid := created.ID.String()
	require.NoError(t, e.Systems.UpsertMember(t.Context(), created.ID, ownerID, auth.RoleOwner))
	require.NoError(t, e.Systems.UpsertMember(t.Context(), created.ID, viewerID, auth.RoleViewer))
	var sys obj
	viewer.do("PUT", "/systems/"+sid+"/image", testPNG(t), 403, nil)
	stranger.do("PUT", "/systems/"+sid+"/image", testPNG(t), 404, nil)
	owner.do("PUT", "/systems/"+sid+"/image", testPNG(t), 200, &sys)
	img := sys["image_url"].(string)
	assert.True(t, strings.HasPrefix(img, "/media/systems/"+sid+"/"))

	// the picture shows on the dashboard, in /me and for members
	var dash obj
	viewer.do("GET", "/dashboard", nil, 200, &dash)
	assert.Equal(t, img, dash["systems"].([]any)[0].(obj)["image_url"])
	owner.do("GET", "/me", nil, 200, &me)
	assert.Equal(t, img, me["systems"].([]any)[0].(obj)["image_url"])

	// deleting the system removes the file
	owner.do("DELETE", "/systems/"+sid, nil, 204, nil)
	assert.False(t, exists(e.mediaFile(img)))
}
