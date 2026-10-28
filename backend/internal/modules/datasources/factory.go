package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/adapter"
	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/datasources/pvoutput"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/google/uuid"
)

// pollerStore is what the pollers read and write: the systems and
// telemetry modules plus the source's state.
type pollerStore struct {
	*systems.Module
	tel *telemetry.Module
	m   *Module
}

func (s pollerStore) LastMeasurementTime(ctx context.Context, systemID uuid.UUID) (*time.Time, error) {
	return s.tel.LastMeasurementTime(ctx, systemID)
}

func (s pollerStore) PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error) {
	return s.tel.PowerCurve(ctx, systemID, from, to, bucket)
}

func (s pollerStore) Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error) {
	return s.tel.Energy(ctx, systemID, granularity, from, to)
}

func (s pollerStore) SetDataSourceState(ctx context.Context, id uuid.UUID, state []byte) error {
	return s.m.SetDataSourceState(ctx, id, state)
}

// factory builds pollers for all supported data source kinds. Remote data
// sources may only connect to private or local addresses if
// AllowPrivateHosts is set.
func (m *Module) factory() Factory {
	engine := pollerStore{Module: m.systems, tel: m.tel, m: m}
	ing := m.tel
	box, allowPrivateHosts, pvoutputURL, hosted := m.cfg.Box, m.cfg.AllowPrivateHosts, m.cfg.PVOutputURL, m.cfg.Hosted
	return func(ds DataSource) (adapter.Poller, error) {
		if ds.Kind == KindPVOutputExport {
			var cfg pvoutput.Config
			if err := json.Unmarshal(ds.Config, &cfg); err != nil {
				return nil, fmt.Errorf("invalid config: %w", err)
			}
			key, err := openSecret(box, cfg.APIKeyEnc, "API key")
			if err != nil {
				return nil, err
			}
			return pvoutput.New(engine, pvoutputURL, pvoutput.Source{ID: ds.ID, SystemID: ds.SystemID, State: ds.State}, cfg, key), nil
		}
		var cfg solarlog.Config
		if err := json.Unmarshal(ds.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
		switch ds.Kind {
		case KindSolarLogRemote:
			password, err := openPassword(box, cfg)
			if err != nil {
				return nil, err
			}
			var open func(ctx context.Context) (storage.Storage, error)
			switch cfg.Protocol {
			case solarlog.ProtocolFTP, solarlog.ProtocolFTPS:
				open = func(ctx context.Context) (storage.Storage, error) {
					return storage.DialFTP(ctx, storage.FTPConfig{
						Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: password,
						Directory: cfg.Directory, TLS: cfg.Protocol == solarlog.ProtocolFTPS,
						AllowPrivate: allowPrivateHosts,
					})
				}
			case solarlog.ProtocolSFTP:
				open = func(ctx context.Context) (storage.Storage, error) {
					return storage.DialSFTP(ctx, storage.SFTPConfig{
						Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: password,
						Directory: cfg.Directory, HostKey: cfg.HostKey,
						AllowPrivate: allowPrivateHosts,
					})
				}
			default:
				return nil, fmt.Errorf("unsupported protocol %q", cfg.Protocol)
			}
			return solarlog.NewPoller(engine, ing, ds.SystemID, cfg.BackfillHistory, open), nil
		case KindSolarLogHosted:
			if hosted == nil {
				return nil, fmt.Errorf("hosted FTP is not configured on this server")
			}
			if cfg.SubaccountID != 0 {
				return nil, errLegacyHosted
			}
			password, err := openPassword(box, cfg)
			if err != nil {
				return nil, err
			}
			acc := hostedftp.Account{Username: cfg.Username, Password: password, HostKey: cfg.HostKey}
			open := func(ctx context.Context) (storage.Storage, error) {
				conn, err := hosted.Dial(ctx, acc)
				if err != nil {
					return nil, err
				}
				return conn, nil
			}
			prev := hostedftp.ParseState(ds.State)
			around := func(ctx context.Context, clean func(context.Context) (int64, error)) error {
				st, err := hosted.Check(ctx, prev, clean, time.Now())
				if raw, mErr := json.Marshal(st); mErr == nil {
					if sErr := m.SetDataSourceState(ctx, ds.ID, raw); sErr != nil {
						err = errors.Join(err, sErr)
					}
				}
				return err
			}
			lim := solarlog.Limits{Retention: hosted.Settings().Retention, ForeignGrace: 24 * time.Hour}
			return solarlog.NewPoller(engine, ing, ds.SystemID, cfg.BackfillHistory, open).WithHousekeeping(lim, around), nil
		}
		return nil, fmt.Errorf("unsupported data source kind %q", ds.Kind)
	}
}

// errLegacyHosted fails the sources created on the former Hetzner Storage
// Box, whose accounts are gone.
var errLegacyHosted = errors.New("this hosted FTP source was created on the old server; delete it and create a new one")

func openPassword(box *secretbox.Box, cfg solarlog.Config) (string, error) {
	return openSecret(box, cfg.PasswordEnc, "password")
}

func openSecret(box *secretbox.Box, sealed, what string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if box == nil {
		return "", fmt.Errorf("secret key not configured")
	}
	v, err := box.Open(sealed)
	if err != nil {
		return "", fmt.Errorf("decrypt %s: %w", what, err)
	}
	return v, nil
}
