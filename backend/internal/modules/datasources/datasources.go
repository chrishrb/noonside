// Package datasources is the data sources module: where a system's data
// comes from (Solar-Log exports over FTP/FTPS/SFTP, hosted FTP accounts on
// noonside's SFTPGo server) or goes to (PVOutput), and the scheduler job that
// runs them.
// It owns the table data_sources.
package datasources

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/errtrack"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/google/uuid"
)

const (
	KindSolarLogRemote = "solarlog_remote"
	// KindSolarLogHosted reads the upload account noonside created on its
	// SFTPGo server (the device uploads there over FTP).
	KindSolarLogHosted = hostedftp.KindHosted
	// KindPVOutputExport pushes the system's data to PVOutput.
	KindPVOutputExport = "pvoutput_export"
)

type DataSource struct {
	ID            uuid.UUID
	SystemID      uuid.UUID
	Kind          string
	Name          string
	Config        []byte // JSON; secret fields are encrypted by the caller
	Enabled       bool
	PollInterval  time.Duration
	LastRunAt     *time.Time
	LastSuccessAt *time.Time
	LastError     string
	// State is the progress of pushing sources (JSON).
	State []byte
	// UploadAt is the last upload to a hosted FTP account (from the upload
	// hook), ImportedUploadAt the newest one a successful run imported.
	UploadAt         *time.Time
	ImportedUploadAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Config configures the pollers.
type Config struct {
	// Box seals and opens the sources' secrets (nil: secrets cannot be
	// stored).
	Box *secretbox.Box
	// AllowPrivateHosts lets remote sources connect to private or local
	// addresses.
	AllowPrivateHosts bool
	// PVOutputURL is PVOutput's base URL.
	PVOutputURL string
	// Hosted manages the upload accounts of solarlog_hosted sources (nil:
	// hosted FTP is not offered).
	Hosted *hostedftp.Service
}

// Module is the data sources module.
type Module struct {
	db      *db.DB
	systems *systems.Module
	tel     *telemetry.Module
	cfg     Config
	runner  *runner
	logger  *slog.Logger
}

func New(d *db.DB, sys *systems.Module, tel *telemetry.Module, cfg Config) *Module {
	m := &Module{db: d, systems: sys, tel: tel, cfg: cfg, logger: slog.Default().With("component", "datasources")}
	m.runner = newRunner(m, m.factory())
	sys.OnDelete(m.beforeSystemDelete)
	return m
}

// Settings returns the module's configuration.
func (m *Module) Settings() Config { return m.cfg }

// HostedAvailable reports whether hosted FTP sources can be created.
func (m *Module) HostedAvailable() bool { return m.cfg.Hosted != nil && m.cfg.Box != nil }

// PollJob is the scheduler job starting the due sources; their polls run in
// the background and the scheduler waits for them at the end of a run.
func (m *Module) PollJob() scheduler.Job {
	return scheduler.Job{
		Name: "data_sources",
		Run: func(ctx context.Context, _ time.Time) error {
			m.runner.startDue(ctx)
			return nil
		},
		Wait: m.runner.wg.Wait,
	}
}

// RunOnce runs ds now and records the outcome; ErrBusy if it is running.
func (m *Module) RunOnce(ctx context.Context, ds DataSource) error { return m.runner.RunOnce(ctx, ds) }

// TryLockJob takes a scheduler job lease (for hostedftp.Service.GC).
func (m *Module) TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (func(), bool, error) {
	return m.db.TryLockJob(ctx, name, ttl, every)
}

// DataSourceIDsByKind returns the IDs of all data sources of the kind.
func (m *Module) DataSourceIDsByKind(ctx context.Context, kind string) ([]uuid.UUID, error) {
	dss, err := m.ListDataSourcesByKind(ctx, kind)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(dss))
	for i, ds := range dss {
		ids[i] = ds.ID
	}
	return ids, nil
}

// HostedGCJob deletes upload accounts without a data source (nil without
// hosted FTP).
func (m *Module) HostedGCJob() *scheduler.Job {
	if m.cfg.Hosted == nil {
		return nil
	}
	return &scheduler.Job{Name: "hosted_ftp_gc", Run: m.cfg.Hosted.GC(m)}
}

// hostedTimeout bounds the SFTPGo calls of one request.
const hostedTimeout = 45 * time.Second

// EnsureHostedHook sets up SFTPGo's upload hook (at start; the GC job
// retries hourly). A failure is only logged, so the server starts anyway.
func (m *Module) EnsureHostedHook(ctx context.Context) {
	if m.cfg.Hosted == nil {
		return
	}
	defer errtrack.Recover(ctx, m.logger, "hosted FTP hook setup panicked")
	hctx, cancel := context.WithTimeout(ctx, hostedTimeout)
	defer cancel()
	if err := m.cfg.Hosted.EnsureHook(hctx); err != nil {
		m.logger.Warn("set up the hosted FTP upload hook; the hosted_ftp_gc job retries", "error", err)
		return
	}
	m.logger.Info("hosted FTP upload hook set up")
}

// beforeSystemDelete closes the upload accounts of a deleted system's
// hosted sources once the system (and with it the sources) is gone, rather
// than at the next GC.
func (m *Module) beforeSystemDelete(ctx context.Context, systemID uuid.UUID) (func(context.Context), error) {
	sources, err := m.ListDataSources(ctx, systemID)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) {
		for i := range sources {
			if sources[i].Kind == KindSolarLogHosted {
				m.RemoveHostedAccount(ctx, &sources[i])
			}
		}
	}, nil
}

// RemoveHostedAccount deletes the upload account of a deleted source. A
// failure is only logged: GC retries for accounts without a source. Sources
// from the former Storage Box (subaccount_id) have no account here.
func (m *Module) RemoveHostedAccount(ctx context.Context, ds *DataSource) {
	var cfg solarlog.Config
	if err := json.Unmarshal(ds.Config, &cfg); err != nil || cfg.Username == "" || cfg.SubaccountID != 0 || m.cfg.Hosted == nil {
		return
	}
	acc := hostedftp.Account{Username: cfg.Username, HostKey: cfg.HostKey}
	if m.cfg.Box != nil && cfg.PasswordEnc != "" {
		acc.Password, _ = m.cfg.Box.Open(cfg.PasswordEnc)
	}
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostedTimeout)
	defer cancel()
	if err := m.cfg.Hosted.Remove(hctx, acc); err != nil {
		m.logger.Error("delete hosted FTP account; GC retries", "data_source_id", ds.ID, "username", cfg.Username, "error", err)
		return
	}
	var userID uuid.UUID
	if p := auth.FromContext(ctx); p != nil {
		userID = p.UserID
	}
	m.logger.Info("hosted FTP source deleted", "system_id", ds.SystemID, "data_source_id", ds.ID, "user_id", userID)
}

// ImportSolarLogDir imports a local Solar-Log export directory, including
// its full history, into the system (`noonside import solarlog`).
func (m *Module) ImportSolarLogDir(ctx context.Context, systemID uuid.UUID, dir string) error {
	engine := pollerStore{Module: m.systems, tel: m.tel, m: m}
	p := solarlog.NewPoller(engine, m.tel, systemID, true,
		func(context.Context) (storage.Storage, error) { return storage.NewLocal(dir), nil })
	return p.Poll(ctx)
}
