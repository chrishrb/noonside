// Package analyticsapi serves the analytics module's API operations: live
// values, the overview of a period, the dashboard, exports and the public
// share-link views. The analytics module has no tables; it composes the
// other modules' data for reading.
package analyticsapi

import (
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
)

// Modules are the modules analytics reads.
type Modules struct {
	Systems     *systems.Module
	Telemetry   *telemetry.Module
	Weather     *weather.Service
	Prices      *prices.Module
	Alerting    *alerting.Module
	DataSources *datasources.Module
}

type Handler struct {
	systems   *systems.Module
	telemetry *telemetry.Module
	weather   *weather.Service
	prices    *prices.Module
	alerting  *alerting.Module
	sources   *datasources.Module
	authz     httpapi.Authz
	logger    *slog.Logger
	now       func() time.Time
}

func New(m Modules) *Handler {
	return &Handler{
		systems: m.Systems, telemetry: m.Telemetry, weather: m.Weather, prices: m.Prices,
		alerting: m.Alerting, sources: m.DataSources,
		authz:  httpapi.Authz{Roles: m.Systems},
		logger: slog.Default().With("namespace", "api.handler"),
		now:    time.Now,
	}
}
