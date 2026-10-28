//go:build integration

// Package integration runs the API against the local stack (compose.yaml:
// Postgres, the Auth server GoTrue and SFTPGo).
// Run with `mise run test:integration`, which exports the TEST_* variables
// from `supabase status`.
package integration

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/reports"
	"github.com/chrishrb/noonside/internal/modules/systems/geocode"
	"github.com/chrishrb/noonside/internal/modules/systems/pvgis"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/chrishrb/noonside/internal/platform/supabase"
	"github.com/chrishrb/noonside/internal/router"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	gomail "github.com/wneessen/go-mail"
)

var (
	databaseURL string
	// supabaseURL is the public URL the tokens are issued for
	// (<supabaseURL>/auth/v1), authURL where GoTrue answers.
	supabaseURL string
	authURL     string
	secretKey   string
	// SFTPGo (hosted FTP): its REST API, admin password and SFTP address.
	sftpgoURL      string
	sftpgoPassword string
	sftpgoSFTPAddr string
	httpClient     = &http.Client{Timeout: 15 * time.Second}
)

// TestMain needs the local stack (compose.yaml: Postgres, GoTrue and
// SFTPGo); run via `mise run test:integration`, which exports the TEST_*
// variables.
func TestMain(m *testing.M) {
	databaseURL = os.Getenv("TEST_DATABASE_URL")
	supabaseURL = os.Getenv("TEST_SUPABASE_URL")
	authURL = os.Getenv("TEST_AUTH_URL")
	secretKey = os.Getenv("TEST_SUPABASE_SECRET_KEY")
	sftpgoURL = cmp.Or(os.Getenv("TEST_SFTPGO_URL"), "http://127.0.0.1:8090")
	sftpgoPassword = os.Getenv("TEST_SFTPGO_ADMIN_PASSWORD")
	sftpgoSFTPAddr = cmp.Or(os.Getenv("TEST_SFTPGO_SFTP_ADDR"), "127.0.0.1:2022")
	if databaseURL == "" || supabaseURL == "" || secretKey == "" || sftpgoPassword == "" {
		fmt.Fprintln(os.Stderr, "skipping integration tests: TEST_DATABASE_URL, TEST_SUPABASE_URL, TEST_SUPABASE_SECRET_KEY and TEST_SFTPGO_ADMIN_PASSWORD are required (see `mise run test:integration`)")
		os.Exit(0)
	}
	if authURL == "" {
		authURL = strings.TrimRight(supabaseURL, "/") + "/auth/v1"
	}
	os.Exit(m.Run())
}

type env struct {
	t *testing.T
	// Modules are the app's modules; db their database.
	*app.Modules
	db     *db.DB
	admin  *supabase.Admin
	server *httptest.Server
	outbox *outbox
	mailer *email.Mailer
	// weatherCalls counts requests to the fake Open-Meteo server.
	weatherCalls *atomic.Int32
	box          *secretbox.Box
	// notify posts to ntfy and webhooks (private hosts allowed for httptest).
	notify *notify.Client
	// pvoutput records the requests to the fake PVOutput server.
	pvoutput func() []hit
	// hosted runs the upload accounts on SFTPGo from compose.yaml; sftpgo is
	// its admin client, hookToken what SFTPGo sends to the upload hook.
	hosted    *hostedftp.Service
	sftpgo    *hostedftp.Client
	hookToken string
	// mediaRoot holds the uploaded pictures.
	mediaRoot string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	verifier, err := auth.NewVerifier(ctx, auth.VerifierConfig{URL: supabaseURL, AuthURL: authURL})
	require.NoError(t, err)
	admin, err := supabase.NewAdmin(authURL, secretKey)
	require.NoError(t, err)
	key, _ := secretbox.GenerateKey()
	box, _ := secretbox.New(key)
	pvo, pvoHits := targetServer(t)
	sftpgo, err := hostedftp.NewClient(sftpgoURL, "admin", sftpgoPassword)
	require.NoError(t, err)
	hookToken := hostedftp.HookToken(key)
	hosted := hostedftp.NewService(sftpgo, hostedftp.Settings{
		QuotaBytes: 2 << 20, Retention: 14 * 24 * time.Hour, HomeBase: "/srv/sftpgo/data",
		SFTPAddr: sftpgoSFTPAddr, PublicHost: "127.0.0.1", PublicPort: 2121,
		HookURL: "http://host.docker.internal:8080/api/v1/hooks/hosted-ftp", HookToken: hookToken,
	})
	renderer, err := email.NewRenderer(email.App{Name: "noonside", URL: "http://app.test"})
	require.NoError(t, err)
	outbox := &outbox{}
	mailer := email.NewWithSender(outbox, renderer, "noreply@app.test", "noonside")
	photon := httptest.NewServer(fakePhoton())
	t.Cleanup(photon.Close)
	weatherCalls := &atomic.Int32{}
	meteo := httptest.NewServer(fakeOpenMeteo(weatherCalls))
	t.Cleanup(meteo.Close)
	notifier := notify.New(true, "noonside-test")
	mediaRoot := t.TempDir()
	mediaDir, err := media.NewDir(mediaRoot)
	require.NoError(t, err)
	a, err := app.Build(d, app.Deps{
		Tokens: verifier, Admin: admin, Mailer: mailer, Box: box, Notify: notifier,
		Weather:           weather.NewClient(weather.Config{ForecastURL: meteo.URL, HistoricalURL: meteo.URL, ArchiveURL: meteo.URL}),
		Geocoder:          geocode.NewClient(photon.URL, "noonside-test"),
		Targets:           pvgis.NewClient(fakePVGIS(t).URL),
		Hosted:            hosted,
		Media:             mediaDir,
		SPA:               router.SPA{Media: mediaDir},
		AllowPrivateHosts: true,
		PVOutputURL:       pvo.URL,
		PublicURL:         "http://app.test",
		LandingURL:        "http://landing.test",
	})
	require.NoError(t, err)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return &env{t: t, Modules: a.Modules, db: d, server: srv, outbox: outbox, mailer: mailer, admin: admin, weatherCalls: weatherCalls, box: box, notify: notifier, pvoutput: pvoHits,
		hosted: hosted, sftpgo: sftpgo, hookToken: hookToken, mediaRoot: mediaRoot}
}

