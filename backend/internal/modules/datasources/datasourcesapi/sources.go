// Package datasourcesapi serves the data sources module's API operations.
package datasourcesapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/datasources/pvoutput"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog"
	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

type Handler struct {
	sources *datasources.Module
	authz   httpapi.Authz
	logger  *slog.Logger
}

func New(m *datasources.Module, roles auth.RoleLookup) *Handler {
	return &Handler{sources: m, authz: httpapi.Authz{Roles: roles}, logger: slog.Default().With("namespace", "api.handler")}
}

// --- data sources ----------------------------------------------------------

func (h *Handler) ListDataSources(ctx context.Context, params generated.ListDataSourcesParams) ([]generated.DataSource, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage); err != nil {
		return nil, err
	}
	dss, err := h.sources.ListDataSources(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.DataSource, len(dss))
	for i := range dss {
		out[i] = h.toDataSource(&dss[i])
	}
	return out, nil
}

func (h *Handler) CreateDataSource(ctx context.Context, req *generated.DataSourceCreate, params generated.CreateDataSourceParams) (*generated.DataSource, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage)
	if err != nil {
		return nil, err
	}
	if req.Kind == generated.DataSourceKindSolarlogHosted {
		return h.createHostedSource(ctx, p, req, params.SystemId)
	}
	raw, err := h.buildConfig(string(req.Kind), nil, req.Config)
	if err != nil {
		return nil, err
	}
	ds, err := h.sources.CreateDataSource(ctx, &datasources.DataSource{
		SystemID:     params.SystemId,
		Kind:         string(req.Kind),
		Name:         strings.TrimSpace(req.Name),
		Config:       raw,
		Enabled:      req.Enabled.Or(true),
		PollInterval: time.Duration(req.PollIntervalS.Or(300)) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	out := h.toDataSource(ds)
	return &out, nil
}

func (h *Handler) UpdateDataSource(ctx context.Context, req *generated.DataSourceUpdate, params generated.UpdateDataSourceParams) (*generated.DataSource, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage); err != nil {
		return nil, err
	}
	ds, err := h.sources.GetDataSource(ctx, params.SystemId, params.DataSourceId)
	if err != nil {
		return nil, err
	}
	if v, ok := req.Name.Get(); ok {
		ds.Name = strings.TrimSpace(v)
	}
	if v, ok := req.Enabled.Get(); ok {
		ds.Enabled = v
	}
	if v, ok := req.PollIntervalS.Get(); ok {
		ds.PollInterval = time.Duration(v) * time.Second
	}
	if c, ok := req.Config.Get(); ok {
		if ds.Config, err = h.buildConfig(ds.Kind, ds.Config, c); err != nil {
			return nil, err
		}
	}
	if ds, err = h.sources.UpdateDataSource(ctx, ds); err != nil {
		return nil, err
	}
	out := h.toDataSource(ds)
	return &out, nil
}

func (h *Handler) DeleteDataSource(ctx context.Context, params generated.DeleteDataSourceParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage); err != nil {
		return err
	}
	ds, err := h.sources.GetDataSource(ctx, params.SystemId, params.DataSourceId)
	if err != nil {
		return err
	}
	if err := h.sources.DeleteDataSource(ctx, params.SystemId, params.DataSourceId); err != nil {
		return err
	}
	if ds.Kind == datasources.KindSolarLogHosted {
		h.sources.RemoveHostedAccount(ctx, ds)
	}
	return nil
}

func (h *Handler) RunDataSource(ctx context.Context, params generated.RunDataSourceParams) (*generated.DataSource, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataSourceManage); err != nil {
		return nil, err
	}
	ds, err := h.sources.GetDataSource(ctx, params.SystemId, params.DataSourceId)
	if err != nil {
		return nil, err
	}
	// The outcome is recorded on the data source and returned below.
	if err := h.sources.RunOnce(ctx, *ds); errors.Is(err, datasources.ErrBusy) {
		return nil, httpapi.Conflict(err.Error())
	}
	if ds, err = h.sources.GetDataSource(ctx, params.SystemId, params.DataSourceId); err != nil {
		return nil, err
	}
	out := h.toDataSource(ds)
	return &out, nil
}

