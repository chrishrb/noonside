package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Migration is one file of db/migrations: <version>_<name>.sql.
type Migration struct {
	Version string
	Name    string
	Path    string
}

var migrationFile = regexp.MustCompile(`^(\d+)_(.+)\.sql$`)

// ReadMigrations lists the migrations in dir, sorted by version. Other files
// are ignored.
func ReadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var ms []Migration
	seen := map[string]string{}
	for _, e := range entries {
		m := migrationFile.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		if prev, ok := seen[m[1]]; ok {
			return nil, fmt.Errorf("migrations %s and %s share version %s", prev, e.Name(), m[1])
		}
		seen[m[1]] = e.Name()
		ms = append(ms, Migration{Version: m[1], Name: m[2], Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Version < ms[j].Version })
	return ms, nil
}

// migrationLock serializes concurrent Migrate calls (pg_advisory_lock key).
const migrationLock = 7_301_964_211

// Migrate applies the migrations in dir that are not recorded yet, each in
// its own transaction, and returns the applied versions. It records them in
// noonside_migrations.schema_migrations (the Supabase CLI's table, renamed
// from supabase_migrations on the first run, so the history of databases set
// up with it carries over). It also applies versions older than the newest
// recorded one.
func Migrate(ctx context.Context, d *DB, dir string) ([]string, error) {
	ms, err := ReadMigrations(dir)
	if err != nil {
		return nil, err
	}
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return nil, fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLock)
	}()

	if _, err := conn.Exec(ctx, `DO $$ BEGIN
			IF EXISTS (SELECT FROM pg_namespace WHERE nspname = 'supabase_migrations')
				AND NOT EXISTS (SELECT FROM pg_namespace WHERE nspname = 'noonside_migrations') THEN
				ALTER SCHEMA supabase_migrations RENAME TO noonside_migrations;
			END IF;
		END $$;
		CREATE SCHEMA IF NOT EXISTS noonside_migrations;
		CREATE TABLE IF NOT EXISTS noonside_migrations.schema_migrations (
			version text PRIMARY KEY, statements text[], name text)`); err != nil {
		return nil, fmt.Errorf("create migration table: %w", err)
	}
	rows, err := conn.Query(ctx, "SELECT version FROM noonside_migrations.schema_migrations")
	if err != nil {
		return nil, err
	}
	done, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	applied := map[string]bool{}
	for _, v := range done {
		applied[v] = true
	}

	var ran []string
	for _, m := range ms {
		if applied[m.Version] {
			continue
		}
		sql, err := os.ReadFile(m.Path)
		if err != nil {
			return ran, err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// The simple protocol runs the whole file, several statements.
			if _, err := tx.Conn().PgConn().Exec(ctx, string(sql)).ReadAll(); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO noonside_migrations.schema_migrations (version, name, statements)
				VALUES ($1, $2, ARRAY[$3])`, m.Version, m.Name, string(sql))
			return err
		})
		if err != nil {
			return ran, fmt.Errorf("migration %s_%s: %w", m.Version, m.Name, err)
		}
		ran = append(ran, m.Version)
	}
	return ran, nil
}
