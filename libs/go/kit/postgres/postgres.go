package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaMigrationsLockKey int64 = 8_521_041

func schemaName(service string) string {
	return strings.ReplaceAll(service, "-", "_")
}

// Connect opens and verifies a pgx connection pool using the given DSN.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return pool, nil
}

// ApplyMigrations runs embedded SQL migration files for a given service.
// Migration filenames are applied in lexical order and tracked in a shared
// schema_migrations table using (service, version) as the primary key.
// Each service gets its own PostgreSQL schema (e.g., orion_placement, orion_compute).
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, service string, migrations fs.FS, dir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres acquire migration connection for %s: %w", service, err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationsLockKey); err != nil {
		return fmt.Errorf("postgres acquire migration lock for %s: %w", service, err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationsLockKey)
	}()

	schema := schemaName(service)

	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schema)); err != nil {
		return fmt.Errorf("postgres create schema %s: %w", schema, err)
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.schema_migrations (
			service    TEXT        NOT NULL,
			version    TEXT        NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (service, version)
		)`, schema)); err != nil {
		return fmt.Errorf("postgres ensure schema_migrations in %s: %w", schema, err)
	}

	entries, err := fs.ReadDir(migrations, dir)
	if err != nil {
		return fmt.Errorf("postgres read migrations for %s: %w", service, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(file, ".sql")
		var applied bool
		if err := conn.QueryRow(ctx,
			fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.schema_migrations WHERE service = $1 AND version = $2)`, schema),
			service, version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("postgres migration lookup %s/%s: %w", service, version, err)
		}
		if applied {
			continue
		}

		body, err := fs.ReadFile(migrations, path.Join(dir, file))
		if err != nil {
			return fmt.Errorf("postgres read migration %s/%s: %w", service, file, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("postgres begin migration %s/%s: %w", service, version, err)
		}

		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres execute migration %s/%s: %w", service, version, err)
		}
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`INSERT INTO %s.schema_migrations (service, version) VALUES ($1, $2)`, schema),
			service, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres record migration %s/%s: %w", service, version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("postgres commit migration %s/%s: %w", service, version, err)
		}
	}

	return nil
}
