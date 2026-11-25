package rpcbudget

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// Priority classifies outbound RPC calls so lower-priority traffic is shed
// before critical background loops when a provider's request budget is under
// pressure.
type Priority int

const (
	// PriorityLow is for user-triggered simulations, quotes, and previews.
	// Calls at this priority are shed first when the bucket drops below the
	// low-priority threshold.
	PriorityLow Priority = iota

	// PriorityNormal is the default priority for untagged outbound calls.
	// Calls at this priority are shed once only the high-priority reserve
	// remains.
	PriorityNormal

	// PriorityHigh is for critical chain maintenance: event indexer polling,
	// backfill, freshness checks, and the TTL keeper. Calls at this priority
	// can consume the full bucket including the reserved headroom.
	PriorityHigh

	priorityCount
)

// String returns the bounded label value used in metrics and logs.
func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	default:
		return "normal"
	}
}

// AllPriorities returns the valid priority classes in ascending order.
func AllPriorities() []Priority {
	return []Priority{PriorityLow, PriorityNormal, PriorityHigh}
}

type priorityContextKey struct{}

// WithPriority attaches p to ctx, overriding any previously attached priority.
func WithPriority(ctx context.Context, p Priority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, priorityContextKey{}, p)
}

// WithDefaultPriority attaches p to ctx only if no priority is already set.
// This lets shared helpers tag a conservative default (e.g. PriorityLow for
// simulations) while allowing higher-priority callers (e.g. a keeper) to
// override it via WithPriority.
func WithDefaultPriority(ctx context.Context, p Priority) context.Context {
	if ctx == nil {
		return WithPriority(context.Background(), p)
	}
	if _, ok := ctx.Value(priorityContextKey{}).(Priority); ok {
		return ctx
	}
	return context.WithValue(ctx, priorityContextKey{}, p)
}

// PriorityFromContext returns the priority attached to ctx, defaulting to
// PriorityNormal when none was set.
func PriorityFromContext(ctx context.Context) Priority {
	if ctx == nil {
		return PriorityNormal
	}
	if p, ok := ctx.Value(priorityContextKey{}).(Priority); ok && p >= PriorityLow && p < priorityCount {
		return p
	}
	return PriorityNormal
}

// ErrBudgetExhausted is returned when a provider's token bucket cannot admit
// a call at its priority class. Callers and retry loops should treat this as
// a fast local rejection rather than an upstream failure.
var ErrBudgetExhausted = errors.New("rpc budget exhausted")

// ShedError wraps ErrBudgetExhausted with the provider name and priority class
// that was shed.
type ShedError struct {
	Provider string
	Priority Priority
}

func (e *ShedError) Error() string {
	return fmt.Sprintf("rpc budget exhausted for provider %s (priority=%s)", e.Provider, e.Priority.String())
}

func (e *ShedError) Unwrap() error {
	return ErrBudgetExhausted
}

// Default limiter settings.
const (
	DefaultRefillRate   = 50.0
	DefaultBurst        = 100.0
	DefaultHighReserve  = 0.25
	DefaultLowThreshold = 0.50
)

// Config parameterises a per-provider priority token-bucket limiter.
//
// HighReserve is the fraction of Burst reserved exclusively for PriorityHigh
// calls (indexer, backfill, freshness, keeper). PriorityNormal calls are shed
// when available tokens drop below Burst*HighReserve.
//
// LowThreshold is the fraction of Burst below which PriorityLow calls (quotes,
// preview simulations) are shed. Must be >= HighReserve so lowest-priority
// calls are always shed first.
type Config struct {
	RefillRate   float64
	Burst        float64
	HighReserve  float64
	LowThreshold float64
}

// DefaultConfig returns the default token-bucket configuration.
func DefaultConfig() Config {
	return Config{
		RefillRate:   DefaultRefillRate,
		Burst:        DefaultBurst,
		HighReserve:  DefaultHighReserve,
		LowThreshold: DefaultLowThreshold,
	}
}

// Validate checks that cfg is internally consistent.
func (c Config) Validate() error {
	if c.RefillRate <= 0 || math.IsNaN(c.RefillRate) || math.IsInf(c.RefillRate, 0) {
		return fmt.Errorf("rpc budget refill rate must be positive, got %v", c.RefillRate)
	}
	if c.Burst < 1 || math.IsNaN(c.Burst) || math.IsInf(c.Burst, 0) {
		return fmt.Errorf("rpc budget burst must be at least 1, got %v", c.Burst)
	}
	if c.HighReserve < 0 || c.HighReserve >= 1 || math.IsNaN(c.HighReserve) {
		return fmt.Errorf("rpc budget high reserve must be in [0, 1), got %v", c.HighReserve)
	}
	if c.LowThreshold < c.HighReserve || c.LowThreshold >= 1 || math.IsNaN(c.LowThreshold) {
		return fmt.Errorf("rpc budget low threshold must be in [high_reserve, 1), got %v (high_reserve=%v)", c.LowThreshold, c.HighReserve)
	}
	return nil
}

