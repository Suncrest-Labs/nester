package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// A duplicate contract address must surface as a domain error rather than a
// raw driver error. Without the mapping the handler falls through to its
// default branch and answers 500, which tells the client to retry a request
// that can never succeed (nester#1148).
func TestMapRepositoryErrorContractAddressConflict(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		want       error
		wantMapped bool
	}{
		{
			name: "live contract address unique index",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "uq_vaults_contract_address_live",
			},
			want:       vault.ErrContractAddressRegistered,
			wantMapped: true,
		},
		{
			name: "wrapped by the driver",
			err: fmt.Errorf("insert vault: %w", &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "uq_vaults_contract_address_live",
			}),
			want:       vault.ErrContractAddressRegistered,
			wantMapped: true,
		},
		{
			name: "transaction hash conflict stays distinct",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "idx_vault_transactions_transaction_hash_unique",
			},
			want:       vault.ErrDuplicateTransaction,
			wantMapped: true,
		},
		{
			name: "unrelated unique violation is passed through",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "uq_something_else",
			},
			wantMapped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapRepositoryError(tt.err)
			if !tt.wantMapped {
				if !errors.Is(got, tt.err) {
					t.Fatalf("mapRepositoryError(%v) = %v, want the original error", tt.err, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapRepositoryError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestMapRepositoryErrorNil(t *testing.T) {
	if err := mapRepositoryError(nil); err != nil {
		t.Fatalf("mapRepositoryError(nil) = %v, want nil", err)
	}
}
