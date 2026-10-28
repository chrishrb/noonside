package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/systems/geocode"
	"github.com/chrishrb/noonside/internal/modules/systems/pvgis"
	"github.com/chrishrb/noonside/internal/modules/systems/systemsapi"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/chrishrb/noonside/internal/platform/supabase"
	"github.com/chrishrb/noonside/internal/router"
	"github.com/spf13/viper"
)

// New connects to the database and builds the app from the configuration.
func New(ctx context.Context) (*App, error) {
	d, err := OpenDB(ctx)
	if err != nil {
		return nil, err
	}
	deps, err := NewDeps(ctx)
	if err != nil {
		d.Close()
		return nil, err
	}
	a, err := Build(d, *deps)
	if err != nil {
		d.Close()
		return nil, err
	}
	return a, nil
}

// OpenDB connects to DATABASE_URL.
func OpenDB(ctx context.Context) (*db.DB, error) {
	databaseURL := viper.GetString("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}
	return db.Open(ctx, databaseURL)
}

// NewDeps builds the external services from the configuration.
func NewDeps(ctx context.Context) (*Deps, error) {
	box, err := secretBox()
	if err != nil {
		return nil, err
	}
	verifier, err := auth.NewVerifier(ctx, auth.VerifierConfig{
		URL:       viper.GetString("SUPABASE_URL"),
		AuthURL:   viper.GetString("SUPABASE_INTERNAL_URL"),
		JWTSecret: viper.GetString("SUPABASE_JWT_SECRET"),
	})
	if err != nil {
		return nil, err
	}
	admin, err := NewSupabaseAdmin()
	if err != nil {
		return nil, err
	}
	mailer, err := NewMailer()
	if err != nil {
		return nil, err
	}
	hosted, err := NewHosted(box)
	if err != nil {
		return nil, err
	}
	pusher, err := NewPusher()
	if err != nil {
		return nil, err
	}
	store, mediaDir, err := NewMedia()
	if err != nil {
		return nil, err
	}
	allowPrivate := viper.GetBool("ALLOW_PRIVATE_DATA_SOURCE_HOSTS")
	deps := &Deps{
		Tokens: verifier, Admin: admin, Mailer: mailer,
		Pusher: pusher, VAPIDPublicKey: pusher.PublicKey(),
		Box: box,
		// ntfy topics and webhooks are user-supplied URLs the server posts
		// to; the same switch as for data sources allows private hosts.
		Notify:               notify.New(allowPrivate, "noonside (+"+viper.GetString("PUBLIC_URL")+")"),
		Weather:              NewWeatherClient(),
		Targets:              newTargetEstimator(),
		PriceSource:          NewPriceSource(),
		Hosted:               hosted,
		Media:                store,
		AllowPrivateHosts:    allowPrivate,
		PVOutputURL:          viper.GetString("PVOUTPUT_BASE_URL"),
		PublicURL:            viper.GetString("PUBLIC_URL"),
		LandingURL:           viper.GetString("LANDING_URL"),
		InvitationTTL:        viper.GetDuration("INVITATION_TTL"),
		WaitlistMailsPerHour: viper.GetInt("WAITLIST_MAILS_PER_HOUR"),
		CORSAllowedOrigins:   viper.GetStringSlice("CORS_ALLOWED_ORIGINS"),
		SPA: router.SPA{
			SentryDSN: viper.GetString("SENTRY_WEB_DSN"), Media: mediaDir, AuthTemplates: ConfigPath("AUTH_TEMPLATES_DIR"),
		},
		RateLimit: router.RateLimitConfig{
			TrustedProxyHops: viper.GetInt("TRUSTED_PROXY_HOPS"),
			Disabled:         !viper.GetBool("RATE_LIMIT_ENABLED"),
		},
	}
	// A nil *geocode.Client must stay a nil systems.Geocoder.
	if g := NewGeocoder(); g != nil {
		deps.Geocoder = g
	}
	return deps, nil
}

// NewMedia builds the picture store: the directory MEDIA_DIR (served by the
// router; relative to the config directory), else none (uploads off).
func NewMedia() (media.Store, *media.Dir, error) {
	dir := ConfigPath("MEDIA_DIR")
	if dir == "" {
		slog.Warn("MEDIA_DIR is not set; picture uploads are disabled")
		return nil, nil, nil
	}
	d, err := media.NewDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("media: %w", err)
	}
	return d, d, nil
}

