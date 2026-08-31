package cache

import (
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// InstrumentRedis attaches OpenTelemetry tracing to a Redis client
// (nester#1054).
//
// Command *arguments* are deliberately not recorded. redisotel's
// WithDBStatement(false) limits the db.statement attribute to the command
// name — "GET", "SETNX" — with no keys and no values. Both matter here: cache
// keys are namespaced by user and would carry identifiers, and cached values
// are serialised portfolio and balance data. The command name plus the span's
// duration is what actually diagnoses a slow or failing cache, and none of
// the rest is needed to do it.
//
// The client is returned for call-site convenience; instrumentation is applied
// in place via hooks. A nil client is returned unchanged, matching the
// codebase's existing convention that an unconfigured REDIS_ADDR yields a nil
// client and every cache path degrades to direct compute.
func InstrumentRedis(client *redis.Client, enabled bool) *redis.Client {
	if client == nil || !enabled {
		return client
	}

	// Errors from the instrumentation hooks are intentionally swallowed: a
	// telemetry wiring failure must never prevent the cache from working.
	// Redis is already a best-effort dependency throughout this package.
	_ = redisotel.InstrumentTracing(client,
		redisotel.WithDBStatement(false),
	)

	return client
}
