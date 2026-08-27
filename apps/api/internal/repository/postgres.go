package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
)

// PostgresDB wraps the pgxpool to provide database access and readiness checks.
type PostgresDB struct {
	Pool *pgxpool.Pool
}

// NewPostgresDB initializes a new PostgreSQL connection pool using pgxpool.
//
// Tracing is opt-in via the tracingEnabled argument; see NewPostgresDBTraced.
func NewPostgresDB(cfg config.DatabaseConfig) (*PostgresDB, error) {
	return newPostgresDB(cfg, false)
}

// NewPostgresDBTraced is NewPostgresDB with OpenTelemetry query spans
// attached (nester#1054).
//
// Privacy is the design constraint. otelpgx is configured with
// WithTrimSQLInSpanName so span names stay low-cardinality, and crucially
// *without* IncludeQueryParameters — pgx would otherwise attach every bound
// argument to the span, and in this codebase those are account numbers,
// balances, wallet addresses and amounts. The parameterised SQL text is
// recorded (placeholders intact), which is what identifies a slow query
// without disclosing whose data it ran against.
func NewPostgresDBTraced(cfg config.DatabaseConfig) (*PostgresDB, error) {
	return newPostgresDB(cfg, true)
}

// pgxTracerOptions are the otelpgx options the traced pool uses.
//
// Defined once and shared with the tracing tests so the test cannot drift from
// production. That matters for the privacy contract specifically: if someone
// later adds otelpgx.WithIncludeQueryParameters() here, the tests that assert
// bound values never reach a span will fail, instead of silently continuing to
// exercise a differently-configured tracer.
func pgxTracerOptions() []otelpgx.Option {
	return []otelpgx.Option{
		// IncludeQueryParameters is deliberately absent: bound arguments are
		// account numbers, balances, wallet addresses and amounts.
		otelpgx.WithTrimSQLInSpanName(),
	}
}

func newPostgresDB(cfg config.DatabaseConfig, tracingEnabled bool) (*PostgresDB, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	poolSize := cfg.PoolSize()
	if poolSize > 25 {
		poolSize = 25
	}
	poolConfig.MaxConns = int32(poolSize)
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.HealthCheckPeriod = poolConfig.MaxConnIdleTime

	if tracingEnabled {
		// IncludeQueryParameters is deliberately not set: bound parameters
		// are user financial data and must never reach a span.
		poolConfig.ConnConfig.Tracer = otelpgx.NewTracer(pgxTracerOptions()...)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectionTimeout())
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	pingCtx, pingCancel := context.WithTimeout(context.Background(), cfg.ConnectionTimeout())
	defer pingCancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return &PostgresDB{Pool: pool}, nil
}

// Ping performs a health check on the connection pool.
func (db *PostgresDB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}
