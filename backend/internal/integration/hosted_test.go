//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/jlaffaye/ftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHostedFTP creates an upload account on SFTPGo (compose.yaml), lets
// the "device" upload the Solar-Log test export over FTP, reports the upload
// as SFTPGo's event rule would and imports it.
func TestHostedFTP(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ownerEmail := uniq("hosted-owner")
	ownerID := e.createUser(ownerEmail, "owner-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	viewerEmail := uniq("hosted-viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	viewer := e.login(viewerEmail, "viewer-password-1")
	strangerEmail := uniq("hosted-stranger")
	e.createUser(strangerEmail, "stranger-password-1", false)
	stranger := e.login(strangerEmail, "stranger-password-1")

	var me obj
	owner.do("GET", "/me", nil, 200, &me)
	assert.Equal(t, true, me["features"].(obj)["hosted_ftp"])

	sid := e.systemOwnedBy(ownerID, "Hosted")
	require.NoError(t, e.Systems.UpsertMember(ctx, uuid.MustParse(sid), viewerID, auth.RoleViewer))
	create := obj{"kind": "solarlog_hosted", "name": "Solar-Log", "config": obj{}}
	viewer.do("POST", "/systems/"+sid+"/data-sources", create, 403, nil)
	stranger.do("POST", "/systems/"+sid+"/data-sources", create, 404, nil)

	var ds obj
	owner.do("POST", "/systems/"+sid+"/data-sources", create, 201, &ds)
	dsID := ds["id"].(string)
	up := ds["upload"].(obj)
	username := up["username"].(string)
	password := up["password"].(string)
	assert.Regexp(t, `^pv[a-z2-7]{8}$`, username)
	assert.Regexp(t, `^[a-zA-Z0-9]{5}-[a-zA-Z0-9]{6}$`, password)
	assert.Equal(t, "127.0.0.1", up["host"])
	assert.EqualValues(t, 2121, up["port"])
	assert.Equal(t, "/", up["directory"])
	assert.EqualValues(t, 2<<20, up["quota_bytes"])
	assert.EqualValues(t, 14, up["retention_days"])
	assert.Nil(t, up["ready"])
	assert.Equal(t, true, ds["config"].(obj)["has_password"])
	assert.Nil(t, ds["config"].(obj)["host"], "connection details are in upload only")

	user, err := e.sftpgo.GetUser(ctx, username)
	require.NoError(t, err)
	assert.Equal(t, "data-source="+dsID, user.Description)
	assert.Equal(t, e.hosted.HomeDir(uuid.MustParse(sid), uuid.MustParse(dsID)), user.HomeDir)
	assert.EqualValues(t, 2<<20, user.QuotaSize)

	// The password is sealed in the database and never listed again.
	stored, err := e.DataSources.GetDataSource(ctx, uuid.MustParse(sid), uuid.MustParse(dsID))
	require.NoError(t, err)
	assert.NotContains(t, string(stored.Config), password)
	var list []obj
	owner.do("GET", "/systems/"+sid+"/data-sources", nil, 200, &list)
	require.Len(t, list, 1)
	assert.NotContains(t, fmt.Sprint(list), password)
	assert.Nil(t, list[0]["upload"].(obj)["password"])

	// One per system; the connection cannot be changed.
	owner.do("POST", "/systems/"+sid+"/data-sources", create, 409, nil)
	owner.do("PATCH", "/systems/"+sid+"/data-sources/"+dsID, obj{"config": obj{"host": "evil.example"}}, 400, nil)
	owner.do("PATCH", "/systems/"+sid+"/data-sources/"+dsID, obj{"name": "Dach", "config": obj{"backfill_history": true}}, 200, &ds)
	assert.Equal(t, "Dach", ds["name"])

	// The device uploads the export over FTP.
	src := "../modules/datasources/solarlog/testdata"
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	files := map[string][]byte{}
	for _, f := range entries {
		data, err := os.ReadFile(filepath.Join(src, f.Name()))
		require.NoError(t, err)
		files[f.Name()] = data
	}
	require.NoError(t, ftpUpload(username, password, files))

	// SFTPGo reports each upload to the hook (it can't reach this test
	// server, so the test does): only with the token, unknown users are
	// ignored.
	hook := e.anon()
	hook.do("POST", "/hooks/hosted-ftp", obj{"username": username, "path": "/days.js", "event": "upload"}, 401, nil)
	hook.token = "wrong"
	hook.do("POST", "/hooks/hosted-ftp", obj{"username": username, "path": "/days.js", "event": "upload"}, 401, nil)
	hook.token = e.hookToken
	hook.do("POST", "/hooks/hosted-ftp", obj{"username": "pvunknown", "path": "/x.js", "event": "upload"}, 204, nil)
	hook.do("POST", "/hooks/hosted-ftp", obj{"username": username, "path": "/days.js", "event": "upload"}, 204, nil)
	stored, err = e.DataSources.GetDataSource(ctx, uuid.MustParse(sid), uuid.MustParse(dsID))
	require.NoError(t, err)
	require.NotNil(t, stored.UploadAt, "the upload is noted")
	assert.WithinDuration(t, time.Now(), *stored.UploadAt, time.Minute)
	assert.Nil(t, stored.ImportedUploadAt)

	// The scheduler imports it (here: once, as the poll job would after the
	// quiet time) over SFTP.
	require.NoError(t, e.DataSources.RunOnce(ctx, *stored))
	imported, err := e.DataSources.GetDataSource(ctx, uuid.MustParse(sid), uuid.MustParse(dsID))
	require.NoError(t, err)
	require.NotNil(t, imported.ImportedUploadAt)
	assert.True(t, imported.ImportedUploadAt.Equal(*stored.UploadAt), "the upload read at the start counts as imported")
	owner.do("GET", "/systems/"+sid+"/data-sources", nil, 200, &list)
	ds = list[0]
	require.Equal(t, "", ds["last_error"])

	// base_vars.js fills the system, the inverters and the targets.
	var got obj
	owner.do("GET", "/systems/"+sid, nil, 200, &got)
	assert.InDelta(t, 78.36, got["peak_power_kwp"], 1e-9)
	assert.InDelta(t, 0.3914, got["tariff_per_kwh"], 1e-9)
	var ov obj
	owner.do("GET", "/systems/"+sid+"/overview?period=day&date=2024-10-11", nil, 200, &ov)
	assert.InDelta(t, 208.11, ov["summary"].(obj)["energy_kwh"], 0.01)
	assert.InDelta(t, 144.08, ov["summary"].(obj)["target_kwh"], 0.01)
	assert.Len(t, ov["inverters"], 11)
	// The test export is from 2024: imported minute files past the
	// retention are deleted.
	names := e.hostedFiles(username, password, imported.Config)
	assert.NotContains(t, names, "min241010.js")
	assert.Contains(t, names, "base_vars.js")
	used := ds["upload"].(obj)["used_bytes"].(float64)
	assert.Positive(t, used)
	assert.Equal(t, false, ds["upload"].(obj)["locked"])

	// Reset: a new password, shown once, works for the device and the
	// poller; the old one no longer does.
	viewer.do("POST", "/systems/"+sid+"/data-sources/"+dsID+"/reset-password", nil, 403, nil)
	owner.do("POST", "/systems/"+sid+"/data-sources/"+dsID+"/reset-password", nil, 200, &ds)
	newPassword := ds["upload"].(obj)["password"].(string)
	assert.NotEqual(t, password, newPassword)
	require.Error(t, ftpUpload(username, password, map[string][]byte{"days.js": files["days.js"]}))
	require.NoError(t, ftpUpload(username, newPassword, map[string][]byte{"days.js": files["days.js"]}))
	owner.do("POST", "/systems/"+sid+"/data-sources/"+dsID+"/run", nil, 200, &ds)
	require.Equal(t, "", ds["last_error"])
	// Re-importing (days.js again) is idempotent.
	var again obj
	owner.do("GET", "/systems/"+sid+"/overview?period=day&date=2024-10-11", nil, 200, &again)
	assert.Equal(t, ov["summary"], again["summary"])
	assert.Equal(t, ov["total"], again["total"])

	// Near the quota (2 MB) the run fails; SFTPGo refuses what goes beyond.
	require.NoError(t, ftpUpload(username, newPassword, map[string][]byte{"min_a.js": bytes.Repeat([]byte("x"), 1850<<10)}))
	require.Error(t, ftpUpload(username, newPassword, map[string][]byte{"min_b.js": bytes.Repeat([]byte("x"), 1<<20)}), "over quota")
	owner.do("POST", "/systems/"+sid+"/data-sources/"+dsID+"/run", nil, 200, &ds)
	assert.Contains(t, ds["last_error"], "upload quota exceeded")
	assert.Equal(t, true, ds["upload"].(obj)["locked"])

	// Deleting the source deletes the files and the account.
	viewer.do("DELETE", "/systems/"+sid+"/data-sources/"+dsID, nil, 403, nil)
	owner.do("DELETE", "/systems/"+sid+"/data-sources/"+dsID, nil, 204, nil)
	_, err = e.sftpgo.GetUser(ctx, username)
	assert.True(t, hostedftp.IsNotFound(err), "user deleted: %v", err)
	require.Error(t, ftpUpload(username, newPassword, map[string][]byte{"days.js": files["days.js"]}))
}

// TestHostedFTPOrphans checks that deleting a system closes its upload
// account, and that GC removes accounts without a data source.
func TestHostedFTPOrphans(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ownerEmail := uniq("hosted-gc")
	ownerID := e.createUser(ownerEmail, "owner-password-1", false)
	owner := e.login(ownerEmail, "owner-password-1")
	sid := e.systemOwnedBy(ownerID, "Hosted GC")
	var ds obj
	owner.do("POST", "/systems/"+sid+"/data-sources", obj{"kind": "solarlog_hosted", "name": "x", "config": obj{}}, 201, &ds)
	username := ds["upload"].(obj)["username"].(string)
	owner.do("DELETE", "/systems/"+sid, nil, 204, nil)
	_, err := e.sftpgo.GetUser(ctx, username)
	assert.True(t, hostedftp.IsNotFound(err), "closed with the system: %v", err)

	// An account left behind (e.g. SFTPGo was unreachable on delete), with
	// a file in it, and one noonside does not manage.
	orphan, err := hostedftp.GenerateUsername()
	require.NoError(t, err)
	require.NoError(t, e.sftpgo.CreateUser(ctx, hostedftp.NewUser{
		Username: orphan, Password: "Orphan-Passw0rd", HomeDir: "/srv/sftpgo/data/orphan/" + orphan,
		Description: "data-source=" + uuid.NewString(),
	}))
	require.NoError(t, ftpUpload(orphan, "Orphan-Passw0rd", map[string][]byte{"days.js": []byte("x")}))
	foreign := "other-" + orphan
	require.NoError(t, e.sftpgo.CreateUser(ctx, hostedftp.NewUser{Username: foreign, Password: "Foreign-Passw0rd", HomeDir: "/srv/sftpgo/data/other"}))
	t.Cleanup(func() { _ = e.sftpgo.DeleteUser(context.Background(), foreign) })

	_, err = e.db.Pool().Exec(ctx, `DELETE FROM job_leases WHERE name = 'hosted_ftp_gc'`)
	require.NoError(t, err)
	require.NoError(t, e.hosted.GC(e.DataSources)(ctx, time.Now().Add(time.Minute)))
	_, err = e.sftpgo.GetUser(ctx, orphan)
	assert.True(t, hostedftp.IsNotFound(err), "orphan deleted: %v", err)
	_, err = e.sftpgo.GetUser(ctx, foreign)
	assert.NoError(t, err, "unmanaged users stay")
}

// TestHostedFTPHookRule sets up SFTPGo's event rule (idempotently) under a
// name of its own, limited to a user that never uploads, so it does not
// interfere with a dev server's rule.
func TestHostedFTPHookRule(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	hook := hostedftp.Hook{Name: "noonside-test-" + uuid.NewString()[:8], URL: "http://host.docker.internal:1/hook", Token: e.hookToken, Users: []string{"nobody-uploads"}}
	t.Cleanup(func() { _ = e.sftpgo.DeleteEventRule(context.Background(), hook.Name) })
	require.NoError(t, e.sftpgo.EnsureEventRule(ctx, hook))
	require.NoError(t, e.sftpgo.EnsureEventRule(ctx, hook), "again: updated in place")
	require.NoError(t, e.sftpgo.DeleteEventRule(ctx, hook.Name))
}

// ftpUpload uploads files to SFTPGo's FTP server as a device would
// (passive mode, plain FTP in the local stack).
func ftpUpload(username, password string, files map[string][]byte) error {
	conn, err := ftp.Dial(net.JoinHostPort("127.0.0.1", strconv.Itoa(2121)), ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return err
	}
	defer conn.Quit() //nolint:errcheck
	if err := conn.Login(username, password); err != nil {
		return err
	}
	for name, data := range files {
		if err := conn.Stor(name, bytes.NewReader(data)); err != nil {
			return err
		}
	}
	return nil
}

// hostedFiles lists the account's directory over SFTP, as the poller sees it.
func (e *env) hostedFiles(username, password string, rawConfig []byte) []string {
	e.t.Helper()
	var cfg struct {
		HostKey string `json:"host_key"`
	}
	require.NoError(e.t, json.Unmarshal(rawConfig, &cfg))
	require.NotEmpty(e.t, cfg.HostKey, "pinned at creation")
	conn, err := e.hosted.Dial(context.Background(), hostedftp.Account{Username: username, Password: password, HostKey: cfg.HostKey})
	require.NoError(e.t, err)
	defer conn.Close() //nolint:errcheck
	names, err := conn.List(context.Background())
	require.NoError(e.t, err)
	return names
}

// systemOwnedBy creates a system (as a superadmin) owned by the user.
func (e *env) systemOwnedBy(ownerID uuid.UUID, name string) string {
	email := uniq("hosted-super")
	e.createUser(email, "super-password-1", true)
	var sys obj
	e.login(email, "super-password-1").do("POST", "/systems", obj{"name": name}, 201, &sys)
	sid := sys["id"].(string)
	require.NoError(e.t, e.Systems.UpsertMember(context.Background(), uuid.MustParse(sid), ownerID, auth.RoleOwner))
	return sid
}