// NewMailer builds the mailer from SMTP_* / MAIL_* settings. Without
// SMTP_HOST emails are not delivered.
func NewMailer() (*email.Mailer, error) {
	m, err := email.New(email.Config{
		SMTP: email.SMTPConfig{
			Host:     viper.GetString("SMTP_HOST"),
			Port:     viper.GetInt("SMTP_PORT"),
			Username: viper.GetString("SMTP_USERNAME"),
			Password: viper.GetString("SMTP_PASSWORD"),
			TLS:      viper.GetString("SMTP_TLS"),
		},
		From:     viper.GetString("MAIL_FROM"),
		FromName: viper.GetString("MAIL_FROM_NAME"),
		App:      email.App{Name: "noonside", URL: viper.GetString("PUBLIC_URL")},
	})
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	if !m.Enabled() {
		slog.Warn("SMTP_HOST is not set; emails are not delivered")
	}
	return m, nil
}

// NewPusher builds the Web Push client; without VAPID_PRIVATE_KEY push is
// disabled.
func NewPusher() (*push.Client, error) {
	subject := viper.GetString("VAPID_SUBJECT")
	if subject == "" {
		subject = viper.GetString("MAIL_FROM")
	}
	c, err := push.New(viper.GetString("VAPID_PUBLIC_KEY"), viper.GetString("VAPID_PRIVATE_KEY"), subject)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	if !c.Enabled() {
		slog.Warn("VAPID_PRIVATE_KEY is not set; push notifications are disabled")
	}
	return c, nil
}

// NewWeatherClient builds the Open-Meteo client; weather_enabled=false or an
// empty weather_base_url disables weather (nil). OPEN_METEO_API_KEY is only
// needed for commercial use (the free API is non-commercial).
func NewWeatherClient() *weather.Client {
	base := viper.GetString("WEATHER_BASE_URL")
	if !viper.GetBool("WEATHER_ENABLED") || base == "" {
		return nil
	}
	return weather.NewClient(weather.Config{
		ForecastURL:   base,
		HistoricalURL: viper.GetString("WEATHER_HISTORICAL_URL"),
		ArchiveURL:    viper.GetString("WEATHER_ARCHIVE_URL"),
		APIKey:        viper.GetString("OPEN_METEO_API_KEY"),
	})
}

// NewPriceSource builds the day-ahead price source: ENTSO-E with
// ENTSOE_API_TOKEN, otherwise aWATTar; prices_enabled=false disables it.
func NewPriceSource() prices.Source {
	if !viper.GetBool("PRICES_ENABLED") {
		return nil
	}
	if token := viper.GetString("ENTSOE_API_TOKEN"); token != "" {
		return prices.NewEntsoE(viper.GetString("ENTSOE_BASE_URL"), token)
	}
	de, at := viper.GetString("AWATTAR_DE_URL"), viper.GetString("AWATTAR_AT_URL")
	if de == "" && at == "" {
		return nil
	}
	return prices.NewAwattar(de, at)
}

// NewGeocoder builds the address search; an empty geocode_base_url disables
// it (addresses are then not validated).
func NewGeocoder() *geocode.Client {
	base := viper.GetString("GEOCODE_BASE_URL")
	if base == "" {
		return nil
	}
	return geocode.NewClient(base, "noonside ("+viper.GetString("PUBLIC_URL")+")")
}

// newTargetEstimator builds the PVGIS client; an empty pvgis_base_url
// disables target estimates. A nil *pvgis.Client must stay a nil interface.
func newTargetEstimator() systemsapi.TargetEstimator {
	base := viper.GetString("PVGIS_BASE_URL")
	if base == "" {
		return nil
	}
	return pvgis.NewClient(base)
}

