package stellar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
)

// MainnetRetentionWindowLedgers is the approximate 7-day retention window on
// mainnet.sorobanrpc.com (~5s per ledger => 120,960 ledgers).
const MainnetRetentionWindowLedgers uint64 = 120_960

// SoakSample captures a single poll cycle observation during an indexer soak.
type SoakSample struct {
	CursorLedger uint64
	TipLedger    uint64
	LagSeconds   float64
	RPCError     bool
}

// SoakAuditReport summarizes the results of a soak run and its invariant checks.
type SoakAuditReport struct {
	TotalSamples       int     `json:"total_samples"`
	LagP50Seconds      float64 `json:"lag_p50_seconds"`
	LagP99Seconds      float64 `json:"lag_p99_seconds"`
	MaxLagLedgers      uint64  `json:"max_lag_ledgers"`
	ErrorCount         int     `json:"error_count"`
	DuplicateEvents    int     `json:"duplicate_events"`
	CursorRegressions  int     `json:"cursor_regressions"`
	RetentionBreaches  int     `json:"retention_breaches"`
	ContinuousAdvance  bool    `json:"continuous_advance"`
}

// EvaluateSoakSamples computes lag percentiles (p50/p99), checks monotonic cursor
// advancement, and verifies that ledger lag never approaches the mainnet RPC
// retention window.
func EvaluateSoakSamples(samples []SoakSample, duplicateEvents int) (SoakAuditReport, error) {
	if len(samples) == 0 {
		return SoakAuditReport{}, errors.New("soak audit requires at least one sample")
	}

	lags := make([]float64, 0, len(samples))
	var maxLagLedgers uint64
	var errorCount int
	var regressions int
	var retentionBreaches int

	prevCursor := samples[0].CursorLedger
	firstCursor := samples[0].CursorLedger
	lastCursor := samples[len(samples)-1].CursorLedger

	for i, s := range samples {
		lags = append(lags, s.LagSeconds)
		if s.RPCError {
			errorCount++
		}
		if i > 0 && s.CursorLedger < prevCursor {
			regressions++
		}
		if s.CursorLedger > prevCursor {
			prevCursor = s.CursorLedger
		}
		if s.TipLedger >= s.CursorLedger {
			lagLedgers := s.TipLedger - s.CursorLedger
			if lagLedgers > maxLagLedgers {
				maxLagLedgers = lagLedgers
			}
			if lagLedgers >= MainnetRetentionWindowLedgers {
				retentionBreaches++
			}
		}
	}

	sort.Float64s(lags)

	return SoakAuditReport{
		TotalSamples:      len(samples),
		LagP50Seconds:     percentile(lags, 0.50),
		LagP99Seconds:     percentile(lags, 0.99),
		MaxLagLedgers:     maxLagLedgers,
		ErrorCount:        errorCount,
		DuplicateEvents:   duplicateEvents,
		CursorRegressions: regressions,
		RetentionBreaches: retentionBreaches,
		ContinuousAdvance: regressions == 0 && lastCursor > firstCursor && retentionBreaches == 0 && duplicateEvents == 0,
	}, nil
}

// CountDuplicateProcessedEvents queries processed_events for any duplicate event_id entries.
func CountDuplicateProcessedEvents(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, errors.New("db is required")
	}
	var dupGroups int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT event_id
			FROM processed_events
			GROUP BY event_id
			HAVING COUNT(*) > 1
		) AS dup
	`).Scan(&dupGroups)
	if err != nil {
		return 0, fmt.Errorf("query duplicate processed_events: %w", err)
	}
	return dupGroups, nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