// buildConfig applies an API config onto the stored one (nil for a new
// source) and returns the JSON to store.
func (h *Handler) buildConfig(kind string, stored []byte, in generated.DataSourceConfig) ([]byte, error) {
	if kind == datasources.KindSolarLogHosted {
		return hostedConfig(stored, in)
	}
	if kind == datasources.KindPVOutputExport {
		var cur pvoutput.Config
		if stored != nil {
			if err := json.Unmarshal(stored, &cur); err != nil {
				return nil, err
			}
		}
		cfg, err := h.mergePVOutputConfig(cur, in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	}
	var cur solarlog.Config
	if stored != nil {
		if err := json.Unmarshal(stored, &cur); err != nil {
			return nil, err
		}
	}
	cfg, err := h.mergeConfig(kind, cur, in)
	if err != nil {
		return nil, err
	}
	return json.Marshal(cfg)
}

func (h *Handler) seal(v, field string) (string, error) {
	if v == "" {
		return "", nil
	}
	if h.sources.Settings().Box == nil {
		return "", httpapi.BadRequest(field + ": server has no secret key configured; cannot store secrets")
	}
	return h.sources.Settings().Box.Seal(v)
}

// mergePVOutputConfig applies an API config onto cur, encrypting the key.
func (h *Handler) mergePVOutputConfig(cur pvoutput.Config, in generated.DataSourceConfig) (pvoutput.Config, error) {
	if v, ok := in.PvoutputSystemID.Get(); ok {
		cur.SystemID = strings.TrimSpace(v)
	}
	if v, ok := in.APIKey.Get(); ok {
		sealed, err := h.seal(strings.TrimSpace(v), "config.api_key")
		if err != nil {
			return cur, err
		}
		cur.APIKeyEnc = sealed
	}
	if cur.SystemID == "" || strings.Trim(cur.SystemID, "0123456789") != "" {
		return cur, httpapi.BadRequest("config.pvoutput_system_id must be the numeric PVOutput system ID")
	}
	if cur.APIKeyEnc == "" {
		return cur, httpapi.BadRequest("config.api_key is required")
	}
	return cur, nil
}

// mergeConfig applies an API config onto cur, encrypting the password.
func (h *Handler) mergeConfig(kind string, cur solarlog.Config, in generated.DataSourceConfig) (solarlog.Config, error) {
	if v, ok := in.Host.Get(); ok {
		cur.Host = strings.TrimSpace(v)
	}
	if v, ok := in.Port.Get(); ok {
		cur.Port = v
	}
	if v, ok := in.Username.Get(); ok {
		cur.Username = v
	}
	if v, ok := in.Directory.Get(); ok {
		cur.Directory = strings.TrimSpace(v)
	}
	if v, ok := in.Protocol.Get(); ok {
		cur.Protocol = string(v)
	}
	if v, ok := in.HostKey.Get(); ok {
		cur.HostKey = strings.TrimSpace(v)
	}
	if v, ok := in.BackfillHistory.Get(); ok {
		cur.BackfillHistory = v
	}
	if v, ok := in.Password.Get(); ok {
		if v == "" {
			cur.PasswordEnc = ""
		} else {
			if h.sources.Settings().Box == nil {
				return cur, httpapi.BadRequest("server has no secret key configured; cannot store passwords")
			}
			sealed, err := h.sources.Settings().Box.Seal(v)
			if err != nil {
				return cur, err
			}
			cur.PasswordEnc = sealed
		}
	}
	switch kind {
	case datasources.KindSolarLogRemote:
		if cur.Protocol == "" {
			return cur, httpapi.BadRequest("config.protocol is required")
		}
		if cur.Host == "" {
			return cur, httpapi.BadRequest("config.host is required")
		}
		if cur.Protocol == solarlog.ProtocolSFTP {
			if _, err := storage.ParseHostKeys(cur.HostKey); err != nil {
				return cur, httpapi.BadRequest("config.host_key: " + err.Error())
			}
		} else {
			cur.HostKey = ""
		}
	}
	return cur, nil
}

func (h *Handler) toDataSource(ds *datasources.DataSource) generated.DataSource {
	if ds.Kind == datasources.KindPVOutputExport {
		var cfg pvoutput.Config
		_ = json.Unmarshal(ds.Config, &cfg)
		out := dataSourceOut(ds, generated.DataSourceConfig{
			PvoutputSystemID: generated.NewOptString(cfg.SystemID), HasAPIKey: generated.NewOptBool(cfg.APIKeyEnc != ""),
		})
		if st := pvoutput.ParseState(ds.State); !st.StatusUntil.IsZero() {
			out.UploadedUntil = generated.NewOptDateTime(st.StatusUntil)
		}
		return out
	}
	var cfg solarlog.Config
	_ = json.Unmarshal(ds.Config, &cfg)
	if ds.Kind == datasources.KindSolarLogHosted {
		return h.hostedOut(ds, cfg)
	}
	c := generated.DataSourceConfig{
		HasPassword:     generated.NewOptBool(cfg.PasswordEnc != ""),
		BackfillHistory: generated.NewOptBool(cfg.BackfillHistory),
	}
	if cfg.Protocol != "" {
		c.Protocol = generated.NewOptSolarLogProtocol(generated.SolarLogProtocol(cfg.Protocol))
	}
	if cfg.Host != "" {
		c.Host = generated.NewOptString(cfg.Host)
	}
	if cfg.Port != 0 {
		c.Port = generated.NewOptInt(cfg.Port)
	}
	if cfg.Username != "" {
		c.Username = generated.NewOptString(cfg.Username)
	}
	if cfg.Directory != "" {
		c.Directory = generated.NewOptString(cfg.Directory)
	}
	if cfg.HostKey != "" {
		c.HostKey = generated.NewOptString(cfg.HostKey)
	}
	return dataSourceOut(ds, c)
}

func dataSourceOut(ds *datasources.DataSource, c generated.DataSourceConfig) generated.DataSource {
	return generated.DataSource{
		ID:            ds.ID,
		Kind:          generated.DataSourceKind(ds.Kind),
		Name:          ds.Name,
		Enabled:       ds.Enabled,
		PollIntervalS: int(ds.PollInterval.Seconds()),
		Config:        c,
		LastRunAt:     httpapi.OptTime(ds.LastRunAt),
		LastSuccessAt: httpapi.OptTime(ds.LastSuccessAt),
		LastError:     ds.LastError,
	}
}
