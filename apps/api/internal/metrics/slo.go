package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// This file defines the service level indicators for nester#1056. The metrics
// in metrics.go describe how the process behaves; these describe whether the
// product worked for a user, which is what an SLO is written against.
//
// The cardinality policy in the package doc applies here without exception,
// and the financial domain makes it sharper: an amount, a vault ID, a wallet
// address, or a transaction hash is never a label. Every label below is a
// closed Go constant set, so the series count is fixed at compile time and
// cannot be moved by traffic or by a caller.

// flowDurationBuckets covers the settlement path for a deposit or withdrawal,
// which is dominated by Soroban ledger close time rather than by anything the
// API does. Ledgers close at roughly 5s intervals, so a single-ledger
// confirmation lands near 5-6s and a two-ledger confirmation near 11s. The
// buckets are placed to resolve that band: 2.5s and 5s bracket the
// single-ledger case, 10s and 15s the two-and-three-ledger cases, and 30s/60s
// separate "slow" from "the chain or the invoker is stuck". Reusing
// requestDurationBuckets here would put half the resolution below 250ms,
// where no confirmed deposit can ever land.
var flowDurationBuckets = []float64{
	1, 2.5, 5, 7.5, 10, 15, 20, 30, 45, 60, 120,
}

// Flow identifies a user-visible financial flow.
//
// Deposits and withdrawals get separate SLOs rather than one "transaction"
// SLO because their failure modes and their user impact differ: a failed
// deposit is money that did not start earning, a failed withdrawal is money
// the user cannot reach, and the second is materially worse. Averaging them
// into one indicator hides a withdrawal outage behind healthy deposit volume.
type Flow string

const (
	FlowDeposit    Flow = "deposit"
	FlowWithdrawal Flow = "withdrawal"
)

// FlowOutcome is the terminal classification of one flow attempt.
//
// The split between rejected, cancelled, and the two failure kinds is the
// whole substance of the deposit and withdrawal SLIs, so each is defined
// against the code that produces it:
//
//   - OutcomeSucceeded: the chain call returned without error and the ledger
//     row was written. Both halves are required; a chain success whose
//     database write failed is not a success, because the user's balance does
//     not reflect their money.
//
//   - OutcomeRejected: the request never reached the chain because the
//     service correctly refused it — zero or negative amount, excess decimal
//     scale, closed vault, insufficient balance, unknown vault. These are the
//     service working as designed against an invalid request, so they are
//     excluded from the SLI denominator. Excluding them is what stops a
//     client looping an invalid request from manufacturing an SLO breach.
//
//   - OutcomeCancelled: the user declined the wallet signature, or abandoned
//     the attempt before submission. Excluded from the denominator on the
//     issue's instruction, and separately counted so that a wave of
//     cancellations caused by a broken signing prompt is still visible rather
//     than silently dropped.
//
//   - OutcomeFailedChain: the Soroban invocation returned an error, timed
//     out, or the transaction failed on-chain. Counted as a failure. This
//     includes upstream RPC problems: from the user's position "the network
//     was down" and "we could not reach the network" are the same event, and
//     an SLI that excused infrastructure it does not own would report health
//     during a total outage.
//
//   - OutcomeFailedInternal: the API failed on its own side — database write
//     failure, panic, unhandled path. Always a failure.
//
// A contract-level rejection that is really a user error rather than a system
// fault is the one genuinely ambiguous case; ErrBelowMinDeposit is mapped to
// OutcomeRejected because the contract refused a request that the API should
// have caught, and counting it as a chain failure would let a UI bug that
// permits sub-minimum deposits burn the deposit error budget.
type FlowOutcome string

const (
	OutcomeSucceeded      FlowOutcome = "succeeded"
	OutcomeRejected       FlowOutcome = "rejected"
	OutcomeCancelled      FlowOutcome = "cancelled"
	OutcomeFailedChain    FlowOutcome = "failed_chain"
	OutcomeFailedInternal FlowOutcome = "failed_internal"
)

// sloCollectors holds the SLI instrumentation. It is embedded in Metrics
// rather than living in a second registry so that one scrape carries both the
// infrastructure and the product view; correlating them across two endpoints
// during an incident is exactly the friction a runbook cannot afford.
type sloCollectors struct {
	flowAttemptsTotal *prometheus.CounterVec
	flowDuration      *prometheus.HistogramVec

	indexerLagLedgers      prometheus.Gauge
	indexerLagStaleness    prometheus.Gauge
	indexerLagScrapeErrors prometheus.Counter

	reconcileRunsTotal      *prometheus.CounterVec
	reconcileDivergences    *prometheus.CounterVec
	reconcileLastRunAge     prometheus.Gauge
	pendingSubmissions      prometheus.Gauge
	pendingSubmissionOldest prometheus.Gauge
}

// ReconcileOutcome is the terminal classification of one reconciliation pass.
//
// A pass that could not even list its work (succeeded == false, the
// list-pending query failed) is a different operational event from one that
// ran and found nothing wrong, and folding them together would let a
// permanently broken reconciler report a clean divergence count forever.
type ReconcileOutcome string

