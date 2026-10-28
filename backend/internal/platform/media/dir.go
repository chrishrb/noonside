package media

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Dir stores objects as files below a directory (development, Docker,
// tests). Handler serves them.
type Dir struct{ root string }

func NewDir(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Dir{root: root}, nil
}

func (d *Dir) path(key string) (string, error) {
	clean := path.Clean("/" + key)
	if clean == "/" || clean != "/"+key || strings.Contains(key, "..") {
		return "", errors.New("media: invalid key")
	}
	return filepath.Join(d.root, filepath.FromSlash(clean)), nil
}

func (d *Dir) Put(_ context.Context, key, _ string, body []byte) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}

func (d *Dir) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Handler serves the objects for GET/HEAD below PathPrefix, without
// directory listings.
func (d *Dir) Handler() http.Handler {
	files := http.StripPrefix(strings.TrimSuffix(PathPrefix, "/"), http.FileServer(http.Dir(d.root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", CacheControl)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
