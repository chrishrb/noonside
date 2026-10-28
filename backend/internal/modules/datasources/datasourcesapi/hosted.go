package datasourcesapi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

// Hosted FTP: a solarlog_hosted source owns a user on noonside's SFTPGo
// server. The device uploads to it over FTP; SFTPGo reports each upload to
// the upload hook, and the scheduler then reads the files over SFTP with
// the same credentials, sealed in the config.

// hostedTimeout bounds the SFTPGo calls of one request.
const hostedTimeout = 45 * time.Second

func (h *Handler) createHostedSource(ctx context.Context, p *auth.Principal, req *generated.DataSourceCreate, systemID uuid.UUID) (*generated.DataSource, error) {
	hosted := h.sources.Settings().Hosted
	if hosted == nil || h.sources.Settings().Box == nil {
		return nil, httpapi.BadRequest("hosted FTP is not available on this server")
	}
	existing, err := h.sources.ListDataSources(ctx, systemID)
	if err != nil {
		return nil, err
	}
	for _, ds := range existing {
		if ds.Kind == datasources.KindSolarLogHosted {
			return nil, httpapi.Conflict("this system already has a hosted FTP data source")
		}
	}
	all, err := h.sources.ListDataSourcesByKind(ctx, datasources.KindSolarLogHosted)
	if err != nil {
		return nil, err
	}
	if limit := hosted.Settings().MaxAccounts; limit > 0 && len(all) >= limit {
		return nil, httpapi.Conflict("no hosted FTP accounts are left on this server")
	}
	backfill := req.Config.BackfillHistory.Or(false)

	// Insert first (disabled, so the scheduler skips it): the account
	// carries the source ID, and GC keeps accounts whose source exists.
	ds, err := h.sources.CreateDataSource(ctx, &datasources.DataSource{
		SystemID:     systemID,
		Kind:         datasources.KindSolarLogHosted,
		Name:         strings.TrimSpace(req.Name),
		Config:       []byte(`{}`),
		PollInterval: time.Duration(req.PollIntervalS.Or(300)) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, hostedTimeout)
	defer cancel()
	acc, err := hosted.Provision(hctx, systemID, ds.ID)
	if err != nil {
		h.logger.Error("create hosted FTP account", "system_id", systemID, "data_source_id", ds.ID, "error", err)
		_ = h.sources.DeleteDataSource(context.WithoutCancel(ctx), systemID, ds.ID)
		return nil, httpapi.BadGateway("could not create the FTP account, try again later")
	}
	sealed, err := h.sources.Settings().Box.Seal(acc.Password)
	if err == nil {
		ds.Config, err = json.Marshal(solarlog.Config{
			Username: acc.Username, PasswordEnc: sealed, HostKey: acc.HostKey, BackfillHistory: backfill,
		})
	}
	saved := ds
	if err == nil {
		ds.Enabled = req.Enabled.Or(true)
		saved, err = h.sources.UpdateDataSource(ctx, ds)
	}
	if err != nil {
		// GC removes the account once the source is gone.
		_ = h.sources.DeleteDataSource(context.WithoutCancel(ctx), systemID, ds.ID)
		return nil, err
	}
	h.logger.Info("hosted FTP source created", "system_id", systemID, "data_source_id", ds.ID, "user_id", p.UserID)
	out := h.toDataSource(saved)
	out.Upload.Value.Password = generated.NewOptString(acc.Password)
	return &out, nil
}

func (h *Handler) ResetDataSourcePassword(ctx context.Context, params generated.ResetDataSourcePasswordParams) (*generated.DataSource, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage)
	if err != nil {
		return nil, err
	}
	ds, err := h.sources.GetDataSource(ctx, params.SystemId, params.DataSourceId)
	if err != nil {
		return nil, err
	}
	if ds.Kind != datasources.KindSolarLogHosted {
		return nil, httpapi.BadRequest("only hosted FTP sources have a password noonside can reset")
	}
	if h.sources.Settings().Hosted == nil || h.sources.Settings().Box == nil {
		return nil, httpapi.BadRequest("hosted FTP is not available on this server")
	}
	var cfg solarlog.Config
	if err := json.Unmarshal(ds.Config, &cfg); err != nil {
		return nil, err
	}
	if cfg.SubaccountID != 0 || cfg.Username == "" {
		return nil, httpapi.Conflict("this hosted FTP source was created on the old server; delete it and create a new one")
	}
	hctx, cancel := context.WithTimeout(ctx, hostedTimeout)
	defer cancel()
	pw, err := h.sources.Settings().Hosted.ResetPassword(hctx, cfg.Username)
	if err != nil {
		h.logger.Error("reset hosted FTP password", "data_source_id", ds.ID, "error", err)
		return nil, httpapi.BadGateway("could not reset the FTP password, try again later")
	}
	if cfg.PasswordEnc, err = h.sources.Settings().Box.Seal(pw); err != nil {
		return nil, err
	}
	if ds.Config, err = json.Marshal(cfg); err != nil {
		return nil, err
	}
	if ds, err = h.sources.UpdateDataSource(ctx, ds); err != nil {
		return nil, err
	}
	h.logger.Info("hosted FTP password reset", "system_id", ds.SystemID, "data_source_id", ds.ID, "user_id", p.UserID)
	out := h.toDataSource(ds)
	out.Upload.Value.Password = generated.NewOptString(pw)
	return &out, nil
}

// HostedFtpHook notes an upload reported by SFTPGo (authenticated by the
// hook token in the security handler), so the scheduler imports the source.
func (h *Handler) HostedFtpHook(ctx context.Context, req *generated.HostedFtpEvent) error {
	found, err := h.sources.MarkHostedUpload(ctx, req.Username)
	if err != nil {
		return err
	}
	h.logger.Debug("hosted FTP upload", "username", req.Username, "event", req.Event.Or(""), "known", found)
	return nil
}

// hostedConfig applies an update: the connection is noonside's, only
// backfill_history can change.
func hostedConfig(stored []byte, in generated.DataSourceConfig) ([]byte, error) {
	if stored == nil {
		return nil, httpapi.BadRequest("hosted FTP sources are created without config")
	}
	if in.Protocol.Set || in.Host.Set || in.Port.Set || in.Username.Set || in.Password.Set || in.Directory.Set || in.HostKey.Set {
		return nil, httpapi.BadRequest("the connection of a hosted FTP source cannot be changed")
	}
	var cfg solarlog.Config
	if err := json.Unmarshal(stored, &cfg); err != nil {
		return nil, err
	}
	if v, ok := in.BackfillHistory.Get(); ok {
		cfg.BackfillHistory = v
	}
	return json.Marshal(cfg)
}

func (h *Handler) hostedOut(ds *datasources.DataSource, cfg solarlog.Config) generated.DataSource {
	out := dataSourceOut(ds, generated.DataSourceConfig{
		HasPassword:     generated.NewOptBool(cfg.PasswordEnc != ""),
		BackfillHistory: generated.NewOptBool(cfg.BackfillHistory),
	})
	st := hostedftp.ParseState(ds.State)
	up := generated.HostedUpload{Username: cfg.Username, Directory: "/", UsedBytes: st.UsedBytes, Locked: st.Locked}
	if h.sources.Settings().Hosted != nil {
		set := h.sources.Settings().Hosted.Settings()
		up.Host, up.Port = set.PublicHost, set.PublicPort
		up.QuotaBytes = set.QuotaBytes
		up.RetentionDays = int(set.Retention / (24 * time.Hour))
	}
	out.Upload = generated.NewOptHostedUpload(up)
	return out
}
