package auth

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGoTrueKeys(t *testing.T) {
	k, err := GenerateGoTrueKeys(time.Hour)
	require.NoError(t, err)

	var jwks []map[string]any
	require.NoError(t, json.Unmarshal([]byte(k.JWTKeys), &jwks))
	require.Len(t, jwks, 1)
	assert.NotEmpty(t, jwks[0]["d"], "GoTrue needs the private key")

	// The published key set (public part only) verifies both tokens.
	pub := map[string]any{}
	for _, f := range []string{"kty", "crv", "alg", "use", "kid", "x", "y"} {
		pub[f] = jwks[0][f]
	}
	set, _ := json.Marshal(map[string]any{"keys": []any{pub}})
	kf, err := keyfunc.NewJWKSetJSON(set)
	require.NoError(t, err)
	for role, tok := range map[string]string{"service_role": k.ServiceRoleKey} {
		var c jwt.MapClaims
		_, err := jwt.ParseWithClaims(tok, &c, kf.Keyfunc, jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired())
		require.NoError(t, err, role)
		assert.Equal(t, role, c["role"])
	}
	assert.GreaterOrEqual(t, len(k.JWTSecret), 32)

	// API keys are not user tokens.
	v := newVerifier(kf, testIssuer, "")
	_, err = v.Verify(k.ServiceRoleKey)
	assert.Error(t, err)
}
