package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// startSFTP serves the local filesystem over SFTP for user "solar" with
// password "pw" and returns the port and the server's authorized_keys line.
func startSFTP(t *testing.T) (int, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "solar" && string(pw) == "pw" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSFTP(conn, cfg)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
}

func serveSFTP(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close() //nolint:errcheck
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range creqs {
				_ = r.Reply(r.Type == "subsystem" && string(r.Payload[4:]) == "sftp", nil)
			}
		}()
		srv, err := sftp.NewServer(ch, sftp.ReadOnly())
		if err != nil {
			return
		}
		_ = srv.Serve()
		_ = srv.Close()
	}
}

func TestSFTP(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base_vars.js"), []byte("b"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "min241010.js"), []byte("a"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
	port, hostKey := startSFTP(t)
	cfg := SFTPConfig{Host: "127.0.0.1", Port: port, Username: "solar", Password: "pw", Directory: dir, HostKey: hostKey, AllowPrivate: true}
	ctx := context.Background()

	s, err := DialSFTP(ctx, cfg)
	require.NoError(t, err)
	names, err := s.List(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"base_vars.js", "min241010.js"}, names)
	data, err := s.Read(ctx, "../"+filepath.Base(dir)+"/base_vars.js")
	require.NoError(t, err, "only the base name is used")
	assert.Equal(t, "b", string(data))
	require.NoError(t, s.Close())

	t.Run("known_hosts format", func(t *testing.T) {
		c := cfg
		c.HostKey = "# ssh-keyscan\n[127.0.0.1]:" + strconv.Itoa(port) + " " + hostKey + "\n"
		s, err := DialSFTP(ctx, c)
		require.NoError(t, err)
		_ = s.Close()
	})

	t.Run("no host key", func(t *testing.T) {
		c := cfg
		c.HostKey = ""
		_, err := DialSFTP(ctx, c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no host key pinned")
		assert.Contains(t, err.Error(), hostKey)
	})

	t.Run("wrong host key", func(t *testing.T) {
		_, other := startSFTP(t)
		c := cfg
		c.HostKey = other
		_, err := DialSFTP(ctx, c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "host key mismatch")
	})

	t.Run("wrong password", func(t *testing.T) {
		c := cfg
		c.Password = "nope"
		_, err := DialSFTP(ctx, c)
		assert.Error(t, err)
	})
}

func TestParseHostKeys(t *testing.T) {
	_, err := ParseHostKeys("not a key")
	assert.Error(t, err)
	keys, err := ParseHostKeys("\n# comment\n")
	require.NoError(t, err)
	assert.Empty(t, keys)
}
