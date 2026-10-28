package storage

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/chrishrb/noonside/internal/platform/netguard"

	"github.com/jlaffaye/ftp"
)

type FTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	Directory string
	TLS       bool
	// AllowPrivate permits private and local addresses (see netguard.Dialer).
	AllowPrivate bool
}

// FTP reads an export directory from an FTP server (the Solar-Log uploads
// its HTML export there).
type FTP struct {
	conn *ftp.ServerConn
	dir  string
}

func DialFTP(ctx context.Context, cfg FTPConfig) (*FTP, error) {
	port := cfg.Port
	if port == 0 {
		port = 21
	}
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	}
	opts := []ftp.DialOption{ftp.DialWithDialFunc(cancelableDial(ctx, tlsCfg, cfg.AllowPrivate))}
	if tlsCfg != nil {
		opts = append(opts, ftp.DialWithExplicitTLS(tlsCfg))
	}
	conn, err := ftp.Dial(net.JoinHostPort(cfg.Host, strconv.Itoa(port)), opts...)
	if err != nil {
		return nil, fmt.Errorf("ftp dial: %w", err)
	}
	if err := conn.Login(cfg.Username, cfg.Password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("ftp login: %w", err)
	}
	return &FTP{conn: conn, dir: cfg.Directory}, nil
}

// cancelableDial dials the control and data connections so that cancelling
// ctx closes them: the ftp package has no context support after dialing, and
// a stalled transfer would otherwise block the poll forever. With a custom
// dial func the package no longer wraps data connections in TLS, so that
// happens here (the control connection, dialed first, is upgraded by AUTH
// TLS in ftp.Dial). The address check also covers data connections, whose
// address comes from the server's PASV reply.
func cancelableDial(ctx context.Context, tlsCfg *tls.Config, allowPrivate bool) func(network, addr string) (net.Conn, error) {
	var mu sync.Mutex
	control := true
	d := netguard.Dialer(30*time.Second, allowPrivate)
	return func(network, addr string) (net.Conn, error) {
		raw, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		var conn net.Conn = &ctxConn{Conn: raw, stop: context.AfterFunc(ctx, func() { _ = raw.Close() })}
		mu.Lock()
		data := !control
		control = false
		mu.Unlock()
		if data && tlsCfg != nil {
			conn = tls.Client(conn, tlsCfg)
		}
		return conn, nil
	}
}

// ctxConn unregisters its close-on-cancel hook when closed.
type ctxConn struct {
	net.Conn
	stop func() bool
}

func (c *ctxConn) Close() error {
	c.stop()
	return c.Conn.Close()
}

func (f *FTP) List(ctx context.Context) ([]string, error) {
	entries, err := f.conn.List(f.dir)
	if err != nil {
		return nil, fmt.Errorf("ftp list: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type == ftp.EntryTypeFile {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

func (f *FTP) Read(ctx context.Context, name string) ([]byte, error) {
	r, err := f.conn.Retr(path.Join(f.dir, path.Base(name)))
	if err != nil {
		return nil, fmt.Errorf("ftp retr %s: %w", name, err)
	}
	defer r.Close() //nolint:errcheck
	return readLimited(r, name)
}

func (f *FTP) Close() error { return f.conn.Quit() }
