package hostedftp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePassword(t *testing.T) {
	for range 50 {
		pw, err := GeneratePassword()
		require.NoError(t, err)
		assert.Len(t, pw, 12, "the Solar-Log rejects longer passwords")
		assert.Regexp(t, `^[a-zA-Z0-9]{5}-[a-zA-Z0-9]{6}$`, pw, "only letters, digits and one hyphen")
		assert.True(t, strings.ContainsAny(pw, lower), pw)
		assert.True(t, strings.ContainsAny(pw, upper), pw)
		assert.True(t, strings.ContainsAny(pw, digits), pw)
	}
}

func TestGenerateUsername(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		u, err := GenerateUsername()
		require.NoError(t, err)
		assert.Regexp(t, `^pv[a-z2-7]{8}$`, u)
		assert.False(t, seen[u])
		seen[u] = true
	}
}

func TestHookToken(t *testing.T) {
	tok := HookToken("secret")
	assert.Len(t, tok, 64)
	assert.Equal(t, tok, HookToken("secret"), "stable")
	assert.NotEqual(t, tok, HookToken("other"))

	s := NewService(nil, Settings{HookToken: tok})
	assert.True(t, s.ValidHookToken(tok))
	assert.False(t, s.ValidHookToken(tok[:63]))
	assert.False(t, s.ValidHookToken(""))
	assert.False(t, NewService(nil, Settings{}).ValidHookToken(""), "no token configured")
}

// fakeSFTPGo records the requests to a minimal SFTPGo REST API.
type fakeSFTPGo struct {
	*httptest.Server
	mu       sync.Mutex
	tokens   int
	requests []string          // "METHOD path"
	bodies   map[string][]byte // last body per "METHOD path"
	users    map[string]map[string]any
	objects  map[string]bool // existing event actions/rules by path
	expire   bool            // reject the current token once
}

func newFake(t *testing.T) *fakeSFTPGo {
	f := &fakeSFTPGo{bodies: map[string][]byte{}, users: map[string]map[string]any{}, objects: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSFTPGo) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.requests = append(f.requests, key)
	f.bodies[key] = body
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if r.URL.Path == "/api/v2/token" {
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw" {
			reply(401, map[string]string{"error": "bad credentials"})
			return
		}
		f.tokens++
		reply(200, map[string]any{"access_token": "tok" + string(rune('0'+f.tokens)), "expires_at": time.Now().Add(20 * time.Minute)})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok") || f.expire {
		f.expire = false
		reply(401, map[string]string{"error": "invalid token"})
		return
	}
	switch path := strings.TrimPrefix(r.URL.Path, "/api/v2"); {
	case path == "/users" && r.Method == http.MethodPost:
		var u map[string]any
		_ = json.Unmarshal(body, &u)
		f.users[u["username"].(string)] = u
		reply(201, u)
	case path == "/users" && r.Method == http.MethodGet:
		list := []map[string]any{}
		if r.URL.Query().Get("offset") == "0" {
			for _, u := range f.users {
				list = append(list, map[string]any{"username": u["username"], "description": u["description"]})
			}
		}
		reply(200, list)
	case strings.HasPrefix(path, "/users/"):
		name := strings.TrimPrefix(path, "/users/")
		u, ok := f.users[name]
		if !ok {
			reply(404, map[string]string{"error": "not found"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			out := map[string]any{}
			for k, v := range u {
				if k != "password" {
					out[k] = v
				}
			}
			reply(200, out)
		case http.MethodPut:
			var nu map[string]any
			_ = json.Unmarshal(body, &nu)
			f.users[name] = nu
			reply(200, map[string]string{"message": "User updated"})
		case http.MethodDelete:
			delete(f.users, name)
			reply(200, map[string]string{"message": "User deleted"})
		}
	case r.Method == http.MethodPut:
		if !f.objects[path] {
			reply(404, map[string]string{"error": "not found"})
			return
		}
		reply(200, map[string]string{"message": "updated"})
	case r.Method == http.MethodPost:
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		f.objects[path+"/"+obj["name"].(string)] = true
		reply(201, obj)
	default:
		reply(404, map[string]string{"error": "not found"})
	}
}

func TestClientUsers(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)

	_, err := NewClient(f.URL, "admin", "")
	require.Error(t, err, "password required")

	bad, err := NewClient(f.URL, "admin", "wrong")
	require.NoError(t, err)
	_, err = bad.ListUsers(ctx)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 401, apiErr.Status)

	c, err := NewClient(f.URL+"/api/v2/", "admin", "pw")
	require.NoError(t, err)
	require.NoError(t, c.CreateUser(ctx, NewUser{
		Username: "pvabc", Password: "Secret-Passw0rd", HomeDir: "/srv/sftpgo/data/s/d",
		QuotaBytes: 200 << 20, MaxFileSize: 20 << 20, Description: "data-source=x",
	}))
	assert.JSONEq(t, `{
		"status": 1, "username": "pvabc", "password": "Secret-Passw0rd", "description": "data-source=x",
		"home_dir": "/srv/sftpgo/data/s/d", "quota_size": 209715200,
		"permissions": {"/": ["list", "download", "upload", "overwrite", "delete", "create_dirs", "rename"]},
		"filesystem": {"provider": 0},
		"filters": {"denied_protocols": ["DAV", "HTTP"], "max_upload_file_size": 20971520}
	}`, string(f.bodies["POST /api/v2/users"]))

	u, err := c.GetUser(ctx, "pvabc")
	require.NoError(t, err)
	assert.Equal(t, "data-source=x", u.Description)
	assert.EqualValues(t, 200<<20, u.QuotaSize)

	require.NoError(t, c.UpdatePassword(ctx, "pvabc", "Other-Passw0rd"))
	assert.Equal(t, "Other-Passw0rd", f.users["pvabc"]["password"])
	assert.Equal(t, "/srv/sftpgo/data/s/d", f.users["pvabc"]["home_dir"], "the rest of the user is sent back")
	assert.Contains(t, f.requests, "PUT /api/v2/users/pvabc")

	// An expired token is renewed once.
	f.expire = true
	users, err := c.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "pvabc", users[0].Username)
	assert.Equal(t, 2, f.tokens)

	require.NoError(t, c.DeleteUser(ctx, "pvabc"))
	require.NoError(t, c.DeleteUser(ctx, "pvabc"), "missing is fine")
	_, err = c.GetUser(ctx, "pvabc")
	assert.True(t, IsNotFound(err))
	assert.Equal(t, 2, f.tokens, "the token is cached")
}

