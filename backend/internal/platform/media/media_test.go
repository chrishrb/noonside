package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
	return b.Bytes()
}

func TestRead(t *testing.T) {
	img, err := Read(bytes.NewReader(pngBytes(t, 10, 20)))
	require.NoError(t, err)
	assert.Equal(t, "image/png", img.ContentType)

	for name, body := range map[string][]byte{
		"text":      []byte("hello"),
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"too large": append(pngBytes(t, 1, 1), make([]byte, MaxBytes)...),
		"too wide":  pngBytes(t, MaxPixels+1, 1),
		"truncated": pngBytes(t, 10, 10)[:20],
	} {
		_, err := Read(bytes.NewReader(body))
		assert.True(t, errors.Is(err, ErrInvalid), name)
	}
}

func TestNewKeyAndURL(t *testing.T) {
	k := NewKey("avatars/u1", "image/webp")
	assert.True(t, strings.HasPrefix(k, "avatars/u1/"))
	assert.True(t, strings.HasSuffix(k, ".webp"))
	assert.NotEqual(t, k, NewKey("avatars/u1", "image/webp"))
	assert.Equal(t, "/media/"+k, URL(&k))
	assert.Equal(t, "", URL(nil))
}

func TestDir(t *testing.T) {
	root := t.TempDir()
	d, err := NewDir(root)
	require.NoError(t, err)
	ctx := context.Background()
	body := pngBytes(t, 2, 2)
	require.NoError(t, d.Put(ctx, "systems/s1/a.png", "image/png", body))
	require.Error(t, d.Put(ctx, "../escape.png", "image/png", body))

	rec := httptest.NewRecorder()
	d.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/systems/s1/a.png", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, CacheControl, rec.Header().Get("Cache-Control"))
	assert.Equal(t, body, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	d.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/systems/s1/", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	require.NoError(t, d.Delete(ctx, "systems/s1/a.png"))
	require.NoError(t, d.Delete(ctx, "systems/s1/a.png")) // already gone
	_, err = os.Stat(filepath.Join(root, "systems/s1/a.png"))
	assert.True(t, os.IsNotExist(err))
}
