package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type Local struct {
	dir string
}

func NewLocal(dir string) *Local {
	return &Local{dir: dir}
}

func (l *Local) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (l *Local) Read(ctx context.Context, name string) ([]byte, error) {
	if name != filepath.Base(name) {
		return nil, fmt.Errorf("invalid file name %q", name)
	}
	f, err := os.Open(filepath.Join(l.dir, name))
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	return readLimited(f, name)
}

func (l *Local) Files(ctx context.Context) ([]File, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, File{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}

func (l *Local) Remove(ctx context.Context, name string) error {
	if name != filepath.Base(name) {
		return fmt.Errorf("invalid file name %q", name)
	}
	return os.Remove(filepath.Join(l.dir, name))
}

func (l *Local) Close() error { return nil }
