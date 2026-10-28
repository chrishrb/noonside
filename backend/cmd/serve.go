package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	Long:  `Starts the API server and the data source scheduler.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		flush := setupLogging()
		defer flush()

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		a, err := app.New(ctx)
		if err != nil {
			return err
		}
		defer a.Close()

		// SFTPGo may start after the API; the hosted_ftp_gc job retries.
		go a.DataSources.EnsureHostedHook(ctx)

		var wg sync.WaitGroup
		if viper.GetBool("SCHEDULER_ENABLED") {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.Scheduler.Run(ctx)
			}()
		}

		srv := &http.Server{
			Addr:              fmt.Sprintf(":%s", viper.GetString("HTTP_PORT")),
			Handler:           a.Handler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			slog.Info("Gracefully shutting down...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				slog.Error("failed to shut down server", "error", err)
			}
		}()

		slog.Info("Starting server...", "addr", srv.Addr, "env", viper.GetString("ENVIRONMENT"))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("failed to start server: %w", err)
		}
		wg.Wait()
		slog.Info("Graceful shutdown completed")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
