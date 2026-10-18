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

// Default and mainnet-strict server-side PostgreSQL session timeouts (#1564).
const (
	DefaultStatementTimeout               = 10 * time.Second
	DefaultLockTimeout                    = 3 * time.Second
	DefaultIdleInTransactionTimeout       = 15 * time.Second
	MainnetStatementTimeout               = 5 * time.Second
	MainnetLockTimeout                    = 2 * time.Second
	MainnetIdleInTransactionTimeout       = 10 * time.Second
	MigrationStatementTimeout             = 5 * time.Minute
	MigrationLockTimeout                  = 30 * time.Second
	BackfillStatementTimeout              = 2 * time.Minute
	BackfillIdleInTransactionTimeout      = 2 * time.Minute
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

	// StatementTimeout sets PostgreSQL statement_timeout per connection.
	// When zero, defaults to DefaultStatementTimeout (or MainnetStatementTimeout when Mainnet is true).
	StatementTimeout time.Duration

	// LockTimeout sets PostgreSQL lock_timeout per connection.
	// When zero, defaults to DefaultLockTimeout (or MainnetLockTimeout when Mainnet is true).
	LockTimeout time.Duration

	// IdleInTransactionSessionTimeout sets PostgreSQL idle_in_transaction_session_timeout per connection.
	// When zero, defaults to DefaultIdleInTransactionTimeout (or MainnetIdleInTransactionTimeout when Mainnet is true).
	IdleInTransactionSessionTimeout time.Duration

	// Mainnet selects stricter default session timeouts when individual timeout fields are zero.
	Mainnet bool
}

// ResolvedTimeouts returns the effective (statement_timeout, lock_timeout, idle_in_transaction_session_timeout)
// durations for cfg, applying stricter defaults when Mainnet is true.
func (cfg Config) ResolvedTimeouts() (statementTimeout, lockTimeout, idleInTxTimeout time.Duration) {
	statementTimeout = cfg.StatementTimeout
	lockTimeout = cfg.LockTimeout
	idleInTxTimeout = cfg.IdleInTransactionSessionTimeout

	if cfg.Mainnet {
		if statementTimeout <= 0 {
			statementTimeout = MainnetStatementTimeout
		}
		if lockTimeout <= 0 {
			lockTimeout = MainnetLockTimeout
		}
		if idleInTxTimeout <= 0 {
			idleInTxTimeout = MainnetIdleInTransactionTimeout
		}
		return statementTimeout, lockTimeout, idleInTxTimeout
	}

	if statementTimeout <= 0 {
		statementTimeout = DefaultStatementTimeout
	}
	if lockTimeout <= 0 {
		lockTimeout = DefaultLockTimeout
	}
	if idleInTxTimeout <= 0 {
		idleInTxTimeout = DefaultIdleInTransactionTimeout
	}
	return statementTimeout, lockTimeout, idleInTxTimeout
}

// FormatPostgresTimeoutMilliseconds formats a positive duration as integer milliseconds
// for PostgreSQL RuntimeParams (e.g. statement_timeout, lock_timeout).
func FormatPostgresTimeoutMilliseconds(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1 && d > 0 {
		ms = 1
	}
	return strconv.FormatInt(ms, 10)
}

// Open opens a new *sql.DB, applies the pool and server-side session timeout
// settings from cfg, and verifies connectivity with a PingContext. The caller
// owns the returned *sql.DB and must call Close when finished.
func Open(cfg Config) (*sql.DB, error) {
	connCfg, parseErr := pgx.ParseConfig(cfg.DSN)
	var pool *sql.DB
	if parseErr == nil && cfg.DSN != "" {
		if connCfg.RuntimeParams == nil {
			connCfg.RuntimeParams = make(map[string]string)
		}
		stmtTimeout, lockTimeout, idleTxTimeout := cfg.ResolvedTimeouts()
		if _, exists := connCfg.RuntimeParams["statement_timeout"]; !exists {
			connCfg.RuntimeParams["statement_timeout"] = FormatPostgresTimeoutMilliseconds(stmtTimeout)
		}
		if _, exists := connCfg.RuntimeParams["lock_timeout"]; !exists {
			connCfg.RuntimeParams["lock_timeout"] = FormatPostgresTimeoutMilliseconds(lockTimeout)
		}
		if _, exists := connCfg.RuntimeParams["idle_in_transaction_session_timeout"]; !exists {
			connCfg.RuntimeParams["idle_in_transaction_session_timeout"] = FormatPostgresTimeoutMilliseconds(idleTxTimeout)
		}
		pool = stdlib.OpenDB(*connCfg)
	} else {
		var err error
		pool, err = sql.Open("pgx", cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("db: open: %w", err)
		}
	}

	if cfg.MaxOpenConns > 0 {
		pool.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		pool.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	timeout := cfg.ConnectionTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return pool, nil
}

// OverrideTxTimeouts overrides statement_timeout, lock_timeout, and
// idle_in_transaction_session_timeout for the duration of a single transaction
// (using SET LOCAL) — used by migrations and long-running backfill batches (#1564).
func OverrideTxTimeouts(ctx context.Context, tx *sql.Tx, statementTimeout, lockTimeout, idleInTxTimeout time.Duration) error {
	if statementTimeout > 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", statementTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("db: set local statement_timeout: %w", err)
		}
	}
	if lockTimeout > 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", lockTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("db: set local lock_timeout: %w", err)
		}
	}
	if idleInTxTimeout > 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL idle_in_transaction_session_timeout = '%dms'", idleInTxTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("db: set local idle_in_transaction_session_timeout: %w", err)
		}
	}
	return nil
}

// OverrideBackfillTimeouts relaxes transaction-local timeouts for a backfill transaction.
func OverrideBackfillTimeouts(ctx context.Context, tx *sql.Tx) error {
	return OverrideTxTimeouts(ctx, tx, BackfillStatementTimeout, MigrationLockTimeout, BackfillIdleInTransactionTimeout)
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
