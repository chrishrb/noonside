package hostedftp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/google/uuid"
)

// KindHosted is the data source kind backed by an upload account.
const KindHosted = "solarlog_hosted"

// HookName names SFTPGo's event action and rule that report uploads.
const HookName = "noonside-upload"

// API is the part of the SFTPGo API the service needs (Client).
type API interface {
	CreateUser(ctx context.Context, u NewUser) error
	GetUser(ctx context.Context, username string) (*User, error)
	UpdatePassword(ctx context.Context, username, password string) error
	DeleteUser(ctx context.Context, username string) error
	ListUsers(ctx context.Context) ([]User, error)
	EnsureEventRule(ctx context.Context, h Hook) error
}

// Settings configure the server and what users get.
type Settings struct {
	// QuotaBytes is the upload quota per source (SFTPGo refuses uploads
	// beyond it).
	QuotaBytes int64
	// MaxAccounts caps the hosted sources across all systems (0: no cap).
	MaxAccounts int
	// Retention keeps imported minute files this long.
	Retention time.Duration
	// HomeBase is the absolute directory on the SFTPGo server under which
	// the accounts' homes are created.
	HomeBase string
	// SFTPAddr (host:port) is where the API reads the uploads over SFTP.
	SFTPAddr string
	// HostKey pins SFTPGo's SSH host key; empty reads it from SFTPAddr when
	// an account is created (trust on first use, it is our own server).
	HostKey string
	// PublicHost and PublicPort are where devices upload over FTP.
	PublicHost string
	PublicPort int
	// HookURL is the API's upload hook as SFTPGo reaches it, HookToken the
	// bearer token SFTPGo sends (HookToken).
	HookURL   string
	HookToken string
}

// Account is a provisioned upload account.
type Account struct {
	Username string
	// Password is only set right after creating or resetting it.
	Password string
	HostKey  string
}

// descPrefix marks users noonside manages; GC only touches those.
const descPrefix = "data-source="

// Service provisions upload accounts on the SFTPGo server.
type Service struct {
	api      API
	settings Settings
	// scan reads the server's SSH host key (replaced in tests).
	scan   func(ctx context.Context, host string, port int, allowPrivate bool) (string, error)
	logger *slog.Logger

	mu      sync.Mutex
	hostKey string // cached by serverHostKey
}

func NewService(api API, settings Settings) *Service {
	return &Service{
		api: api, settings: settings,
		scan:   storage.ScanHostKey,
		logger: slog.Default().With("component", "hostedftp"),
	}
}

func (s *Service) Settings() Settings { return s.settings }

// HookToken is the bearer token of the upload hook, derived from the
// server's SECRET_KEY so nothing else needs to be configured.
func HookToken(secretKey string) string {
	m := hmac.New(sha256.New, []byte(secretKey))
	m.Write([]byte("hosted-ftp-hook"))
	return hex.EncodeToString(m.Sum(nil))
}

// ValidHookToken compares a presented token in constant time.
func (s *Service) ValidHookToken(token string) bool {
	return s.settings.HookToken != "" && hmac.Equal([]byte(token), []byte(s.settings.HookToken))
}

// EnsureHook configures SFTPGo to call the upload hook after every upload.
func (s *Service) EnsureHook(ctx context.Context) error {
	return s.api.EnsureEventRule(ctx, Hook{Name: HookName, URL: s.settings.HookURL, Token: s.settings.HookToken})
}

// HomeDir is the upload directory of a source on the SFTPGo server.
func (s *Service) HomeDir(systemID, sourceID uuid.UUID) string {
	return path.Join(s.settings.HomeBase, systemID.String(), sourceID.String())
}