const (
	// ReconcileCompleted means the pass ran to completion. It says nothing
	// about whether divergences were found — that is the divergence counter.
	ReconcileCompleted ReconcileOutcome = "completed"
	// ReconcileFailed means the pass could not enumerate its work and did
	// not inspect anything.
	ReconcileFailed ReconcileOutcome = "failed"
)

// DivergenceKind classifies a single reconciliation finding.
//
// The values mirror reconciliation.DiscrepancyType rather than inventing a
// parallel vocabulary, so an operator reading an alert and an engineer reading
// the reconciliation engine are using the same words. It is a closed set, so
// cardinality is fixed at compile time in line with the package policy.
type DivergenceKind string

const (
	// DivergenceMissing is a record the chain has and we do not.
	DivergenceMissing DivergenceKind = "missing"
	// DivergenceExtra is a record we have and the chain does not.
	DivergenceExtra DivergenceKind = "extra"
	// DivergenceMismatch is a record both sides have with differing values —
	// the most serious kind, since it means a balance is wrong rather than
	// absent.
	DivergenceMismatch DivergenceKind = "mismatch"
	// DivergenceStuck is a record that has not reached a terminal state
	// within its expected window.
	DivergenceStuck DivergenceKind = "stuck"
)

func newSLOCollectors() *sloCollectors {
	return &sloCollectors{
		// The SLI counter. Success rate is
		//   succeeded / (succeeded + failed_chain + failed_internal)
		// with rejected and cancelled excluded from both halves. Keeping all
		// five outcomes on one counter rather than splitting successes and
		// failures into separate metrics means the denominator is always
		// derivable from a single series selector, and an outcome that is
		// added later cannot silently fall out of the denominator.
		flowAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "flow",
			Name:      "attempts_total",
			Help:      "Terminal outcomes of deposit and withdrawal attempts, by flow and outcome.",
		}, []string{"flow", "outcome"}),

		// Latency is observed only for attempts that reached a terminal
		// state on-chain, and the outcome label is retained because a
		// failure that takes 45s to surface and a success that takes 6s are
		// different operational events. Rejected attempts are never observed
		// here: they terminate in microseconds without touching the chain,
		// and including them would drag every percentile toward zero and
		// make the latency SLI report health during a chain stall.
		flowDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: "flow",
			Name:      "duration_seconds",
			Help:      "End-to-end latency of deposit and withdrawal attempts that reached the chain, by flow and outcome.",
			Buckets:   flowDurationBuckets,
		}, []string{"flow", "outcome"}),

		// Balance freshness. Ledgers rather than seconds because the
		// indexer's own unit is the ledger sequence, and converting to time
		// would bake in an assumed close interval that varies.
		indexerLagLedgers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "indexer",
			Name:      "lag_ledgers",
			Help:      "Network ledger tip minus last successfully indexed ledger.",
		}),

		// A lag gauge alone cannot distinguish "lag is 0" from "the sampler
		// died and the value is frozen at its last write". This gauge is the
		// age of the lag reading, so an alert can require freshness of the
		// freshness signal. Without it the balance SLI fails silently in the
		// most dangerous direction: reporting perfect health.
		indexerLagStaleness: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "indexer",
			Name:      "lag_last_sample_age_seconds",
			Help:      "Seconds since the indexer lag gauge was last successfully updated.",
		}),

		// Sampling errors are counted rather than folded into the lag value,
		// because writing a sentinel lag on error would be indistinguishable
		// from a real stall and would page for the wrong reason.
		indexerLagScrapeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "indexer",
			Name:      "lag_sample_errors_total",
			Help:      "Failed attempts to sample indexer lag.",
		}),

		// Reconciliation (nester#1108). The reconciler compares our record of
		// the money path against the chain. Until now it reported only to the
		// log, which means a divergence — the single most serious signal this
		// system can produce, since it says a balance is wrong — was visible
		// only to whoever happened to read the logs.
		//
		// Runs are counted by outcome so an alert can tell "reconciled, all
		// clean" from "could not reconcile at all". The latter is the
		// dangerous silence: no divergences are reported precisely because
		// nothing was checked.
		reconcileRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "reconcile",
			Name:      "runs_total",
			Help:      "Reconciliation passes by outcome.",
		}, []string{"outcome"}),

		// Divergences are counted, never gauged. A gauge would report the
		// current pass's count and silently erase a divergence that was
		// found and then resolved by a correction, losing exactly the
		// history an incident review needs. The kind label carries the
		// reconciliation engine's own discrepancy vocabulary.
		reconcileDivergences: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "reconcile",
			Name:      "divergences_total",
			Help:      "Reconciliation findings where our record and the chain disagree, by kind.",
		}, []string{"kind"}),

		// As with indexer lag, a divergence counter alone cannot distinguish
		// "no divergences" from "the reconciler stopped running an hour ago".
		// This gauge is the age of the last completed pass, so an alert can
		// require the checker itself to be alive.
		reconcileLastRunAge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "reconcile",
			Name:      "last_run_age_seconds",
			Help:      "Seconds since the last reconciliation pass completed.",
		}),

		// Pending submissions (nester#1108). A submission that never reaches
		// a terminal state is money in flight that the user cannot see and
		// the protocol has not settled. Depth alone is ambiguous — a large
		// queue that drains is healthy, a small one that never drains is not
		// — so the age of the oldest entry is published alongside it and is
		// the value worth alerting on.
		pendingSubmissions: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "submission",
			Name:      "pending_count",
			Help:      "Transactions submitted to the chain still awaiting a terminal status.",
		}),

		pendingSubmissionOldest: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "submission",
			Name:      "pending_oldest_age_seconds",
			Help:      "Age of the oldest transaction still awaiting a terminal status.",
		}),
	}
}