func TestEnsureEventRule(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	c, err := NewClient(f.URL, "admin", "pw")
	require.NoError(t, err)
	hook := Hook{Name: "noonside-upload", URL: "http://api:8080/api/v1/hooks/hosted-ftp", Token: "t0k"}

	require.NoError(t, c.EnsureEventRule(ctx, hook))
	assert.Equal(t, []string{
		"GET /api/v2/token",
		"PUT /api/v2/eventactions/noonside-upload", "POST /api/v2/eventactions",
		"PUT /api/v2/eventrules/noonside-upload", "POST /api/v2/eventrules",
	}, f.requests, "created when missing")
	assert.JSONEq(t, `{
		"name": "noonside-upload", "description": "noonside: import hosted FTP uploads", "type": 1,
		"options": {"http_config": {
			"endpoint": "http://api:8080/api/v1/hooks/hosted-ftp", "method": "POST", "timeout": 20,
			"headers": [{"key": "Authorization", "value": "Bearer t0k"}, {"key": "Content-Type", "value": "application/json"}],
			"body": "{\"username\":\"{{Name}}\",\"path\":\"{{VirtualPath}}\",\"event\":\"{{Event}}\"}"
		}}
	}`, string(f.bodies["POST /api/v2/eventactions"]))
	assert.JSONEq(t, `{
		"name": "noonside-upload", "description": "noonside: report uploads to the API", "status": 1, "trigger": 1,
		"conditions": {"fs_events": ["upload"]},
		"actions": [{"name": "noonside-upload", "order": 1}]
	}`, string(f.bodies["POST /api/v2/eventrules"]))

	f.requests = nil
	hook.Users = []string{"pvtest*"}
	require.NoError(t, c.EnsureEventRule(ctx, hook))
	assert.Equal(t, []string{"PUT /api/v2/eventactions/noonside-upload", "PUT /api/v2/eventrules/noonside-upload"}, f.requests, "updated when present")
	var rule map[string]any
	require.NoError(t, json.Unmarshal(f.bodies["PUT /api/v2/eventrules/noonside-upload"], &rule))
	assert.Equal(t, map[string]any{"names": []any{map[string]any{"pattern": "pvtest*"}}}, rule["conditions"].(map[string]any)["options"])
}

// fakeAPI is an in-memory API for the service tests.
type fakeAPI struct {
	users map[string]User
}

func (a *fakeAPI) CreateUser(_ context.Context, u NewUser) error {
	a.users[u.Username] = User{Username: u.Username, Description: u.Description, HomeDir: u.HomeDir, QuotaSize: u.QuotaBytes}
	return nil
}