// outbox captures sent emails. With fail set, sending fails.
type outbox struct {
	mu   sync.Mutex
	msgs []string
	fail bool
}

func (o *outbox) setFail(fail bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fail = fail
}

func (o *outbox) Send(_ context.Context, m *gomail.Msg) error {
	o.mu.Lock()
	fail := o.fail
	o.mu.Unlock()
	if fail {
		return errors.New("smtp unavailable")
	}
	var b bytes.Buffer
	if _, err := m.WriteTo(&b); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.msgs = append(o.msgs, b.String())
	return nil
}

func (o *outbox) last() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.msgs) == 0 {
		return ""
	}
	return o.msgs[len(o.msgs)-1]
}

var invitationTokenRe = regexp.MustCompile(`/invitations/(s0i_[A-Za-z0-9_-]+)`)

// find returns the newest message containing s (soft line breaks removed),
// or "".
func (o *outbox) find(s string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.msgs) - 1; i >= 0; i-- {
		if m := strings.ReplaceAll(o.msgs[i], "=\r\n", ""); strings.Contains(m, s) {
			return m
		}
	}
	return ""
}

// invitationToken returns the token of the accept link in the last email.
func (o *outbox) invitationToken(t *testing.T) string {
	t.Helper()
	// Undo quoted-printable soft line breaks.
	body := strings.ReplaceAll(o.last(), "=\r\n", "")
	m := invitationTokenRe.FindStringSubmatch(body)
	require.NotNil(t, m, "no invitation link in the last email")
	return m[1]
}

// client is an API caller with a cookie jar and optional credentials.
type client struct {
	e      *env
	http   *http.Client
	token  string
	apiKey string
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

// createUser creates a confirmed Auth (GoTrue) user.
func (e *env) createUser(email, password string, superadmin bool) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	u, err := e.admin.CreateUser(ctx, supabase.CreateUserRequest{Email: email, Password: password, Name: email, EmailConfirmed: true})
	require.NoError(e.t, err)
	if superadmin {
		_, err = e.admin.SetAppMetadata(ctx, u.ID, map[string]any{auth.SuperadminClaim: true})
		require.NoError(e.t, err)
	}
	return u.ID
}

// signIn gets an access token from GoTrue with the password grant.
func signIn(email, password string) (int, string) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, _ := http.NewRequest(http.MethodPost, authURL+"/token?grant_type=password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.AccessToken
}

func (e *env) login(email, password string) *client {
	e.t.Helper()
	status, token := signIn(email, password)
	require.Equal(e.t, http.StatusOK, status, "supabase sign-in for %s", email)
	c := e.anon()
	c.token = token
	return c
}

func (c *client) do(method, path string, body any, wantStatus int, out any) {
	c.e.t.Helper()
	status, raw := c.raw(method, path, body)
	require.Equal(c.e.t, wantStatus, status, "%s %s: %s", method, path, raw)
	if out != nil {
		require.NoError(c.e.t, json.Unmarshal(raw, out), string(raw))
	}
}

func (c *client) raw(method, path string, body any) (int, []byte) {
	c.e.t.Helper()
	var rd io.Reader
	contentType := "application/json"
	if b, ok := body.([]byte); ok { // uploads
		rd, contentType = bytes.NewReader(b), "application/octet-stream"
	} else if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.e.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.e.server.URL+"/api/v1"+path, rd)
	require.NoError(c.e.t, err)
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	require.NoError(c.e.t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// reportService builds a report service without weather expectations.
func (e *env) reportService() *reports.Service {
	return reports.NewService(reports.ModuleStore{
		DB: e.db, Module: e.Systems, Telemetry: e.Telemetry, Alerting: e.Alerting, Notifications: e.Notifications,
	}, e.Prices, nil)
}
