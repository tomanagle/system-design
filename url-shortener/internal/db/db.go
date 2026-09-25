package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	Pool        *sql.DB
	DatabaseURL string
}

type NewDBParams struct {
	DatabaseURL string
}

func NewDB(params NewDBParams) *DB {
	return &DB{
		Pool:        nil,
		DatabaseURL: params.DatabaseURL,
	}
}

// Open creates a shared Postgres connection pool and verifies connectivity.
// The caller controls the connection timeout through ctx and must close the pool.
func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	pool, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Limits apply per app replica, so three replicas use at most 30 connections.
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)

	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func (db *DB) MustOpen(ctx context.Context) *sql.DB {
	pool, err := Open(ctx, db.DatabaseURL)
	if err != nil {
		panic(err)
	}
	db.Pool = pool
	return pool
}
