package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GoTrueKeys are the secrets of a self-hosted Supabase Auth (GoTrue) server.
type GoTrueKeys struct {
	// JWTKeys is GOTRUE_JWT_KEYS: a JSON array with one ES256 private JWK,
	// used to sign access tokens and published (public part) at
	// /.well-known/jwks.json, where the Verifier fetches it.
	JWTKeys string
	// JWTSecret is GOTRUE_JWT_SECRET (HS256), which GoTrue still requires;
	// the Verifier doesn't accept HS256 unless SUPABASE_JWT_SECRET is set.
	JWTSecret string
	// AnonKey is the publishable key (VITE_SUPABASE_PUBLISHABLE_KEY); not
	// secret.
	AnonKey string
	// ServiceRoleKey is SUPABASE_SECRET_KEY: GoTrue's admin API accepts it.
	ServiceRoleKey string
}

// GenerateGoTrueKeys creates a signing key and the anon and service role
// tokens signed with it, valid for validity.
func GenerateGoTrueKeys(validity time.Duration) (*GoTrueKeys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	d, err := priv.Bytes()
	if err != nil {
		return nil, err
	}
	pub, err := priv.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		return nil, err
	}
	kid := uuid.NewString()
	enc := base64.RawURLEncoding
	jwk := map[string]any{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"key_ops": []string{"sign", "verify"}, "ext": true,
		"x": enc.EncodeToString(pub[1:33]),
		"y": enc.EncodeToString(pub[33:65]),
		"d": enc.EncodeToString(d),
	}
	keys, err := json.Marshal([]any{jwk})
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	now := time.Now()
	token := func(role string) (string, error) {
		t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"iss": "noonside", "role": role, "iat": now.Unix(), "exp": now.Add(validity).Unix(),
		})
		t.Header["kid"] = kid
		return t.SignedString(priv)
	}
	anon, err := token("anon")
	if err != nil {
		return nil, fmt.Errorf("sign anon key: %w", err)
	}
	service, err := token("service_role")
	if err != nil {
		return nil, fmt.Errorf("sign service role key: %w", err)
	}
	return &GoTrueKeys{
		JWTKeys:        string(keys),
		JWTSecret:      enc.EncodeToString(secret),
		AnonKey:        anon,
		ServiceRoleKey: service,
	}, nil
}
