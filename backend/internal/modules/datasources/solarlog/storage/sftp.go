package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/netguard"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SFTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	Directory string
	// HostKey pins the server's public key(s): one key per line in
	// authorized_keys ("ssh-ed25519 AAAA…") or known_hosts / ssh-keyscan
	// ("host ssh-ed25519 AAAA…") format. Empty fails with the offered key.
	HostKey string
	// AllowPrivate permits private and local addresses (see netguard.Dialer).
	AllowPrivate bool
}

// SFTP reads an export directory over SSH.
type SFTP struct {
	ssh    *ssh.Client
	client *sftp.Client
	dir    string
	stop   func() bool
}

func DialSFTP(ctx context.Context, cfg SFTPConfig) (*SFTP, error) {
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	keys, err := ParseHostKeys(cfg.HostKey)
	if err != nil {
		return nil, err
	}
	algos := []string{}
	for _, k := range keys {
		algos = append(algos, hostKeyAlgorithms(k.Type())...)
	}
	password := func() (string, error) { return cfg.Password, nil }
	sshCfg := &ssh.ClientConfig{
		User: cfg.Username,
		Auth: []ssh.AuthMethod{
			ssh.PasswordCallback(password),
			// Some servers only accept passwords via keyboard-interactive.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = cfg.Password
				}
				return answers, nil
			}),
		},
		HostKeyCallback:   pinnedHostKey(keys),
		HostKeyAlgorithms: algos,
		Timeout:           30 * time.Second,
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	d := netguard.Dialer(sshCfg.Timeout, cfg.AllowPrivate)
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sftp dial: %w", err)
	}
	// Bound the handshake; the ssh package has no context support.
	_ = conn.SetDeadline(time.Now().Add(sshCfg.Timeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sftp handshake: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	sc := ssh.NewClient(c, chans, reqs)
	client, err := sftp.NewClient(sc)
	if err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("sftp session: %w", err)
	}
	// Abort blocked reads when the poll is cancelled.
	stop := context.AfterFunc(ctx, func() { _ = sc.Close() })
	return &SFTP{ssh: sc, client: client, dir: cfg.Directory, stop: stop}, nil
}

func (s *SFTP) List(ctx context.Context) ([]string, error) {
	dir := s.dir
	if dir == "" {
		dir = "."
	}
	entries, err := s.client.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("sftp list: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Mode().IsRegular() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (s *SFTP) Read(ctx context.Context, name string) ([]byte, error) {
	f, err := s.client.Open(path.Join(s.dir, path.Base(name)))
	if err != nil {
		return nil, fmt.Errorf("sftp open %s: %w", name, err)
	}
	defer f.Close() //nolint:errcheck
	return readLimited(f, name)
}

func (s *SFTP) Files(ctx context.Context) ([]File, error) {
	dir := s.dir
	if dir == "" {
		dir = "."
	}
	entries, err := s.client.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("sftp list: %w", err)
	}
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		if e.Mode().IsRegular() {
			out = append(out, File{Name: e.Name(), Size: e.Size(), ModTime: e.ModTime()})
		}
	}
	return out, nil
}

func (s *SFTP) Remove(ctx context.Context, name string) error {
	if err := s.client.Remove(path.Join(s.dir, path.Base(name))); err != nil {
		return fmt.Errorf("sftp remove %s: %w", name, err)
	}
	return nil
}

// Purge deletes everything in the directory, including subdirectories a
// client may have created.
func (s *SFTP) Purge(ctx context.Context) error {
	dir := s.dir
	if dir == "" {
		dir = "."
	}
	entries, err := s.client.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("sftp list: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if err := s.client.RemoveAll(path.Join(dir, e.Name())); err != nil {
			errs = append(errs, fmt.Errorf("sftp remove %s: %w", e.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (s *SFTP) Close() error {
	s.stop()
	return errors.Join(s.client.Close(), s.ssh.Close())
}

// ParseHostKeys parses pinned host keys (see SFTPConfig.HostKey). Blank
// lines and # comments are ignored.
func ParseHostKeys(s string) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err == nil {
			keys = append(keys, k)
			continue
		}
		if _, _, k, _, _, err := ssh.ParseKnownHosts([]byte(line)); err == nil {
			keys = append(keys, k)
			continue
		}
		return nil, fmt.Errorf("invalid host key %q", truncate(line, 40))
	}
	return keys, nil
}

func pinnedHostKey(keys []ssh.PublicKey) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		for _, k := range keys {
			if bytes.Equal(k.Marshal(), key.Marshal()) {
				return nil
			}
		}
		offered := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		if len(keys) == 0 {
			return fmt.Errorf("no host key pinned; verify the fingerprint %s and set the host key to %q", ssh.FingerprintSHA256(key), offered)
		}
		return fmt.Errorf("host key mismatch: server offered %s (%s)", ssh.FingerprintSHA256(key), offered)
	}
}

// hostKeyAlgorithms makes the server present a key we have pinned.
func hostKeyAlgorithms(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// errScanned ends the handshake once ScanHostKey has the key.
var errScanned = errors.New("host key scanned")

// ScanHostKey returns the SSH host key the server presents, in
// authorized_keys format (trust on first use: only for servers whose
// identity is otherwise known, e.g. our own SFTP server).
func ScanHostKey(ctx context.Context, host string, port int, allowPrivate bool) (string, error) {
	var key string
	cfg := &ssh.ClientConfig{
		User: "scan",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
			return errScanned
		},
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoRSASHA512},
		Timeout:           15 * time.Second,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := netguard.Dialer(cfg.Timeout, allowPrivate).DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("ssh dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	_, _, _, err = ssh.NewClientConn(conn, addr, cfg)
	if key == "" {
		return "", fmt.Errorf("ssh host key scan: %w", err)
	}
	return key, nil
}
