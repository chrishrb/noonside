package cmd

import (
	"cmp"
	"log/slog"
	"os"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
	"github.com/spf13/viper"
)

// setupLogging installs the JSON logger, reporting errors to Sentry when
// sentry_dsn is set. Call it once the config is loaded. The returned func sends queued events.
func setupLogging() func() {
	flush, err := errtrack.Init(errtrack.Config{
		DSN:                 viper.GetString("SENTRY_DSN"),
		Environment:         cmp.Or(viper.GetString("SENTRY_ENVIRONMENT"), viper.GetString("ENVIRONMENT")),
		TracesSampleRate:    viper.GetFloat64("SENTRY_TRACES_SAMPLE_RATE"),
		JobTracesSampleRate: viper.GetFloat64("SENTRY_JOB_TRACES_SAMPLE_RATE"),
	})
	slog.SetDefault(slog.New(errtrack.Handler(slog.NewJSONHandler(os.Stdout, nil))))
	if err != nil {
		slog.Warn("sentry disabled", "reason", err)
	}
	return flush
}
