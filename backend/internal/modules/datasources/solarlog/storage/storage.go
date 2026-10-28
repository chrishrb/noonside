// Package storage provides access to Solar-Log export files.
package storage

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Storage lists and reads the files of a Solar-Log export directory.
type Storage interface {
	List(ctx context.Context) ([]string, error)
	Read(ctx context.Context, name string) ([]byte, error)
	Close() error
}

// File is a regular file in the export directory.
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Manager is implemented by storages that can report sizes and delete
// files; hosted upload directories use it to enforce quota and retention.
type Manager interface {
	Files(ctx context.Context) ([]File, error)
	Remove(ctx context.Context, name string) error
}

// MaxFileSize bounds a single export file. Solar-Log files are a few KB up
// to a few MB (days_hist.js); anything larger is not an export.
const MaxFileSize = 20 << 20

// readLimited reads r up to MaxFileSize and fails beyond it.
func readLimited(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%s: %w", name, ErrTooLarge)
	}
	return data, nil
}

// ErrTooLarge is returned by Read for files above MaxFileSize.
var ErrTooLarge = fmt.Errorf("file larger than %d MB", MaxFileSize>>20)
