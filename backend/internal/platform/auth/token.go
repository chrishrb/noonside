package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewOpaqueToken returns a random URL-safe token and its SHA-256 hash.
// Only the hash is stored; the token is handed to the client once.
func NewOpaqueToken(prefix string) (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
