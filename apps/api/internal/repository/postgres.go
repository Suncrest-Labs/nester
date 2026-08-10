package repository

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
)

// maxPoolConns caps the pgxpool connection count. It also keeps the value well
// inside int32 range, so narrowing it for pgxpool.Config.MaxConns is safe.
const maxPoolConns = 25

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

	// Clamp into [1, maxPoolConns] before narrowing to int32. PoolSize() is an
	// int parsed from the environment, so on a 64-bit build it can exceed
	// int32 range, and pgxpool rejects MaxConns below 1. The explicit
	// math.MaxInt32 guard makes the conversion provably in-range.
	poolSize := cfg.PoolSize()
	if poolSize < 1 {
		poolSize = 1
	}
	if poolSize > maxPoolConns {
		poolSize = maxPoolConns
	}
	if poolSize > math.MaxInt32 {
		poolSize = math.MaxInt32
	}
	maxConns := int32(poolSize)
	poolConfig.MaxConns = maxConns
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
