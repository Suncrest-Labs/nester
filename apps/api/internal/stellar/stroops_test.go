package stellar

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// oneUSDCInStroops is 1 USDC as a Soroban contract emits it: 1 * 10^7.
const oneUSDCInStroops = "10000000"

func TestStroopsToAssetUnits(t *testing.T) {
	cases := []struct {
		name    string
		stroops string
		want    string
	}{
		{"one unit", oneUSDCInStroops, "1"},
		{"zero", "0", "0"},
		{"one stroop is the smallest unit", "1", "0.0000001"},
		{"fractional units", "102500000", "10.25"},
		{"large amount beyond float64 exact range", "1000000000000000000", "100000000000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StroopsToAssetUnits(decimal.RequireFromString(tc.stroops))
			assert.Equal(t, tc.want, got.String())
		})
	}
}

// TestStroopsRoundTrip pins the two helpers as exact inverses so a future edit
// to one cannot silently desynchronise the chain-call path from the ledger.
func TestStroopsRoundTrip(t *testing.T) {
	for _, units := range []string{"1", "0.5", "10.25", "12345.6789012"} {
		got := StroopsToAssetUnits(AssetUnitsToStroops(decimal.RequireFromString(units)))
		assert.True(t, got.Equal(decimal.RequireFromString(units)), "round trip lost value for %s: got %s", units, got)
	}
}

// TestApplyIndexedEvent_Deposit_ConvertsStroopsToAssetUnits is the regression
// test for nester#1146: a 1 USDC on-chain deposit must credit 1, not
// 10_000_000.
func TestApplyIndexedEvent_Deposit_ConvertsStroopsToAssetUnits(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-1usdc",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     500,
		Data:       map[string]any{"amount": oneUSDCInStroops},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE vaults").
		WithArgs("1", event.ContractID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_WithdrawAndHarvest_ConvertStroops(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eventType string
	}{
		{"withdraw", "withdraw"},
		{"harvest", "harvest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			assert.NoError(t, err)
			defer db.Close()

			event := indexedEvent{
				ID:         "evt-" + tc.eventType,
				ContractID: "C1",
				EventType:  tc.eventType,
				Ledger:     501,
				Data:       map[string]any{"amount": oneUSDCInStroops},
			}

			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO processed_events").
				WithArgs(event.ID, event.Ledger).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE vaults").
				WithArgs("1", event.ContractID).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			processed, err := applyIndexedEvent(context.Background(), db, event)
			assert.NoError(t, err)
			assert.True(t, processed)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestIndexerAndVerifierAgreeOnScale is the round-trip requirement of
// nester#1146: for the same on-chain deposit, the amount the indexer writes to
// the balance and the amount the API path credits (via the chain event
// verifier) must be identical.
//
// Both paths are driven from the same stroop figure. The verifier's conversion
// is exercised through stroopsToDisplay, which is what
// verifiedEventFromXDR assigns to VerifiedVaultEvent.Amount and what
// VaultService.RecordDeposit then credits.
func TestIndexerAndVerifierAgreeOnScale(t *testing.T) {
	for _, stroops := range []string{oneUSDCInStroops, "1", "102500000", "999000000000"} {
		raw := decimal.RequireFromString(stroops)

		indexed, ok := extractEventAmountUnits(indexedEvent{Data: map[string]any{"amount": stroops}})
		assert.True(t, ok)

		viaVerifier := stroopsToDisplay(raw)

		assert.True(t, indexed.Equal(viaVerifier),
			"indexer credited %s but API path credited %s for %s stroops", indexed, viaVerifier, stroops)
	}
}
