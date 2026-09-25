package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies pending SQL files in filename order, atomically as one batch.
func Migrate(ctx context.Context, pool *sql.DB) error {
	return migrate(ctx, pool, migrations)
}

func migrate(ctx context.Context, pool *sql.DB, files fs.FS) error {
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("no migration files found")
	}

	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer tx.Rollback()

	// Transaction-scoped lock: released on commit, rollback, or disconnection.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(731082725)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}

	rows, err := tx.QueryContext(ctx, "SELECT name, checksum FROM schema_migrations ORDER BY name")
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	var appliedNames, appliedChecksums []string
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration history: %w", err)
		}
		appliedNames = append(appliedNames, name)
		appliedChecksums = append(appliedChecksums, checksum)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	if len(appliedNames) > len(names) {
		return fmt.Errorf("applied migrations are missing from this build")
	}

	for i, name := range names {
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		if i < len(appliedNames) {
			if appliedNames[i] != name || appliedChecksums[i] != checksum {
				return fmt.Errorf("migration history differs at %s: applied files must not be changed, removed, or reordered", name)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)", name, checksum); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
