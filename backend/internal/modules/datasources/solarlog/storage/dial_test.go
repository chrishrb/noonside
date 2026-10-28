package storage

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialFTPRefusesLocal(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	// A name that resolves to a local address is refused.
	_, port, _ := net.SplitHostPort(l.Addr().String())
	_, err = DialFTP(context.Background(), FTPConfig{Host: "localhost", Port: atoi(t, port)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	p, err := net.LookupPort("tcp", s)
	require.NoError(t, err)
	return p
}