// Snapshot is a point-in-time view of a Limiter's state for metrics and
// diagnostics.
type Snapshot struct {
	Name        string
	Capacity    float64
	RefillRate  float64
	Available   float64
	Utilization float64
	Admitted    [priorityCount]uint64
	Shed        [priorityCount]uint64
}

// Limiter is a thread-safe, priority-aware token-bucket rate limiter for one
// outbound RPC provider.
type Limiter struct {
	name  string
	cfg   Config
	clock func() time.Time

	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
	admitted   [priorityCount]uint64
	shed       [priorityCount]uint64
}

// New constructs a Limiter using the wall clock.
func New(name string, cfg Config) *Limiter {
	return NewWithClock(name, cfg, time.Now)
}

// NewWithClock constructs a Limiter using the supplied clock (for tests).
func NewWithClock(name string, cfg Config, clock func() time.Time) *Limiter {
	if clock == nil {
		clock = time.Now
	}
	if cfg.RefillRate <= 0 {
		cfg.RefillRate = DefaultRefillRate
	}
	if cfg.Burst < 1 {
		cfg.Burst = DefaultBurst
	}
	if cfg.HighReserve < 0 || cfg.HighReserve >= 1 {
		cfg.HighReserve = DefaultHighReserve
	}
	if cfg.LowThreshold < cfg.HighReserve || cfg.LowThreshold >= 1 {
		cfg.LowThreshold = math.Max(cfg.HighReserve, DefaultLowThreshold)
	}
	return &Limiter{
		name:       name,
		cfg:        cfg,
		clock:      clock,
		tokens:     cfg.Burst,
		lastRefill: clock(),
	}
}

// Name returns the provider name this limiter guards.
func (l *Limiter) Name() string {
	return l.name
}

// Allow attempts to consume one token for a call at priority p.
//
// Lowest-priority calls are shed first:
//   - PriorityLow requires available tokens >= max(1, Burst*LowThreshold)
//   - PriorityNormal requires available tokens >= max(1, Burst*HighReserve)
//   - PriorityHigh requires available tokens >= 1
func (l *Limiter) Allow(p Priority) error {
	if p < PriorityLow || p >= priorityCount {
		p = PriorityNormal
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked(l.clock())

	minTokens := l.thresholdFor(p)
	if l.tokens < minTokens {
		l.shed[p]++
		return &ShedError{Provider: l.name, Priority: p}
	}

	l.tokens -= 1.0
	if l.tokens < 0 {
		l.tokens = 0
	}
	l.admitted[p]++
	return nil
}

// Snapshot returns the current limiter state after refilling up to now.
func (l *Limiter) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refillLocked(l.clock())

	utilization := 0.0
	if l.cfg.Burst > 0 {
		utilization = 1.0 - (l.tokens / l.cfg.Burst)
		if utilization < 0 {
			utilization = 0
		} else if utilization > 1 {
			utilization = 1
		}
	}

	return Snapshot{
		Name:        l.name,
		Capacity:    l.cfg.Burst,
		RefillRate:  l.cfg.RefillRate,
		Available:   l.tokens,
		Utilization: utilization,
		Admitted:    l.admitted,
		Shed:        l.shed,
	}
}

func (l *Limiter) refillLocked(now time.Time) {
	if now.After(l.lastRefill) {
		elapsed := now.Sub(l.lastRefill).Seconds()
		l.tokens = math.Min(l.cfg.Burst, l.tokens+elapsed*l.cfg.RefillRate)
		l.lastRefill = now
	}
}

func (l *Limiter) thresholdFor(p Priority) float64 {
	switch p {
	case PriorityLow:
		return math.Max(1.0, l.cfg.Burst*l.cfg.LowThreshold)
	case PriorityNormal:
		return math.Max(1.0, l.cfg.Burst*l.cfg.HighReserve)
	case PriorityHigh:
		return 1.0
	default:
		return math.Max(1.0, l.cfg.Burst*l.cfg.HighReserve)
	}
}
