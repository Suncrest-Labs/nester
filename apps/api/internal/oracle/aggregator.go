// Multi-source consensus aggregation (nester#830). Aggregate queries every
// healthy registered source for a data type in parallel, reconciles their
// responses with median-with-deviation-band outlier rejection, and attaches
// a confidence signal derived from source agreement (and, via
// DecayConfidenceForStaleness, freshness for callers serving a cached
// value). HealthTracker records per-source success/failure history and
// applies exponential backoff so a persistently failing source is skipped
// rather than queried on every request.
package oracle

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// AggregationOptions configures one Aggregate call. Callers should document
// their chosen minimums per data type: a price steering a rebalance should
// demand more source agreement than a fiat rate shown for display.
type AggregationOptions struct {
	// MaxDeviationBPS is how far (in basis points from the pre-outlier-
	// rejection median) a source's value may sit before it is discarded as
	// an outlier. Zero disables outlier rejection entirely.
	MaxDeviationBPS int
	// MinAgreeingSources is the number of sources that must survive outlier
	// rejection for the result to be treated as full-confidence. Fewer than
	// this does not make the result Unavailable (see Aggregate's doc
	// comment) — it lowers Confidence instead.
	MinAgreeingSources int
	// PerSourceTimeout bounds how long any one source may take; a slow
	// source is treated exactly like a failed one so it cannot stall the
	// aggregate. Required — zero would mean "no timeout" via
	// context.WithTimeout, which is never the intent here.
	PerSourceTimeout time.Duration
}

// AggregatedValue is the result of reconciling one or more sources.
type AggregatedValue struct {
	Value      float64
	Confidence float64 // 0..1
	// SourcesUsed survived outlier rejection and contributed to Value.
	SourcesUsed []string
	// SourcesRejected responded but were discarded as outliers.
	SourcesRejected []string
	// SourcesSkipped were not queried at all (unhealthy, in backoff).
	SourcesSkipped []string
	// SourcesFailed were queried but errored or timed out.
	SourcesFailed []string
	// Unavailable is true only when zero sources produced a usable value.
	Unavailable bool
}

// SourceName joins SourcesUsed for a human-readable/back-compat Source
// label: a single surviving source reports its own name unchanged (so a
// caller migrating an existing single-source-at-a-time field, like
// ExchangeRate.Source, sees no change in the common case), while genuine
// multi-source agreement reports all contributing names.
func (a AggregatedValue) SourceName() string {
	return strings.Join(a.SourcesUsed, "+")
}

type sourceResult struct {
	name  string
	value float64
	err   error
}

// Aggregate queries every healthy source in `sources` for base/quote in
// parallel (each bounded by opts.PerSourceTimeout), then reconciles the
// responses into a consensus value via median-with-deviation-band outlier
// rejection.
//
// Availability policy: Aggregate reports Unavailable=true (and returns a
// non-nil error) only when zero sources produce a usable value. A lone
// responding source below opts.MinAgreeingSources is NOT treated as
// unavailable — sources being used partly *as* failover (the behavior this
// replaces for XLM/USD) depends on a single surviving source still being
// usable — but its Confidence is reduced, so callers that need full
// consensus should gate on Confidence rather than merely on the presence of
// a value.
func Aggregate(ctx context.Context, base, quote string, sources []Provider, health *HealthTracker, opts AggregationOptions) (AggregatedValue, error) {
	var toQuery []Provider
	var skipped []string
	for _, p := range sources {
		if health != nil && !health.IsHealthy(p.Name()) {
			skipped = append(skipped, p.Name())
			continue
		}
		toQuery = append(toQuery, p)
	}

	results := make([]sourceResult, len(toQuery))
	var wg sync.WaitGroup
	for i, p := range toQuery {
		wg.Add(1)
		go func(i int, p Provider) {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, opts.PerSourceTimeout)
			defer cancel()
			v, err := p.Fetch(callCtx, base, quote)
			results[i] = sourceResult{name: p.Name(), value: v, err: err}
		}(i, p)
	}
	wg.Wait()

	var succeeded []sourceResult
	var failed []string
	for _, r := range results {
		if r.err != nil {
			failed = append(failed, r.name)
			if health != nil {
				health.RecordFailure(r.name, r.err)
			}
			continue
		}
		succeeded = append(succeeded, r)
		if health != nil {
			health.RecordSuccess(r.name)
		}
	}

	if len(succeeded) == 0 {
		return AggregatedValue{
			SourcesSkipped: skipped,
			SourcesFailed:  failed,
			Unavailable:    true,
		}, fmt.Errorf("oracle: aggregate %s/%s: all sources unavailable", base, quote)
	}

	vals := make([]float64, len(succeeded))
	for i, r := range succeeded {
		vals[i] = r.value
	}
	refMedian := median(vals)

	var survivors []sourceResult
	var rejected []string
	maxDevFraction := float64(opts.MaxDeviationBPS) / 10000.0
	for _, r := range succeeded {
		deviation := deviationFrom(refMedian, r.value)
		if opts.MaxDeviationBPS > 0 && deviation > maxDevFraction {
			rejected = append(rejected, r.name)
			continue
		}
		survivors = append(survivors, r)
	}
	// A degenerate reference (e.g. refMedian == 0 making every relative
	// deviation infinite) must not report Unavailable when real responses
	// exist — fall back to the unfiltered set rather than reject everything.
	if len(survivors) == 0 {
		survivors = succeeded
		rejected = nil
	}

	survivorVals := make([]float64, len(survivors))
	survivorNames := make([]string, len(survivors))
	for i, r := range survivors {
		survivorVals[i] = r.value
		survivorNames[i] = r.name
	}

	return AggregatedValue{
		Value:           median(survivorVals),
		Confidence:      confidenceFor(len(survivors), len(sources), opts.MinAgreeingSources),
		SourcesUsed:     survivorNames,
		SourcesRejected: rejected,
		SourcesSkipped:  skipped,
		SourcesFailed:   failed,
	}, nil
}

