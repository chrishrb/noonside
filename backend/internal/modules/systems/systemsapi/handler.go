// Package systemsapi serves the systems module's API operations: systems,
// planes, targets, inverters, members, invitations, API keys, share links
// and the address search.
package systemsapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/systems/pvgis"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
)

// TargetEstimator estimates monthly yields (implemented by pvgis.Client).
type TargetEstimator interface {
	Monthly(ctx context.Context, lat, lon float64, planes []pvgis.Plane, kwp float64) ([12]float64, error)
}

// Mailer sends transactional emails (implemented by email.Mailer).
type Mailer interface {
	Send(ctx context.Context, to string, e email.Email) error
	Enabled() bool
}

type Config struct {
	// PublicURL is the frontend base URL used in invitation and share links.
	PublicURL string
	// InvitationTTL is the lifetime of invitations.
	InvitationTTL time.Duration
	// Geocoder looks up system addresses (nil: disabled; addresses are then
	// not validated).
	Geocoder systems.Geocoder
	// Targets estimates monthly targets (nil: disabled).
	Targets TargetEstimator
}

type Handler struct {
	systems  *systems.Module
	identity *identity.Module
	mailer   Mailer
	authz    httpapi.Authz
	cfg      Config
	logger   *slog.Logger
	now      func() time.Time
}

func New(sys *systems.Module, id *identity.Module, mailer Mailer, cfg Config) *Handler {
	if cfg.InvitationTTL == 0 {
		cfg.InvitationTTL = 7 * 24 * time.Hour
	}
	return &Handler{
		systems: sys, identity: id, mailer: mailer, cfg: cfg,
		authz:  httpapi.Authz{Roles: sys},
		logger: slog.Default().With("namespace", "api.handler"),
		now:    time.Now,
	}
}