// serverHostKey returns the pinned host key, or the one SFTPGo presents.
func (s *Service) serverHostKey(ctx context.Context) (string, error) {
	if s.settings.HostKey != "" {
		return s.settings.HostKey, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hostKey != "" {
		return s.hostKey, nil
	}
	host, port, err := s.sftpAddr()
	if err != nil {
		return "", err
	}
	key, err := s.scan(ctx, host, port, true)
	if err != nil {
		return "", fmt.Errorf("read sftp host key: %w", err)
	}
	s.hostKey = key
	return key, nil
}

func (s *Service) sftpAddr() (string, int, error) {
	host, p, err := net.SplitHostPort(s.settings.SFTPAddr)
	if err != nil {
		return "", 0, fmt.Errorf("hosted_ftp_sftp_addr: %w", err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, fmt.Errorf("hosted_ftp_sftp_addr: %w", err)
	}
	return host, port, nil
}

// Dial opens the account's directory over SFTP. The host key in the
// settings wins over the one stored with the source.
func (s *Service) Dial(ctx context.Context, a Account) (*storage.SFTP, error) {
	host, port, err := s.sftpAddr()
	if err != nil {
		return nil, err
	}
	key := a.HostKey
	if s.settings.HostKey != "" {
		key = s.settings.HostKey
	}
	// Our own server, usually at a cluster-internal address.
	return storage.DialSFTP(ctx, storage.SFTPConfig{
		Host: host, Port: port, Username: a.Username, Password: a.Password, HostKey: key, AllowPrivate: true,
	})
}

// Provision creates the upload account of a data source with a new
// username and password and pins the server's SSH host key.
func (s *Service) Provision(ctx context.Context, systemID, sourceID uuid.UUID) (*Account, error) {
	pw, err := GeneratePassword()
	if err != nil {
		return nil, err
	}
	name, err := GenerateUsername()
	if err != nil {
		return nil, err
	}
	key, err := s.serverHostKey(ctx)
	if err != nil {
		return nil, err
	}
	err = s.api.CreateUser(ctx, NewUser{
		Username: name, Password: pw, HomeDir: s.HomeDir(systemID, sourceID),
		QuotaBytes: s.settings.QuotaBytes, MaxFileSize: storage.MaxFileSize,
		Description: descPrefix + sourceID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("create upload account: %w", err)
	}
	s.logger.Info("created upload account", "data_source_id", sourceID, "system_id", systemID, "username", name)
	return &Account{Username: name, Password: pw, HostKey: key}, nil
}

// ResetPassword sets and returns a new password.
func (s *Service) ResetPassword(ctx context.Context, username string) (string, error) {
	pw, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	if err := s.api.UpdatePassword(ctx, username, pw); err != nil {
		return "", fmt.Errorf("reset upload password: %w", err)
	}
	s.logger.Info("reset upload password", "username", username)
	return pw, nil
}

// Remove deletes the account's files and then the account (SFTPGo keeps
// the files of deleted users). Without a password (orphans) it sets a new
// one first, to log in and delete. A missing account is no error.
func (s *Service) Remove(ctx context.Context, a Account) error {
	if _, err := s.api.GetUser(ctx, a.Username); IsNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read upload account: %w", err)
	}
	if a.Password == "" {
		pw, err := s.ResetPassword(ctx, a.Username)
		if err != nil {
			return err
		}
		a.Password = pw
	}
	if a.HostKey == "" {
		key, err := s.serverHostKey(ctx)
		if err != nil {
			return err
		}
		a.HostKey = key
	}
	conn, err := s.Dial(ctx, a)
	if err != nil {
		return fmt.Errorf("delete upload files: %w", err)
	}
	err = conn.Purge(ctx)
	_ = conn.Close()
	if err != nil {
		return fmt.Errorf("delete upload files: %w", err)
	}
	if err := s.api.DeleteUser(ctx, a.Username); err != nil {
		return fmt.Errorf("delete upload account: %w", err)
	}
	s.logger.Info("deleted upload account", "username", a.Username)
	return nil
}

// State is the JSON state of a solarlog_hosted data source.
type State struct {
	UsedBytes int64 `json:"used_bytes"`
	// Locked: the quota is (nearly) used up, so SFTPGo refuses uploads.
	Locked    bool      `json:"locked,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func ParseState(raw []byte) State {
	var st State
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

// ErrQuota fails runs while the quota is (nearly) used up.
var ErrQuota = errors.New("upload quota exceeded")

// Check runs clean (the poller's housekeeping, which returns the bytes left
// in the directory) and records the usage. SFTPGo enforces the quota
// itself: it removes an upload that would exceed it, so usage never quite
// reaches it. From 90 % on the account counts as full and the run fails
// with ErrQuota, so the data_source_failing alert fires.
func (s *Service) Check(ctx context.Context, prev State, clean func(context.Context) (int64, error), now time.Time) (State, error) {
	used, err := clean(ctx)
	if err != nil {
		return prev, err
	}
	quota := s.settings.QuotaBytes
	st := State{UsedBytes: used, Locked: quota > 0 && used >= quota*9/10, CheckedAt: now}
	if st.Locked {
		return st, fmt.Errorf("%w: %d MB of %d MB used; uploads are refused (delete and re-create the source to start over)",
			ErrQuota, used>>20, quota>>20)
	}
	return st, nil
}