// NewHosted builds the SFTPGo service for hosted FTP uploads; it is nil
// (feature off) without HOSTED_FTP_API_URL and SFTPGO_ADMIN_PASSWORD. Hosted
// sources store their password sealed and the upload hook's token is
// derived from SECRET_KEY, so they also need it.
func NewHosted(box *secretbox.Box) (*hostedftp.Service, error) {
	apiURL, password := viper.GetString("HOSTED_FTP_API_URL"), viper.GetString("SFTPGO_ADMIN_PASSWORD")
	if apiURL == "" || password == "" {
		return nil, nil
	}
	if box == nil {
		slog.Warn("SECRET_KEY is not set; hosted FTP is disabled")
		return nil, nil
	}
	c, err := hostedftp.NewClient(apiURL, viper.GetString("HOSTED_FTP_ADMIN_USER"), password)
	if err != nil {
		return nil, err
	}
	hookURL := viper.GetString("HOSTED_FTP_HOOK_URL")
	if hookURL == "" {
		hookURL = strings.TrimRight(viper.GetString("PUBLIC_URL"), "/") + "/api/v1/hooks/hosted-ftp"
	}
	set := hostedftp.Settings{
		QuotaBytes:  viper.GetInt64("HOSTED_FTP_QUOTA_MB") << 20,
		MaxAccounts: viper.GetInt("HOSTED_FTP_MAX_ACCOUNTS"),
		Retention:   time.Duration(viper.GetInt("HOSTED_FTP_RETENTION_DAYS")) * 24 * time.Hour,
		HomeBase:    viper.GetString("HOSTED_FTP_HOME_BASE"),
		SFTPAddr:    viper.GetString("HOSTED_FTP_SFTP_ADDR"),
		HostKey:     viper.GetString("HOSTED_FTP_HOST_KEY"),
		PublicHost:  viper.GetString("HOSTED_FTP_PUBLIC_HOST"),
		PublicPort:  viper.GetInt("HOSTED_FTP_PUBLIC_PORT"),
		HookURL:     hookURL,
		HookToken:   hostedftp.HookToken(viper.GetString("SECRET_KEY")),
	}
	if set.SFTPAddr == "" || set.PublicHost == "" || !filepath.IsAbs(set.HomeBase) {
		return nil, fmt.Errorf("hosted FTP needs hosted_ftp_sftp_addr, hosted_ftp_public_host and an absolute hosted_ftp_home_base")
	}
	return hostedftp.NewService(c, set), nil
}

func secretBox() (*secretbox.Box, error) {
	key := viper.GetString("SECRET_KEY")
	if key == "" {
		slog.Warn("SECRET_KEY is not set; data source passwords cannot be stored")
		return nil, nil
	}
	return secretbox.New(key)
}

// NewSupabaseAdmin creates the Auth admin client from SUPABASE_URL (or
// SUPABASE_INTERNAL_URL) and SUPABASE_SECRET_KEY.
func NewSupabaseAdmin() (*supabase.Admin, error) {
	a, err := supabase.NewAdmin(AuthURL(), viper.GetString("SUPABASE_SECRET_KEY"))
	if err != nil {
		return nil, fmt.Errorf("supabase admin: %w (set SUPABASE_URL and SUPABASE_SECRET_KEY)", err)
	}
	return a, nil
}

// AuthURL is where the server reaches the Auth server (GoTrue):
// SUPABASE_INTERNAL_URL inside the cluster, else <SUPABASE_URL>/auth/v1
// through the gateway (or the Vite proxy in dev).
func AuthURL() string {
	if u := viper.GetString("SUPABASE_INTERNAL_URL"); u != "" {
		return u
	}
	if u := viper.GetString("SUPABASE_URL"); u != "" {
		return strings.TrimRight(u, "/") + "/auth/v1"
	}
	return ""
}

// ConfigPath reads a path setting; a relative path is relative to the
// config directory (backend/config), so commands work from any directory.
func ConfigPath(key string) string {
	p := viper.GetString(key)
	if p == "" || filepath.IsAbs(p) || config.Dir == "" {
		return p
	}
	return filepath.Join(config.Dir, p)
}
