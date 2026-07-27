# Market context trust boundary

Market-context signals are sourced, low-trust information. They are not facts,
recommendations, or transaction instructions.

Only protocol-specific allowlisted URLs are ingested. Source terms and polling
intervals must be configured per publisher; the batch job caches each document
for six hours by default. Ingested text is enclosed as untrusted data. A signal
is discarded unless its protocol, publisher, and URL exactly match metadata
supplied by the ingestion boundary.

Single-source confidence is capped at `0.45`. Matching direction and type from
independent publishers can raise confidence, capped at `0.80`. The advisory risk
contribution is capped at `0.15`.

## No-fund-movement rule

The market-context module has no wallet, contract, rebalance, or transaction
dependency. It returns only a bounded advisory risk sub-score. Automated fund
movement continues to require on-chain metrics and deterministic thresholds.
Operators and user interfaces must label these signals as context, include their
source links, and display the disclaimer supplied by the API.

The extraction call uses the configured current Claude model and tool-constrained
structured output. `MarketContextBatchJob.run_once` is intended for the existing
leader-elected scheduler and cost-governance batch layer; it persists accepted
signals through its injected store.
