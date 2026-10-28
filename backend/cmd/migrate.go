package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the database migrations (migrations_dir) that are not applied yet",
	Long: `Apply the SQL files in migrations_dir (backend/db/migrations; /migrations in the
image) that are not recorded in supabase_migrations.schema_migrations, each in
its own transaction. The Auth server (GoTrue) must have created the auth
schema first: the first migration references auth.users. With --wait it waits
for the database and for auth.users (the chart runs it as an init container
next to a starting GoTrue).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		dir := app.ConfigPath("MIGRATIONS_DIR")
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()
		if migrateWait > 0 {
			if err := waitForAuthSchema(ctx, d, migrateWait); err != nil {
				return err
			}
		}
		ran, err := db.Migrate(ctx, d, dir)
		for _, v := range ran {
			fmt.Printf("applied %s\n", v)
		}
		if err != nil {
			return err
		}
		if len(ran) == 0 {
			fmt.Println("database is up to date")
		}
		return nil
	},
}

var migrateWait time.Duration

// waitForAuthSchema polls until the database answers and GoTrue has created
// auth.users.
func waitForAuthSchema(ctx context.Context, d *db.DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		var exists bool
		err := d.Pool().QueryRow(ctx, "SELECT to_regclass('auth.users') IS NOT NULL").Scan(&exists)
		if err == nil && exists {
			return nil
		}
		if err == nil {
			err = errors.New("auth.users does not exist yet (GoTrue not migrated)")
		}
		fmt.Fprintf(os.Stderr, "waiting: %v\n", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("database not ready after %s: %w", timeout, err)
		case <-time.After(2 * time.Second):
		}
	}
}

func init() {
	migrateCmd.Flags().DurationVar(&migrateWait, "wait", 0, "wait up to this long for the database and GoTrue's auth.users")
	rootCmd.AddCommand(migrateCmd)
}