func (a *fakeAPI) GetUser(_ context.Context, name string) (*User, error) {
	u, ok := a.users[name]
	if !ok {
		return nil, &APIError{Status: 404}
	}
	return &u, nil
}

func (a *fakeAPI) UpdatePassword(context.Context, string, string) error { return nil }

func (a *fakeAPI) DeleteUser(_ context.Context, name string) error {
	delete(a.users, name)
	return nil
}

func (a *fakeAPI) ListUsers(context.Context) ([]User, error) {
	var out []User
	for _, u := range a.users {
		out = append(out, u)
	}
	return out, nil
}

func (a *fakeAPI) EnsureEventRule(context.Context, Hook) error { return errors.New("unreachable") }

func TestProvision(t *testing.T) {
	api := &fakeAPI{users: map[string]User{}}
	s := NewService(api, Settings{HomeBase: "/srv/sftpgo/data", SFTPAddr: "sftpgo:2022", QuotaBytes: 1 << 20})
	var scanned string
	s.scan = func(_ context.Context, host string, port int, allowPrivate bool) (string, error) {
		scanned = host
		assert.Equal(t, 2022, port)
		assert.True(t, allowPrivate, "our own server")
		return "ssh-ed25519 AAAA", nil
	}
	sys, src := uuid.New(), uuid.New()
	acc, err := s.Provision(context.Background(), sys, src)
	require.NoError(t, err)
	assert.Equal(t, "sftpgo", scanned)
	assert.Equal(t, "ssh-ed25519 AAAA", acc.HostKey)
	assert.Len(t, acc.Password, 12)
	u := api.users[acc.Username]
	assert.Equal(t, "/srv/sftpgo/data/"+sys.String()+"/"+src.String(), u.HomeDir)
	assert.Equal(t, "data-source="+src.String(), u.Description)
	assert.EqualValues(t, 1<<20, u.QuotaSize)

	// A configured host key wins and is not scanned.
	pinned := NewService(api, Settings{HomeBase: "/x", SFTPAddr: "sftpgo:2022", HostKey: "ssh-ed25519 BBBB"})
	pinned.scan = func(context.Context, string, int, bool) (string, error) { return "", errors.New("must not scan") }
	acc, err = pinned.Provision(context.Background(), sys, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 BBBB", acc.HostKey)

	require.NoError(t, s.Remove(context.Background(), Account{Username: "pvmissing"}), "a missing account is no error")
}

func TestCheck(t *testing.T) {
	s := NewService(nil, Settings{QuotaBytes: 100})
	now := time.Now()
	used := func(n int64) func(context.Context) (int64, error) {
		return func(context.Context) (int64, error) { return n, nil }
	}
	st, err := s.Check(context.Background(), State{}, used(40), now)
	require.NoError(t, err)
	assert.Equal(t, State{UsedBytes: 40, CheckedAt: now}, st)

	st, err = s.Check(context.Background(), st, used(90), now)
	require.ErrorIs(t, err, ErrQuota)
	assert.True(t, st.Locked)

	st, err = s.Check(context.Background(), st, used(10), now)
	require.NoError(t, err)
	assert.False(t, st.Locked, "unlocked once housekeeping freed space")

	prev := State{UsedBytes: 5}
	st, err = s.Check(context.Background(), prev, func(context.Context) (int64, error) { return 0, errors.New("list") }, now)
	require.Error(t, err)
	assert.Equal(t, prev, st, "a failed clean-up keeps the state")
}

type fakeGCStore struct{ ids []uuid.UUID }

func (f fakeGCStore) TryLockJob(context.Context, string, time.Duration, time.Duration) (func(), bool, error) {
	return func() {}, true, nil
}

func (f fakeGCStore) DataSourceIDsByKind(context.Context, string) ([]uuid.UUID, error) {
	return f.ids, nil
}

func TestGCSkipsUnmanagedAndKnown(t *testing.T) {
	known := uuid.New()
	api := &fakeAPI{users: map[string]User{
		"pvknown": {Username: "pvknown", Description: descPrefix + known.String()},
		"admin2":  {Username: "admin2", Description: "someone else's"},
		"pvbad":   {Username: "pvbad", Description: descPrefix + "not-a-uuid"},
	}}
	s := NewService(api, Settings{})
	// The hook setup fails (Warn only) and nothing is an orphan.
	require.NoError(t, s.GC(fakeGCStore{ids: []uuid.UUID{known}})(context.Background(), time.Now().Add(time.Minute)))
	assert.Len(t, api.users, 3)
}
