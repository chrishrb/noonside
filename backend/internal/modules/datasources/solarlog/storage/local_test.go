package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "min241010.js"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base_vars.js"), []byte("b"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))

	s := NewLocal(dir)
	names, err := s.List(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"min241010.js", "base_vars.js"}, names)

	data, err := s.Read(context.Background(), "base_vars.js")
	require.NoError(t, err)
	assert.Equal(t, "b", string(data))

	_, err = s.Read(context.Background(), "../etc/passwd")
	assert.Error(t, err)

	_, err = NewLocal("/does/not/exist").List(context.Background())
	assert.Error(t, err)
}

func TestReadLimit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.js"), make([]byte, MaxFileSize+1), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.js"), make([]byte, MaxFileSize), 0o600))
	l := NewLocal(dir)
	_, err := l.Read(context.Background(), "big.js")
	require.ErrorIs(t, err, ErrTooLarge)
	data, err := l.Read(context.Background(), "ok.js")
	require.NoError(t, err)
	assert.Len(t, data, MaxFileSize)
}
