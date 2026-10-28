// Package media stores uploaded images (profile and system pictures) and
// validates them. Objects are written under immutable, unguessable keys and
// served same-origin under /media/ by the router from a directory, so anyone
// with the URL can load them.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register decoders for DecodeConfig
	_ "image/png"
	"io"
	"net/http"

	_ "golang.org/x/image/webp"
)

const (
	// PathPrefix is where images are served, relative to the app's origin.
	PathPrefix = "/media/"
	// MaxBytes is the largest accepted image. The web app scales pictures
	// down before uploading.
	MaxBytes = 2 << 20
	// MaxPixels is the largest accepted width and height.
	MaxPixels = 4096
	// CacheControl is sent with every object: keys never change content.
	CacheControl = "public, max-age=31536000, immutable"
)

// ErrInvalid is returned for uploads that are not an accepted image.
var ErrInvalid = errors.New("invalid image")

// Store writes and deletes objects.
type Store interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
	Delete(ctx context.Context, key string) error
}

// URL returns the path an object is served at; empty for no key.
func URL(key *string) string {
	if key == nil || *key == "" {
		return ""
	}
	return PathPrefix + *key
}

var extensions = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// Image is a validated upload.
type Image struct {
	Body        []byte
	ContentType string
}

// Read reads and validates an upload: JPEG, PNG or WebP (by content, not by
// the declared type), at most MaxBytes and MaxPixels×MaxPixels. Errors wrap
// ErrInvalid.
func Read(r io.Reader) (*Image, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBytes {
		return nil, fmt.Errorf("%w: larger than %d MB", ErrInvalid, MaxBytes>>20)
	}
	ct := http.DetectContentType(body)
	if _, ok := extensions[ct]; !ok {
		return nil, fmt.Errorf("%w: only JPEG, PNG and WebP are accepted", ErrInvalid)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: cannot be read", ErrInvalid)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxPixels || cfg.Height > MaxPixels {
		return nil, fmt.Errorf("%w: at most %d×%d pixels", ErrInvalid, MaxPixels, MaxPixels)
	}
	return &Image{Body: body, ContentType: ct}, nil
}

// NewKey returns a fresh key below prefix (e.g. "avatars/<user id>") for an
// image of the given type.
func NewKey(prefix, contentType string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + "/" + hex.EncodeToString(b) + extensions[contentType]
}
