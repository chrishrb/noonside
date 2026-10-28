package storage

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFTP serves just enough FTP for RETR. Files named "stall" are
// announced (150) but never sent.
func startFTP(t *testing.T, files map[string]string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFTP(c, files)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func serveFTP(c net.Conn, files map[string]string) {
	defer c.Close() //nolint:errcheck
	reply := func(format string, args ...any) { _, _ = fmt.Fprintf(c, format+"\r\n", args...) }
	reply("220 ready")
	var data net.Listener
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch cmd {
		case "USER":
			reply("331 password")
		case "PASS":
			reply("230 logged in")
		case "TYPE":
			reply("200 ok")
		case "EPSV":
			if data, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
				return
			}
			reply("229 Entering Extended Passive Mode (|||%d|)", data.Addr().(*net.TCPAddr).Port)
		case "RETR":
			dc, err := data.Accept()
			_ = data.Close()
			if err != nil {
				return
			}
			reply("150 opening")
			if strings.HasSuffix(arg, "stall") {
				defer dc.Close() //nolint:errcheck
				continue
			}
			_, _ = dc.Write([]byte(files[strings.TrimPrefix(arg, "/")]))
			_ = dc.Close()
			reply("226 done")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func TestFTPRead(t *testing.T) {
	port := startFTP(t, map[string]string{"min_day.js": "m[mi++]=\"x\""})
	f, err := DialFTP(context.Background(), FTPConfig{Host: "127.0.0.1", Port: port, Username: "u", Password: "p", AllowPrivate: true})
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck

	b, err := f.Read(context.Background(), "min_day.js")
	require.NoError(t, err)
	assert.Equal(t, "m[mi++]=\"x\"", string(b))
}

// A stalled transfer must not outlive the poll's context.
func TestFTPReadCancel(t *testing.T) {
	port := startFTP(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	f, err := DialFTP(ctx, FTPConfig{Host: "127.0.0.1", Port: port, Username: "u", Password: "p", AllowPrivate: true})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := f.Read(ctx, "stall")
		done <- err
	}()
	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not return after the context was cancelled")
	}
}
