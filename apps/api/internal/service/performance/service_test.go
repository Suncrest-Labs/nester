package performance

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// stubVaultRepository is a minimal implementation of vault.Repository for testing
type stubVaultRepository struct {
	vaults []vault.Vault
	err    error
}

func (s *stubVaultRepository) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	return vault.Vault{}, errors.New("not implemented")
}

func (s *stubVaultRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	return vault.Vault{}, errors.New("not implemented")
}

func (s *stubVaultRepository) GetUserVaults(_ context.Context, userID uuid.UUID) ([]vault.Vault, error) {
	return s.vaults, s.err
}

func (s *stubVaultRepository) RecordDeposit(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) RecordHarvest(_ context.Context, input vault.HarvestRecordInput) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepository) ListUserVaultTransactions(_ context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepository) ListVaults(_ context.Context, _ vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}