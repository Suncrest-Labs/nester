package protocoltvl

import (
	"fmt"
	"time"
)

// DefaultAnomalyDropPct is the default percentage TVL must fall between two
// consecutive snapshots to be flagged as an anomaly. It is deliberately much
// tighter than TVLDropThreshold (20% over 24h): a drop this large within a
// single snapshot interval points at an exploit or a bug rather than gradual
// outflow.
const DefaultAnomalyDropPct = 10.0

// AnomalyMaxSnapshotAge bounds how old the prior snapshot may be for a drop
// to count as "within a snapshot interval". After a long scheduler outage the
// last snapshot is stale and a lower reading says nothing about suddenness.
const AnomalyMaxSnapshotAge = 2 * time.Hour

// Anomaly describes a sudden TVL drop between two consecutive snapshots.
type Anomaly struct {
	ProtocolSlug   string
	PreviousTVLUSD float64
	CurrentTVLUSD  float64
	// DropPct is the positive percentage the TVL fell by.
	DropPct float64
	// ThresholdPct is the configured threshold that was met or exceeded.
	ThresholdPct float64
	DetectedAt   time.Time
}

// Explanation is a short, human-readable statement of the anomaly.
func (a Anomaly) Explanation() string {
	return fmt.Sprintf(
		"TVL fell %.1f%% within one snapshot interval ($%.0f -> $%.0f), threshold %.1f%%",
		a.DropPct, a.PreviousTVLUSD, a.CurrentTVLUSD, a.ThresholdPct,
	)
}

// DetectAnomaly reports whether currentTVLUSD, observed at now, is a sudden
// drop relative to prior (the previous snapshot). thresholdPct <= 0 falls back
// to DefaultAnomalyDropPct. It returns nil when there is no usable prior
// snapshot, the prior is older than AnomalyMaxSnapshotAge, or the drop is
// below the threshold. A negative current reading is treated as zero.
func DetectAnomaly(slug string, prior *Snapshot, currentTVLUSD float64, thresholdPct float64, now time.Time) *Anomaly {
	if prior == nil || prior.TVLUSD <= 0 {
		return nil
	}
	if now.Sub(prior.SnapshottedAt) > AnomalyMaxSnapshotAge {
		return nil
	}
	if thresholdPct <= 0 {
		thresholdPct = DefaultAnomalyDropPct
	}
	if currentTVLUSD < 0 {
		currentTVLUSD = 0
	}

	dropPct := (prior.TVLUSD - currentTVLUSD) / prior.TVLUSD * 100
	if dropPct < thresholdPct {
		return nil
	}
	return &Anomaly{
		ProtocolSlug:   slug,
		PreviousTVLUSD: prior.TVLUSD,
		CurrentTVLUSD:  currentTVLUSD,
		DropPct:        dropPct,
		ThresholdPct:   thresholdPct,
		DetectedAt:     now,
	}
}