func (c *sloCollectors) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		c.flowAttemptsTotal,
		c.flowDuration,
		c.indexerLagLedgers,
		c.indexerLagStaleness,
		c.indexerLagScrapeErrors,
		c.reconcileRunsTotal,
		c.reconcileDivergences,
		c.reconcileLastRunAge,
		c.pendingSubmissions,
		c.pendingSubmissionOldest,
	}
}

// RecordFlowAttempt records one terminal deposit or withdrawal outcome.
//
// duration is the time from the service accepting the request to the terminal
// outcome. It is observed only when the attempt reached the chain: a rejected
// attempt passes a non-positive duration and is counted but not timed.
//
// Callers must call this exactly once per attempt, on every return path. The
// helper in the service layer wraps it in a defer so a newly added early
// return cannot silently drop an attempt from the denominator, which would
// inflate the reported success rate — the one direction of error an SLI must
// never make.
func (m *Metrics) RecordFlowAttempt(flow Flow, outcome FlowOutcome, duration time.Duration) {
	if m == nil {
		return
	}

	m.slo.flowAttemptsTotal.WithLabelValues(string(flow), string(outcome)).Inc()

	if duration > 0 && outcome != OutcomeRejected {
		m.slo.flowDuration.WithLabelValues(string(flow), string(outcome)).Observe(duration.Seconds())
	}
}

// SetIndexerLag publishes the current indexer lag and resets the staleness
// gauge, which the sampler's own ticker ages between calls.
func (m *Metrics) SetIndexerLag(lagLedgers uint64) {
	if m == nil {
		return
	}

	m.slo.indexerLagLedgers.Set(float64(lagLedgers))
	m.slo.indexerLagStaleness.Set(0)
}

// SetIndexerLagSampleAge publishes how old the lag reading is.
func (m *Metrics) SetIndexerLagSampleAge(age time.Duration) {
	if m == nil {
		return
	}

	m.slo.indexerLagStaleness.Set(age.Seconds())
}

// RecordIndexerLagSampleError counts a failed lag sample.
func (m *Metrics) RecordIndexerLagSampleError() {
	if m == nil {
		return
	}

	m.slo.indexerLagScrapeErrors.Inc()
}

// RecordReconcileRun records one completed reconciliation pass and resets the
// last-run age, so an alert can distinguish a clean pass from a reconciler
// that has stopped running.
//
// Call this on every return path of a pass, including the early return when
// the work could not be listed — a pass that failed to enumerate is exactly
// the case an operator must not mistake for a clean result.
func (m *Metrics) RecordReconcileRun(outcome ReconcileOutcome) {
	if m == nil {
		return
	}

	m.slo.reconcileRunsTotal.WithLabelValues(string(outcome)).Inc()
	m.slo.reconcileLastRunAge.Set(0)
}

// RecordReconcileDivergence counts one finding where our record and the chain
// disagree. Call it once per finding, not once per pass, so the counter
// reflects how much disagreement exists rather than how often any was seen.
func (m *Metrics) RecordReconcileDivergence(kind DivergenceKind) {
	if m == nil {
		return
	}

	m.slo.reconcileDivergences.WithLabelValues(string(kind)).Inc()
}

// SetReconcileLastRunAge publishes how long ago the last pass completed. The
// reconciler's own ticker ages this between passes.
func (m *Metrics) SetReconcileLastRunAge(age time.Duration) {
	if m == nil {
		return
	}

	m.slo.reconcileLastRunAge.Set(age.Seconds())
}

// SetPendingSubmissions publishes the current in-flight submission backlog:
// how many transactions await a terminal status, and how old the oldest is.
//
// oldest is zero when the queue is empty, which reads correctly on a graph —
// an empty queue has no waiting time — and keeps the alert on this gauge from
// firing on a drained queue.
func (m *Metrics) SetPendingSubmissions(count int, oldest time.Duration) {
	if m == nil {
		return
	}

	m.slo.pendingSubmissions.Set(float64(count))
	if count == 0 {
		m.slo.pendingSubmissionOldest.Set(0)
		return
	}
	m.slo.pendingSubmissionOldest.Set(oldest.Seconds())
}
