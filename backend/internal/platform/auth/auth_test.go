package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleScopes(t *testing.T) {
	assert.Contains(t, RoleScopes(RoleOwner), ScopeSystemDelete)
	assert.NotContains(t, RoleScopes(RoleAdmin), ScopeSystemDelete)
	assert.Contains(t, RoleScopes(RoleAdmin), ScopeMembersInvite)
	assert.NotContains(t, RoleScopes(RoleViewer), ScopeMembersInvite)
	assert.NotContains(t, RoleScopes(RoleViewer), ScopeDataIngest)
	assert.Contains(t, RoleScopes(RoleViewer), ScopeDataRead)
	assert.Nil(t, RoleScopes("bogus"))

	assert.True(t, CanAssignRole(RoleOwner, RoleOwner))
	assert.False(t, CanAssignRole(RoleAdmin, RoleOwner))
	assert.True(t, CanAssignRole(RoleAdmin, RoleViewer))
	assert.False(t, CanAssignRole(RoleViewer, RoleViewer))
}

func TestSuperadminHasEverything(t *testing.T) {
	p := &Principal{Superadmin: true}
	assert.True(t, p.Has(uuid.New(), ScopeSystemDelete))
	assert.True(t, p.IsMember(uuid.New()))
}

const (
	testIssuer = "http://127.0.0.1:54321/auth/v1"
	testSecret = "super-secret-jwt-token-with-at-least-32-characters-long"
)

type testKeys struct {
	priv *ecdsa.PrivateKey
	kid  string
	v    *Verifier
}

func newTestKeys(t *testing.T, secret string) *testKeys {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	kid := uuid.NewString()
	kf, err := keyfunc.NewJWKSetJSON(jwksJSON(priv, kid))
	require.NoError(t, err)
	return &testKeys{priv: priv, kid: kid, v: newVerifier(kf, testIssuer, secret)}
}

func jwksJSON(priv *ecdsa.PrivateKey, kid string) []byte {
	enc := base64.RawURLEncoding
	b, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"x": enc.EncodeToString(priv.X.FillBytes(make([]byte, 32))),
		"y": enc.EncodeToString(priv.Y.FillBytes(make([]byte, 32))),
	}}})
	return b
}

func claims(sub string, mod func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss": testIssuer, "aud": "authenticated", "sub": sub, "role": "authenticated",
		"email": "User@Example.com", "exp": time.Now().Add(time.Hour).Unix(),
		"app_metadata": map[string]any{"provider": "email"},
	}
	if mod != nil {
		mod(c)
	}
	return c
}

func (k *testKeys) sign(t *testing.T, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, c)
	tok.Header["kid"] = k.kid
	s, err := tok.SignedString(k.priv)
	require.NoError(t, err)
	return s
}

func TestVerifierAcceptsSupabaseToken(t *testing.T) {
	k := newTestKeys(t, "")
	uid := uuid.New()
	p, err := k.v.Verify(k.sign(t, claims(uid.String(), nil)))
	require.NoError(t, err)
	assert.Equal(t, uid, p.UserID)
	assert.Equal(t, "user@example.com", p.Email)
	assert.False(t, p.Superadmin)

	p, err = k.v.Verify(k.sign(t, claims(uid.String(), func(c jwt.MapClaims) {
		c["app_metadata"] = map[string]any{SuperadminClaim: true}
	})))
	require.NoError(t, err)
	assert.True(t, p.Superadmin)
}

// The server fetches the keys from GoTrue directly, while the tokens name
// the public URL as issuer.
func TestVerifierSeparateAuthURL(t *testing.T) {
	k := newTestKeys(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(jwksJSON(k.priv, k.kid))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v, err := NewVerifier(ctx, VerifierConfig{URL: "https://app.example.com/", AuthURL: srv.URL + "/"})
	require.NoError(t, err)

	uid := uuid.New()
	p, err := v.Verify(k.sign(t, claims(uid.String(), func(c jwt.MapClaims) { c["iss"] = "https://app.example.com/auth/v1" })))
	require.NoError(t, err)
	assert.Equal(t, uid, p.UserID)

	_, err = v.Verify(k.sign(t, claims(uid.String(), func(c jwt.MapClaims) { c["iss"] = srv.URL })))
	assert.Error(t, err, "the internal address is not the issuer")
}

func TestVerifierRejects(t *testing.T) {
	k := newTestKeys(t, "")
	uid := uuid.NewString()
	cases := map[string]func(jwt.MapClaims){
		"expired":        func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
		"no expiry":      func(c jwt.MapClaims) { delete(c, "exp") },
		"wrong issuer":   func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com/auth/v1" },
		"wrong audience": func(c jwt.MapClaims) { c["aud"] = "other" },
		"anon role":      func(c jwt.MapClaims) { c["role"] = "anon" },
		"anonymous user": func(c jwt.MapClaims) { c["is_anonymous"] = true },
		"bad subject":    func(c jwt.MapClaims) { c["sub"] = "not-a-uuid" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := k.v.Verify(k.sign(t, claims(uid, mod)))
			assert.Error(t, err)
		})
	}

	t.Run("foreign key", func(t *testing.T) {
		other := newTestKeys(t, "")
		other.kid = k.kid
		_, err := k.v.Verify(other.sign(t, claims(uid, nil)))
		assert.Error(t, err)
	})
	t.Run("hs256 without secret", func(t *testing.T) {
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims(uid, nil)).SignedString([]byte(testSecret))
		_, err := k.v.Verify(tok)
		assert.Error(t, err)
	})
	t.Run("alg none", func(t *testing.T) {
		tok, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims(uid, nil)).SignedString(jwt.UnsafeAllowNoneSignatureType)
		_, err := k.v.Verify(tok)
		assert.Error(t, err)
	})
}

func TestVerifierLegacyHS256(t *testing.T) {
	k := newTestKeys(t, testSecret)
	uid := uuid.New()
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims(uid.String(), nil)).SignedString([]byte(testSecret))
	p, err := k.v.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, uid, p.UserID)

	bad, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims(uid.String(), nil)).SignedString([]byte(fmt.Sprint("x", testSecret)))
	_, err = k.v.Verify(bad)
	assert.Error(t, err)
}

// levels records the levels of the log records it receives.
type levels struct{ got []slog.Level }

func (l *levels) Enabled(context.Context, slog.Level) bool { return true }
func (l *levels) Handle(_ context.Context, r slog.Record) error {
	l.got = append(l.got, r.Level)
	return nil
}
func (l *levels) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *levels) WithGroup(string) slog.Handler      { return l }

func TestJWKSRefreshErrorLevel(t *testing.T) {
	t.Run("keys cached: warning", func(t *testing.T) {
		rec := &levels{}
		h := refreshErrorHandler(slog.New(rec), func(context.Context) bool { return true })
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()
		h("https://x.supabase.co/auth/v1/.well-known/jwks.json")(ctx, context.DeadlineExceeded)
		assert.Equal(t, []slog.Level{slog.LevelWarn}, rec.got)
	})
	t.Run("no keys: error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "down", http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		rec := &levels{}
		prev := slog.Default()
		slog.SetDefault(slog.New(rec))
		defer slog.SetDefault(prev)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := NewVerifier(ctx, VerifierConfig{URL: srv.URL})
		require.NoError(t, err) // starts without keys and keeps trying
		assert.Equal(t, []slog.Level{slog.LevelError}, rec.got)
	})
}
