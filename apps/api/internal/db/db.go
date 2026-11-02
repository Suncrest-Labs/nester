// Package db provides helpers for opening and health-checking a PostgreSQL
// connection pool backed by pgx/v5's database/sql adapter.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	// DefaultStatementTimeout bounds a single statement on a pooled
	// connection. A query that runs longer is cancelled by PostgreSQL instead
	// of holding its connection until the client gives up.
	DefaultStatementTimeout = 30 * time.Second

	// DefaultLockTimeout bounds how long a statement waits for a table or row
	// lock before PostgreSQL cancels it.
	DefaultLockTimeout = 5 * time.Second

	// DefaultIdleInTransactionSessionTimeout closes a connection that has been
	// idle inside an open transaction for too long, releasing its locks.
	DefaultIdleInTransactionSessionTimeout = 60 * time.Second

	// MainnetStatementTimeout, MainnetLockTimeout and
	// MainnetIdleInTransactionSessionTimeout are the stricter defaults used
	// when StrictTimeouts is set. The API sets StrictTimeouts on Stellar
	// mainnet, where a stuck query is a financial-availability incident.
	MainnetStatementTimeout                = 10 * time.Second
	MainnetLockTimeout                     = 2 * time.Second
	MainnetIdleInTransactionSessionTimeout = 30 * time.Second
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

	// StatementTimeout bounds a single statement. Zero selects the default
	// (DefaultStatementTimeout, or MainnetStatementTimeout when
	// StrictTimeouts is set). A negative value disables the server-side
	// timeout.
	StatementTimeout time.Duration

	// LockTimeout bounds how long a statement waits for a lock. Zero selects
	// the default (DefaultLockTimeout, or MainnetLockTimeout when
	// StrictTimeouts is set). A negative value disables it.
	LockTimeout time.Duration

	// IdleInTransactionSessionTimeout closes connections left idle inside an
	// open transaction. Zero selects the default
	// (DefaultIdleInTransactionSessionTimeout, or
	// MainnetIdleInTransactionSessionTimeout when StrictTimeouts is set).
	// A negative value disables it.
	IdleInTransactionSessionTimeout time.Duration

	// StrictTimeouts selects the tighter mainnet defaults for any timeout
	// field left at zero.
	StrictTimeouts bool
}

// Open opens a new *sql.DB, applies the pool settings from cfg, and verifies
// connectivity with a PingContext.  The caller owns the returned *sql.DB and
// must call Close when finished.
func Open(cfg Config) (*sql.DB, error) {
	connConfig, err := pgx.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	// RuntimeParams are sent in the PostgreSQL startup packet, so every
	// connection opened from this pool inherits the server-side timeouts.
	ApplySessionTimeouts(
		connConfig,
		cfg.StatementTimeout,
		cfg.LockTimeout,
		cfg.IdleInTransactionSessionTimeout,
		cfg.StrictTimeouts,
	)

	db := stdlib.OpenDB(*connConfig)

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

// ApplySessionTimeouts sets PostgreSQL's per-session statement, lock and
// idle-in-transaction timeouts on connConfig. A zero duration selects the
// default (or the stricter mainnet default when strict is true); a negative
// duration disables that timeout.
//
// The parameters are sent in the startup packet rather than executed as SET
// statements, so they apply to every connection the pool opens, including
// connections created after Open returns.
func ApplySessionTimeouts(connConfig *pgx.ConnConfig, statement, lock, idle time.Duration, strict bool) {
	setRuntimeParam(connConfig, "statement_timeout",
		resolveTimeout(statement, DefaultStatementTimeout, MainnetStatementTimeout, strict))
	setRuntimeParam(connConfig, "lock_timeout",
		resolveTimeout(lock, DefaultLockTimeout, MainnetLockTimeout, strict))
	setRuntimeParam(connConfig, "idle_in_transaction_session_timeout",
		resolveTimeout(idle, DefaultIdleInTransactionSessionTimeout, MainnetIdleInTransactionSessionTimeout, strict))
}

func resolveTimeout(configured, def, mainnet time.Duration, strict bool) time.Duration {
	if configured < 0 {
		return 0
	}
	if configured > 0 {
		return configured
	}
	if strict {
		return mainnet
	}
	return def
}

func setRuntimeParam(connConfig *pgx.ConnConfig, key string, d time.Duration) {
	if d <= 0 {
		return
	}
	if connConfig.RuntimeParams == nil {
		connConfig.RuntimeParams = make(map[string]string)
	}
	// PostgreSQL accepts these parameters as integer milliseconds.
	connConfig.RuntimeParams[key] = strconv.FormatInt(d.Milliseconds(), 10)
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
