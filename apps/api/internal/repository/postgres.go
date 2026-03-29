package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
)

// PostgresDB wraps the pgxpool to provide database access and readiness checks.
type PostgresDB struct {
	Pool *pgxpool.Pool
}

// NewPostgresDB initializes a new PostgreSQL connection pool using pgxpool.
func NewPostgresDB(cfg config.DatabaseConfig) (*PostgresDB, error) {
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
