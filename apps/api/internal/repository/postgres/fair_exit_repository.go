package postgres

import (
	"context"
	"database/sql"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// FairExitRepository reads the off-chain projections of the fair-ordering
// emergency queue (#814), penalty escrow (#805), and slippage-safe
// rebalance (#810) event streams populated by the Stellar event indexer.
// It is read-only: all writes happen in internal/stellar/indexer.go as
// on-chain events are ingested.
type FairExitRepository struct {
	db *sql.DB
}

func NewFairExitRepository(db *sql.DB) *FairExitRepository {
	return &FairExitRepository{db: db}
}

func (r *FairExitRepository) ListEmergencyQueue(ctx context.Context, contractAddress string) ([]vault.EmergencyQueueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_address, seq, shares_requested::text, shares_filled::text, status, enqueued_at, updated_at
		FROM emergency_withdrawal_queue
		WHERE vault_contract_address = $1
		ORDER BY seq ASC
	`, contractAddress)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]vault.EmergencyQueueEntry, 0)
	for rows.Next() {
		var (
			e               vault.EmergencyQueueEntry
			sharesRequested string
			sharesFilled    string
		)
		if err := rows.Scan(&e.ID, &e.UserAddress, &e.Seq, &sharesRequested, &sharesFilled, &e.Status, &e.EnqueuedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.SharesRequested, err = decimal.NewFromString(sharesRequested)
		if err != nil {
			return nil, err
		}
		e.SharesFilled, err = decimal.NewFromString(sharesFilled)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *FairExitRepository) ListPenaltyEvents(ctx context.Context, contractAddress string, limit int) ([]vault.PenaltyEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_address, amount::text, shares_burned::text, reason, occurred_at
		FROM penalty_events
		WHERE vault_contract_address = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, contractAddress, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]vault.PenaltyEvent, 0)
	for rows.Next() {
		var (
			e            vault.PenaltyEvent
			amount       string
			sharesBurned string
		)
		if err := rows.Scan(&e.ID, &e.UserAddress, &amount, &sharesBurned, &e.Reason, &e.OccurredAt); err != nil {
			return nil, err
		}
		e.Amount, err = decimal.NewFromString(amount)
		if err != nil {
			return nil, err
		}
		e.SharesBurned, err = decimal.NewFromString(sharesBurned)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r *FairExitRepository) ListPenaltyDistributions(ctx context.Context, contractAddress string, limit int) ([]vault.PenaltyDistribution, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, depositor_amount::text, treasury_amount::text, retained_dust::text, occurred_at
		FROM penalty_distributions
		WHERE vault_contract_address = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, contractAddress, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dists := make([]vault.PenaltyDistribution, 0)
	for rows.Next() {
		var (
			d               vault.PenaltyDistribution
			depositorAmount string
			treasuryAmount  string
			retainedDust    string
		)
		if err := rows.Scan(&d.ID, &depositorAmount, &treasuryAmount, &retainedDust, &d.OccurredAt); err != nil {
			return nil, err
		}
		d.DepositorAmount, err = decimal.NewFromString(depositorAmount)
		if err != nil {
			return nil, err
		}
		d.TreasuryAmount, err = decimal.NewFromString(treasuryAmount)
		if err != nil {
			return nil, err
		}
		d.RetainedDust, err = decimal.NewFromString(retainedDust)
		if err != nil {
			return nil, err
		}
		dists = append(dists, d)
	}
	return dists, rows.Err()
}

func (r *FairExitRepository) ListRebalanceLegs(ctx context.Context, contractAddress string, limit int) ([]vault.RebalanceLeg, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source_id, delta::text, amount_out::text, min_out::text, occurred_at
		FROM vault_rebalance_legs
		WHERE vault_contract_address = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, contractAddress, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	legs := make([]vault.RebalanceLeg, 0)
	for rows.Next() {
		var (
			l         vault.RebalanceLeg
			delta     string
			amountOut string
			minOut    string
		)
		if err := rows.Scan(&l.ID, &l.SourceID, &delta, &amountOut, &minOut, &l.OccurredAt); err != nil {
			return nil, err
		}
		l.Delta, err = decimal.NewFromString(delta)
		if err != nil {
			return nil, err
		}
		l.AmountOut, err = decimal.NewFromString(amountOut)
		if err != nil {
			return nil, err
		}
		l.MinOut, err = decimal.NewFromString(minOut)
		if err != nil {
			return nil, err
		}
		legs = append(legs, l)
	}
	return legs, rows.Err()
}

func (r *FairExitRepository) ListRebalanceCompletions(ctx context.Context, contractAddress string, limit int) ([]vault.RebalanceCompletion, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_hash, total_value_moved::text, realized_slippage_bps, occurred_at
		FROM vault_rebalance_completions
		WHERE vault_contract_address = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, contractAddress, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	completions := make([]vault.RebalanceCompletion, 0)
	for rows.Next() {
		var (
			c               vault.RebalanceCompletion
			totalValueMoved string
		)
		if err := rows.Scan(&c.ID, &c.PlanHash, &totalValueMoved, &c.RealizedSlippageBps, &c.OccurredAt); err != nil {
			return nil, err
		}
		c.TotalValueMoved, err = decimal.NewFromString(totalValueMoved)
		if err != nil {
			return nil, err
		}
		completions = append(completions, c)
	}
	return completions, rows.Err()
}
