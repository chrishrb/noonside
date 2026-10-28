// Package app is the composition root: it builds the modules from
// configuration, wires them to each other, composes the API handler from the
// modules' handlers and registers the modules' scheduler jobs.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/advice/adviceapi"
	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/alerting/alertingapi"
	"github.com/chrishrb/noonside/internal/modules/analytics/analyticsapi"
	"github.com/chrishrb/noonside/internal/modules/datasources"
	"github.com/chrishrb/noonside/internal/modules/datasources/datasourcesapi"
	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/identity/identityapi"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/notifications/notificationsapi"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/prices/pricesapi"
	"github.com/chrishrb/noonside/internal/modules/reports"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/systems/systemsapi"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/modules/telemetry/telemetryapi"
	"github.com/chrishrb/noonside/internal/modules/waitlist"
	"github.com/chrishrb/noonside/internal/modules/waitlist/waitlistapi"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/modules/weather/weatherapi"
	"github.com/chrishrb/noonside/internal/modules/webhooks"
	"github.com/chrishrb/noonside/internal/modules/webhooks/webhooksapi"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/chrishrb/noonside/internal/router"
)

// Mailer sends transactional emails (implemented by email.Mailer).
type Mailer interface {
	Send(ctx context.Context, to string, e email.Email) error
	Enabled() bool
}

// Deps are the app's external services and settings. Nil services disable
// their feature.
type Deps struct {
	Tokens TokenVerifier
	Admin  identity.UserAdmin
	Mailer Mailer
	// Pusher sends Web Push (nil: disabled); VAPIDPublicKey is handed to
	// browsers to subscribe.
	Pusher         notifications.Pusher
	VAPIDPublicKey string
	// Box seals secrets at rest (nil: secrets cannot be stored).
	Box *secretbox.Box
	// Notify posts to users' ntfy topics and webhooks and to the systems'
	// webhooks.
	Notify      *notify.Client
	Weather     *weather.Client
	Geocoder    systems.Geocoder
	Targets     systemsapi.TargetEstimator
	PriceSource prices.Source
	// Hosted manages hosted FTP upload accounts.
	Hosted *hostedftp.Service
	// Media stores profile and system pictures (nil: no uploads).
	Media media.Store
	// AllowPrivateHosts lets data sources, ntfy and webhooks reach private
	// and local addresses.
	AllowPrivateHosts    bool
	PVOutputURL          string
	PublicURL            string
	LandingURL           string
	InvitationTTL        time.Duration
	WaitlistMailsPerHour int
	CORSAllowedOrigins   []string
	SPA                  router.SPA
	RateLimit            router.RateLimitConfig
}

// Modules are the app's modules.
type Modules struct {
	Identity      *identity.Module
	Systems       *systems.Module
	Telemetry     *telemetry.Module
	Weather       *weather.Service
	Prices        *prices.Module
	Advice        *advice.Service
	DataSources   *datasources.Module
	Notifications *notifications.Module
	Alerting      *alerting.Module
	Reports       *reports.Service
	Webhooks      *webhooks.Module
	Waitlist      *waitlist.Module

	// Engine evaluates the alert rules; Hooks queues the systems' webhook
	// events.
	Engine *alerting.Engine
	Hooks  *webhooks.Service
}

// NewModules builds the modules on d and wires them to each other.
func NewModules(d *db.DB, deps Deps) *Modules {
	m := &Modules{}
	m.Identity = identity.New(d, deps.Admin)
	m.Systems = systems.New(d, m.Identity)
	m.Identity.SetMemberships(m.Systems)
	m.Identity.SetMedia(deps.Media)
	m.Systems.SetMedia(deps.Media)
	m.Telemetry = telemetry.New(d, m.Systems)
	m.Weather = weather.NewService(deps.Weather, d, m.Systems, m.Telemetry)
	m.Prices = prices.New(d, m.Systems, m.Telemetry, deps.PriceSource)
	m.Advice = advice.NewService(m.Weather, m.Prices)
	m.DataSources = datasources.New(d, m.Systems, m.Telemetry, datasources.Config{
		Box: deps.Box, AllowPrivateHosts: deps.AllowPrivateHosts, PVOutputURL: deps.PVOutputURL, Hosted: deps.Hosted,
	})
	m.Notifications = notifications.New(d, m.Systems, m.Identity)
	m.Alerting = alerting.New(d, m.Systems, m.Telemetry, m.DataSources, m.Notifications)
	m.Reports = reports.NewService(reports.ModuleStore{
		DB: d, Module: m.Systems, Telemetry: m.Telemetry, Alerting: m.Alerting, Notifications: m.Notifications,
	}, m.Prices, m.Weather.ExpectedDays)
	m.Webhooks = webhooks.New(d)
	m.Hooks = webhooks.NewService(m.WebhookStore(), m.Reports, m.Advice, deps.PublicURL)
	m.Engine = alerting.NewEngine(m.Alerting.Store()).WithExpected(m.Weather.ExpectedDays).WithHook(m.Hooks.Alert)
	m.Waitlist = waitlist.New(d)
	return m
}

// WebhookStore is what the webhook service and sender run on.
func (m *Modules) WebhookStore() webhooks.ModuleStore {
	return webhooks.ModuleStore{Module: m.Webhooks, Systems: m.Systems, Telemetry: m.Telemetry}
}

// App is the wired application.
type App struct {
	*Modules
	DB        *db.DB
	Scheduler *scheduler.Scheduler
	Handler   http.Handler
}