// deviationFrom returns |value-ref|/ref as a fraction, treating a zero
// reference specially (relative deviation is undefined at zero) so a
// non-zero value against a zero reference is always rejected as an outlier
// rather than dividing by zero, while value==ref==0 is a perfect (zero
// deviation) match.
func deviationFrom(ref, value float64) float64 {
	if ref == 0 {
		if value == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return math.Abs(value-ref) / math.Abs(ref)
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// confidenceFor derives a 0..1 confidence signal from how many sources
// agreed (survived outlier rejection) relative to how many are registered
// in total — a source being unhealthy/down depresses confidence exactly
// like a source that responded but was rejected as an outlier, since both
// mean the consensus rests on fewer independent observations. An
// additional penalty applies when fewer than minAgreeing sources agreed.
// The freshness component of confidence (per #830) is applied separately by
// DecayConfidenceForStaleness when a caller serves an aging cached value.
func confidenceFor(agreeing, totalRegistered, minAgreeing int) float64 {
	if totalRegistered == 0 {
		return 0
	}
	base := float64(agreeing) / float64(totalRegistered)
	if minAgreeing > 0 && agreeing < minAgreeing {
		base *= 0.5
	}
	return math.Min(base, 1.0)
}

// DecayConfidenceForStaleness scales confidence down as a served value ages
// toward maxAge, reaching confidence*0.5 at maxAge (age beyond maxAge is
// clamped there — staleness alone never fully zeroes confidence, since a
// recently-stale value is still informative, just less so than fresh).
// Intended for callers serving a stale cache entry rather than a fresh
// aggregation.
func DecayConfidenceForStaleness(confidence float64, age, maxAge time.Duration) float64 {
	if maxAge <= 0 || age <= 0 {
		return confidence
	}
	ratio := float64(age) / float64(maxAge)
	if ratio > 1 {
		ratio = 1
	}
	return confidence * (1 - 0.5*ratio)
}

// healthBackoffBase/-Max bound the exponential backoff HealthTracker
// applies after consecutive failures: base * 2^(failures-1), capped at max.
const (
	healthBackoffBase = 5 * time.Second
	healthBackoffMax  = 5 * time.Minute
)

// SourceHealth is a point-in-time snapshot of one source's health, exposed
// via HealthTracker.Snapshot for a metrics endpoint to scrape.
type SourceHealth struct {
	Name                string
	ConsecutiveFailures int
	LastError           string
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	BackoffUntil        time.Time
}

// HealthTracker tracks per-source success/failure history and derives
// exponential backoff so a persistently failing source is skipped rather
// than queried on every request. Safe for concurrent use; share one
// instance across every Aggregate call for a given set of sources.
type HealthTracker struct {
	mu      sync.Mutex
	sources map[string]*SourceHealth
	now     func() time.Time
}

// NewHealthTracker returns an empty HealthTracker. Every source starts
// healthy (no failure history).
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{sources: make(map[string]*SourceHealth), now: time.Now}
}

// SetNowFuncForTest overrides the clock HealthTracker uses to evaluate and
// set backoff windows. Exported (rather than an unexported field) because
// this package's own tests live in the external oracle_test package,
// matching the existing service_test.go convention.
func (h *HealthTracker) SetNowFuncForTest(now func() time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.now = now
}

// entry returns (creating if needed) the health record for name. Caller
// must hold h.mu.
func (h *HealthTracker) entry(name string) *SourceHealth {
	s, ok := h.sources[name]
	if !ok {
		s = &SourceHealth{Name: name}
		h.sources[name] = s
	}
	return s
}

// IsHealthy reports whether name may be queried right now: true if it has
// no failure history, or its backoff window has elapsed (a probe is due).
func (h *HealthTracker) IsHealthy(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sources[name]
	if !ok {
		return true
	}
	return !h.now().Before(s.BackoffUntil)
}

// RecordSuccess clears a source's failure history and backoff.
func (h *HealthTracker) RecordSuccess(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.entry(name)
	s.ConsecutiveFailures = 0
	s.LastError = ""
	s.LastSuccessAt = h.now()
	s.BackoffUntil = time.Time{}
}

// RecordFailure records a failure and extends the source's backoff window
// exponentially so a persistently-down source is probed with increasing
// patience rather than hammered every request.
func (h *HealthTracker) RecordFailure(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.entry(name)
	s.ConsecutiveFailures++
	if err != nil {
		s.LastError = err.Error()
	}
	s.LastFailureAt = h.now()

	shift := min(s.ConsecutiveFailures-1, 10) // cap the shift so this can't overflow
	backoff := healthBackoffBase * time.Duration(1<<uint(shift))
	if backoff > healthBackoffMax {
		backoff = healthBackoffMax
	}
	s.BackoffUntil = h.now().Add(backoff)
}

// Snapshot returns a copy of every tracked source's health, for a metrics
// endpoint to scrape.
func (h *HealthTracker) Snapshot() map[string]SourceHealth {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]SourceHealth, len(h.sources))
	for name, s := range h.sources {
		out[name] = *s
	}
	return out
}
