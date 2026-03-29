// Package db provides helpers for opening and health-checking a PostgreSQL
// connection pool backed by pgx/v5's database/sql adapter.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Config holds the parameters used to open a connection pool.
type Config struct {
	// DSN is a libpq-style URL, e.g.
	// "postgres://user:pass@host:5432/dbname?sslmode=disable"
	DSN string

	// MaxOpenConns caps the number of open (in-use + idle) connections.
	// Zero means the default database/sql unlimited behaviour.
	MaxOpenConns int

	// MaxIdleConns caps the number of idle connections retained in the pool.
	// Zero means the default database/sql behaviour (2 at time of writing).
	MaxIdleConns int

	// ConnectionTimeout is how long Open will wait for the initial Ping.
	// Defaults to 5 s when zero.
	ConnectionTimeout time.Duration
}

// Open opens a new *sql.DB, applies the pool settings from cfg, and verifies
// connectivity with a PingContext.  The caller owns the returned *sql.DB and
// must call Close when finished.
func Open(cfg Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	timeout := cfg.ConnectionTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return db, nil
}

// Ping reports whether the database is reachable within the lifetime of ctx.
func Ping(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}
