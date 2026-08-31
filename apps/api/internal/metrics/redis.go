package metrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// redisCommandOther bounds the command label. go-redis reports whatever
// command name was issued, and while the API only issues a handful, an
// allowlist means a future caller cannot accidentally turn a
// caller-influenced command name into unbounded series.
const redisCommandOther = "other"

// redisCommands is the set of commands the API issues, drawn from the
// challenge store, revocation cache, rate limiters, idempotency store, and
// cache package. Anything outside this set is reported as "other" — the
// command still shows up in totals and errors, it just does not mint a
// series. Extend this list when a new command becomes load-bearing enough to
// deserve its own latency breakdown.
var redisCommands = map[string]struct{}{
	"get": {}, "set": {}, "setex": {}, "setnx": {}, "getset": {},
	"del": {}, "unlink": {}, "exists": {}, "expire": {}, "ttl": {},
	"incr": {}, "incrby": {}, "decr": {}, "hget": {}, "hset": {},
	"hgetall": {}, "hdel": {}, "hincrby": {}, "lpush": {}, "rpush": {},
	"lrange": {}, "lrem": {}, "llen": {}, "sadd": {}, "srem": {},
	"smembers": {}, "sismember": {}, "scard": {}, "zadd": {}, "zrem": {},
	"zrange": {}, "zrangebyscore": {}, "zremrangebyscore": {}, "zcard": {},
	"scan": {}, "eval": {}, "evalsha": {}, "script": {}, "publish": {},
	"subscribe": {}, "psubscribe": {}, "ping": {}, "pipeline": {},
	"multi": {}, "exec": {}, "info": {}, "keys": {}, "type": {},
	"persist": {}, "pexpire": {}, "pttl": {},
}

// redisHook records command count, latency, and errors for every Redis
// command the shared client issues.
//
// Only the command name is labelled. Keys are never labelled: Redis keys in
// this codebase embed user IDs, wallet addresses, session IDs, and
// idempotency keys, so a key label would be both unbounded and a direct leak
// of user identifiers into the metrics backend. Command arguments are never
// touched for the same reason.
type redisHook struct {
	m *Metrics
}

// InstrumentRedis attaches metrics instrumentation to a Redis client.
//
// A nil client is a no-op: REDIS_ADDR is optional and the API falls back to
// in-memory implementations when it is unset, so callers should not have to
// branch.
func (m *Metrics) InstrumentRedis(client *redis.Client) {
	if client == nil {
		return
	}
	client.AddHook(&redisHook{m: m})

	// Pool statistics come from the client itself at scrape time, for the
	// same reason the pgxpool stats do: the client already tracks them, so
	// sampling on a ticker would only add staleness.
	m.registry.MustRegister(newRedisPoolCollector(client))
}

// DialHook passes through unchanged. Dial latency is already visible as the
// tail of the command that triggered the dial, and instrumenting it
// separately would add a metric nobody alerts on.
func (h *redisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *redisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		startedAt := time.Now()
		err := next(ctx, cmd)
		h.observe(normalizeRedisCommand(cmd.Name()), time.Since(startedAt), err)
		return err
	}
}

func (h *redisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		startedAt := time.Now()
		err := next(ctx, cmds)
		// A pipeline is one round trip, so it is recorded as one observation
		// under "pipeline" rather than fanning out per queued command, which
		// would inflate the count and attribute the whole round trip's
		// latency to each member.
		h.observe("pipeline", time.Since(startedAt), err)
		return err
	}
}

// observe records a single command outcome.
//
// redis.Nil means "key not found", which is an ordinary control-flow result
// for a cache lookup, not a failure. Counting it as an error would make the
// error rate track cache misses and render any alert on it meaningless.
func (h *redisHook) observe(command string, elapsed time.Duration, err error) {
	h.m.redisCommandsTotal.WithLabelValues(command).Inc()
	h.m.redisCommandDuration.WithLabelValues(command).Observe(elapsed.Seconds())

	if err != nil && !errors.Is(err, redis.Nil) {
		h.m.redisErrorsTotal.WithLabelValues(command).Inc()
	}
}

// normalizeRedisCommand lowercases the command and maps anything outside the
// known set to "other".
func normalizeRedisCommand(name string) string {
	lowered := strings.ToLower(name)
	if _, ok := redisCommands[lowered]; ok {
		return lowered
	}
	return redisCommandOther
}

// PoolStater is the subset of *redis.Client the pool collector needs.
type PoolStater interface {
	PoolStats() *redis.PoolStats
}

// redisPoolCollector exposes go-redis connection pool statistics at scrape
// time.
type redisPoolCollector struct {
	client PoolStater

	hits       *prometheus.Desc
	misses     *prometheus.Desc
	timeouts   *prometheus.Desc
	totalConns *prometheus.Desc
	idleConns  *prometheus.Desc
	staleConns *prometheus.Desc
}

func newRedisPoolCollector(client PoolStater) *redisPoolCollector {
	const subsystem = "redis_pool"

	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}

	return &redisPoolCollector{
		client: client,

		hits: prometheus.NewDesc(name("hits_total"),
			"Total times a free connection was found in the pool.", nil, nil),
		misses: prometheus.NewDesc(name("misses_total"),
			"Total times a free connection was not found in the pool.", nil, nil),
		// The saturation signal: a rising timeouts counter means callers are
		// giving up waiting for a Redis connection.
		timeouts: prometheus.NewDesc(name("timeouts_total"),
			"Total times a wait for a pool connection timed out.", nil, nil),
		totalConns: prometheus.NewDesc(name("total_connections"),
			"Connections currently owned by the Redis pool.", nil, nil),
		idleConns: prometheus.NewDesc(name("idle_connections"),
			"Idle connections in the Redis pool.", nil, nil),
		staleConns: prometheus.NewDesc(name("stale_connections_total"),
			"Total connections removed from the Redis pool as stale.", nil, nil),
	}
}

func (c *redisPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.hits
	ch <- c.misses
	ch <- c.timeouts
	ch <- c.totalConns
	ch <- c.idleConns
	ch <- c.staleConns
}

func (c *redisPoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c.client == nil {
		return
	}

	stats := c.client.PoolStats()
	if stats == nil {
		return
	}

	ch <- prometheus.MustNewConstMetric(c.hits, prometheus.CounterValue, float64(stats.Hits))
	ch <- prometheus.MustNewConstMetric(c.misses, prometheus.CounterValue, float64(stats.Misses))
	ch <- prometheus.MustNewConstMetric(c.timeouts, prometheus.CounterValue, float64(stats.Timeouts))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stats.TotalConns))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stats.IdleConns))
	ch <- prometheus.MustNewConstMetric(c.staleConns, prometheus.CounterValue, float64(stats.StaleConns))
}

// interface guards
var (
	_ redis.Hook           = (*redisHook)(nil)
	_ prometheus.Collector = (*redisPoolCollector)(nil)
)