// Aliases name the modules' handlers so they can be embedded side by side.
type (
	identityAPI      = identityapi.Handler
	systemsAPI       = systemsapi.Handler
	telemetryAPI     = telemetryapi.Handler
	weatherAPI       = weatherapi.Handler
	pricesAPI        = pricesapi.Handler
	adviceAPI        = adviceapi.Handler
	dataSourcesAPI   = datasourcesapi.Handler
	notificationsAPI = notificationsapi.Handler
	alertingAPI      = alertingapi.Handler
	webhooksAPI      = webhooksapi.Handler
	waitlistAPI      = waitlistapi.Handler
	analyticsAPI     = analyticsapi.Handler
)

// Build wires the modules, their scheduler jobs and the HTTP handler on d.
func Build(d *db.DB, deps Deps) (*App, error) {
	m := NewModules(d, deps)
	a := &App{Modules: m, DB: d, Scheduler: scheduler.New()}

	// A nil pointer must stay a nil interface.
	var (
		opener       notifications.Opener
		hookBox      webhooks.Opener
		targets      notifications.TargetSender
		poster       webhooks.Poster
		notifier     notificationsapi.TargetNotifier
		hookNotifier webhooksapi.Notifier
	)
	if deps.Box != nil {
		opener, hookBox = deps.Box, deps.Box
	}
	if deps.Notify != nil {
		targets, poster, notifier, hookNotifier = deps.Notify, deps.Notify, deps.Notify, deps.Notify
	}
	dispatcher := notifications.NewDispatcher(m.Notifications, deps.Mailer, deps.Pusher, deps.PublicURL).
		WithReports(reports.RenderEmail(deps.PublicURL)).
		WithTargets(targets, opener)
	forecaster := alerting.NewForecaster(m.Alerting.Store(), m.Weather.ExpectedDays).WithAdvice(m.Advice.Today)

	// Data sources first (their polls run in the background while the
	// others run), then the singleton jobs in this order.
	a.Scheduler.Add(
		m.DataSources.PollJob(),
		m.Telemetry.RetentionJob(),
		m.Prices.Job(),
		scheduler.Job{Name: "alerts", Run: m.Engine.Run},
		scheduler.Job{Name: "forecast_push", Run: forecaster.Run},
		m.Reports.Job(),
		m.Hooks.Job(),
		notifications.DispatchJob(dispatcher),
		webhooks.NewSender(m.WebhookStore(), poster, hookBox, deps.PublicURL).Job(),
		m.Notifications.PruneJob(),
		m.Alerting.PruneJob(),
		m.Telemetry.RejectionsPruneJob(),
		m.Waitlist.PruneJob(),
		m.Weather.Backfill(),
	)
	if j := m.DataSources.HostedGCJob(); j != nil {
		a.Scheduler.Add(*j)
	}
	if deps.Geocoder != nil {
		a.Scheduler.Add(m.Systems.GeocodeBackfill(deps.Geocoder, systems.BackfillPause))
	}

	handler := &apiHandler{
		identityAPI: identityapi.New(m.Identity, m.Systems, identityapi.Config{
			Features:  generated.Features{HostedFtp: m.DataSources.HostedAvailable()},
			PublicURL: deps.PublicURL,
		}),
		systemsAPI: systemsapi.New(m.Systems, m.Identity, deps.Mailer, systemsapi.Config{
			PublicURL: deps.PublicURL, InvitationTTL: deps.InvitationTTL, Geocoder: deps.Geocoder, Targets: deps.Targets,
		}),
		telemetryAPI:   telemetryapi.New(m.Telemetry, m.Systems),
		weatherAPI:     weatherapi.New(m.Weather, m.Systems, m.Systems),
		pricesAPI:      pricesapi.New(m.Prices, m.Systems, m.Systems),
		adviceAPI:      adviceapi.New(m.Advice, m.Systems, m.Systems),
		dataSourcesAPI: datasourcesapi.New(m.DataSources, m.Systems),
		notificationsAPI: notificationsapi.New(m.Notifications, m.Identity, m.Systems, deps.Mailer, notificationsapi.Config{
			PublicURL: deps.PublicURL, Pusher: deps.Pusher, VAPIDPublicKey: deps.VAPIDPublicKey, Notify: notifier, Box: deps.Box,
		}),
		alertingAPI: alertingapi.New(m.Alerting, m.Systems, m.DataSources, m.Identity),
		webhooksAPI: webhooksapi.New(m.Webhooks, m.Systems, m.Identity, webhooksapi.Config{
			PublicURL: deps.PublicURL, Notify: hookNotifier, Box: deps.Box,
		}),
		waitlistAPI: waitlistapi.New(m.Waitlist, deps.Mailer, waitlistapi.Config{
			PublicURL: deps.PublicURL, LandingURL: deps.LandingURL, WaitlistMailsPerHour: deps.WaitlistMailsPerHour,
		}),
		analyticsAPI: analyticsapi.New(analyticsapi.Modules{
			Systems: m.Systems, Telemetry: m.Telemetry, Weather: m.Weather, Prices: m.Prices,
			Alerting: m.Alerting, DataSources: m.DataSources,
		}),
		security: &security{tokens: deps.Tokens, systems: m.Systems, hosted: deps.Hosted},
	}
	h, err := router.NewRouter(handler, d.Ping, deps.CORSAllowedOrigins, deps.SPA)
	if err != nil {
		return nil, err
	}
	a.Handler = router.WithRateLimit(h, deps.RateLimit)
	return a, nil
}

func (a *App) Close() { a.DB.Close() }
