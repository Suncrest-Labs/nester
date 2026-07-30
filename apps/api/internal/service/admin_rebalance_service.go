package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func (s *AdminService) TriggerRebalance(
	ctx context.Context,
	vaultID uuid.UUID,
	req admindomain.RebalanceRequest,
) (admindomain.RebalanceResponse, error) {
	if vaultID == uuid.Nil {
		return admindomain.RebalanceResponse{}, ErrInvalidAdminInput
	}

	strategy := strings.TrimSpace(strings.ToLower(req.Strategy))
	if strategy == "" {
		strategy = admindomain.RebalanceStrategyAuto
	}
	if strategy != admindomain.RebalanceStrategyAuto {
		return admindomain.RebalanceResponse{}, fmt.Errorf("%w: unsupported strategy %q", ErrInvalidAdminInput, req.Strategy)
	}

	detail, err := s.repository.GetVaultDetail(ctx, vaultID)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}
	if detail.Status != vault.StatusActive {
		return admindomain.RebalanceResponse{}, fmt.Errorf("%w: vault status is %s", ErrRebalanceNotEligible, detail.Status)
	}

	inFlight, err := s.repository.HasInFlightRebalance(ctx, vaultID)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}
	if inFlight {
		return admindomain.RebalanceResponse{}, ErrRebalanceInFlight
	}

	record := admindomain.VaultRebalanceRecord{
		VaultID:  vaultID,
		Strategy: strategy,
		DryRun:   req.DryRun,
		Status:   admindomain.RebalanceStatusPending,
	}
	if len(req.TargetAllocations) > 0 {
		if deltasJSON, err := json.Marshal(req.TargetAllocations); err == nil {
			record.ProjectedDeltas = deltasJSON
		}
	}
	record, err = s.repository.CreateVaultRebalance(ctx, record)
	if err != nil {
		if strings.Contains(err.Error(), "rebalance_in_flight") {
			return admindomain.RebalanceResponse{}, ErrRebalanceInFlight
		}
		return admindomain.RebalanceResponse{}, err
	}

	if req.DryRun {
		return s.finishDryRunRebalance(ctx, detail, record)
	}
	return s.finishSubmitRebalance(ctx, detail, record)
}

func (s *AdminService) finishDryRunRebalance(
	ctx context.Context,
	detail admindomain.VaultDetail,
	record admindomain.VaultRebalanceRecord,
) (admindomain.RebalanceResponse, error) {
	if err := s.chainInvoker.SimulateRebalanceVault(ctx, detail.ContractAddress); err != nil {
		msg := err.Error()
		record.Status = admindomain.RebalanceStatusFailed
		record.ErrorMessage = &msg
		_, _ = s.repository.UpdateVaultRebalance(ctx, record)
		return admindomain.RebalanceResponse{}, fmt.Errorf("rebalance simulation failed: %w", err)
	}

	deltas := projectAutoRebalanceDeltas(detail.Allocations, detail.CurrentBalance)
	deltasJSON, err := json.Marshal(deltas)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}

	record.Status = admindomain.RebalanceStatusDryRun
	record.ProjectedDeltas = deltasJSON
	record, err = s.repository.UpdateVaultRebalance(ctx, record)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}

	return admindomain.RebalanceResponse{
		Status:          "dry_run",
		RebalanceID:     record.ID,
		ProjectedDeltas: deltas,
	}, nil
}

func (s *AdminService) finishSubmitRebalance(
	ctx context.Context,
	detail admindomain.VaultDetail,
	record admindomain.VaultRebalanceRecord,
) (admindomain.RebalanceResponse, error) {
	txHash, err := s.chainInvoker.RebalanceVault(ctx, detail.ContractAddress)
	if err != nil {
		msg := err.Error()
		record.Status = admindomain.RebalanceStatusFailed
		record.ErrorMessage = &msg
		_, _ = s.repository.UpdateVaultRebalance(ctx, record)
		return admindomain.RebalanceResponse{}, fmt.Errorf("rebalance submission failed: %w", err)
	}

	record.Status = admindomain.RebalanceStatusSubmitted
	record.TxHash = &txHash
	record, err = s.repository.UpdateVaultRebalance(ctx, record)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}

	return admindomain.RebalanceResponse{
		Status:                "submitted",
		TxHash:                txHash,
		RebalanceID:           record.ID,
		EstimatedCompletionMS: rebalanceEstimatedCompletionMS,
	}, nil
}
