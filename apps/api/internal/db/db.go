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

	// ConnMaxLifetime sets the maximum amount of time a connection may be reused.
	// Expired connections may be closed lazily before reuse.
	ConnMaxLifetime time.Duration

	// ConnMaxIdleTime sets the maximum amount of time a connection may be idle.
	// Expired connections may be closed lazily before reuse.
	ConnMaxIdleTime time.Duration

	// ConnectionTimeout is how long Open will wait for the initial Ping.
	// Defaults to 5 s when zero.
	ConnectionTimeout time.Duration

	// DefaultQueryTimeout specifies the default timeout for database operations
	// when no deadline is set on the context.
	DefaultQueryTimeout time.Duration
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
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
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

// WithQueryTimeout returns a derived context with timeout if the parent context
// does not already have a deadline set.
func WithQueryTimeout(ctx context.Context, defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	if defaultTimeout <= 0 {
		defaultTimeout = 10 * time.Second
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

