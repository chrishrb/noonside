package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SuperadminClaim is the app_metadata flag marking superadmins. app_metadata
// can only be written with the Supabase secret key (see `noonside user promote`).
const SuperadminClaim = "solar0_superadmin"

// SupabaseClaims are the claims of a Supabase Auth access token.
type SupabaseClaims struct {
	jwt.RegisteredClaims
	Email       string         `json:"email"`
	Role        string         `json:"role"`
	AppMetadata map[string]any `json:"app_metadata"`
	IsAnonymous bool           `json:"is_anonymous"`
}

// Verifier validates Supabase Auth (GoTrue) access tokens.
type Verifier struct {
	jwks       keyfunc.Keyfunc
	hmacSecret []byte
	issuer     string
	methods    []string
	now        func() time.Time
}

type VerifierConfig struct {
	// URL is the public URL the SPA signs in at, e.g. https://noonside.example.com;
	// tokens are issued by <URL>/auth/v1.
	URL string
	// AuthURL is where the server reaches the Auth server (GoTrue) for the
	// JWKS, e.g. http://noonside-auth:9999. Default: <URL>/auth/v1.
	AuthURL string
	// JWTSecret enables the legacy HS256 shared secret (optional).
	JWTSecret string
}

// NewVerifier fetches the Auth server's JWKS in the background and keeps it
// refreshed until ctx is cancelled.
func NewVerifier(ctx context.Context, cfg VerifierConfig) (*Verifier, error) {
	base := strings.TrimRight(cfg.URL, "/")
	if base == "" {
		return nil, errors.New("supabase url is required")
	}
	authURL := strings.TrimRight(cfg.AuthURL, "/")
	if authURL == "" {
		authURL = base + "/auth/v1"
	}
	// Set when the constructor returns; a failed first fetch reports before.
	var loaded atomic.Pointer[keyfunc.Keyfunc]
	jwks, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{authURL + "/.well-known/jwks.json"},
		keyfunc.Override{RefreshErrorHandlerFunc: refreshErrorHandler(slog.Default(), func(ctx context.Context) bool {
			k := loaded.Load()
			return k != nil && hasKeys(ctx, *k)
		})})
	if err != nil {
		return nil, fmt.Errorf("load supabase jwks: %w", err)
	}
	loaded.Store(&jwks)
	return newVerifier(jwks, base+"/auth/v1", cfg.JWTSecret), nil
}

// refreshErrorHandler logs failed JWKS refreshes. A failed hourly refresh
// keeps the cached keys, and an unknown key ID fetches them again, so it is
// only an error while no keys are loaded and no token can be verified.
func refreshErrorHandler(logger *slog.Logger, haveKeys func(context.Context) bool) func(string) func(context.Context, error) {
	return func(u string) func(context.Context, error) {
		return func(ctx context.Context, err error) {
			level := slog.LevelWarn
			if !haveKeys(context.WithoutCancel(ctx)) { // ctx has usually timed out
				level = slog.LevelError
			}
			logger.Log(ctx, level, "refresh supabase jwks failed", "error", err, "url", u)
		}
	}
}

func hasKeys(ctx context.Context, jwks keyfunc.Keyfunc) bool {
	keys, err := jwks.Storage().KeyReadAll(ctx)
	return err == nil && len(keys) > 0
}

func newVerifier(jwks keyfunc.Keyfunc, issuer, secret string) *Verifier {
	v := &Verifier{jwks: jwks, issuer: issuer, now: time.Now,
		methods: []string{"ES256", "RS256", "EdDSA"}}
	if secret != "" {
		v.hmacSecret = []byte(secret)
		v.methods = append(v.methods, "HS256")
	}
	return v
}

// Verify checks signature, issuer, audience and expiry and returns the caller.
// Per-system permissions are resolved separately from the database.
func (v *Verifier) Verify(token string) (*Principal, error) {
	var c SupabaseClaims
	_, err := jwt.ParseWithClaims(token, &c, v.key,
		jwt.WithValidMethods(v.methods),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience("authenticated"),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if c.Role != "authenticated" || c.IsAnonymous {
		return nil, errors.New("token is not for an authenticated user")
	}
	uid, err := uuid.Parse(c.Subject)
	if err != nil {
		return nil, errors.New("invalid token subject")
	}
	super, _ := c.AppMetadata[SuperadminClaim].(bool)
	return &Principal{UserID: uid, Email: strings.ToLower(c.Email), Superadmin: super}, nil
}

func (v *Verifier) key(t *jwt.Token) (any, error) {
	if _, ok := t.Method.(*jwt.SigningMethodHMAC); ok {
		if v.hmacSecret == nil {
			return nil, errors.New("hs256 tokens are not accepted")
		}
		return v.hmacSecret, nil
	}
	return v.jwks.Keyfunc(t)
}
