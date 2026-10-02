package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/flags"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// staticFlagReader satisfies flags.Reader and returns whatever Flag was set for
// the named flag, or flags.ErrNotFound when unknown.
type staticFlagReader struct {
	flags map[string]flags.Flag
}

func (r *staticFlagReader) Get(_ context.Context, name string) (flags.Flag, error) {
	f, ok := r.flags[name]
	if !ok {
		return flags.Flag{}, flags.ErrNotFound
	}
	return f, nil
}

func newCohortEvaluator(userIDs []string) *flags.Evaluator {
	reader := &staticFlagReader{flags: map[string]flags.Flag{
		"mainnet_deposit_allowlist": {
			Name:    "mainnet_deposit_allowlist",
			Type:    flags.TypeCohort,
			Enabled: true,
			Cohort:  userIDs,
		},
	}}
	return flags.NewEvaluator(reader)
}

func newPercentageEvaluator(pct float64) *flags.Evaluator {
	reader := &staticFlagReader{flags: map[string]flags.Flag{
		"mainnet_deposit_allowlist": {
			Name:       "mainnet_deposit_allowlist",
			Type:       flags.TypePercentage,
			Enabled:    true,
			Percentage: pct,
		},
	}}
	return flags.NewEvaluator(reader)
}

func TestFlagDepositAllowlistGate_CohortAllowed(t *testing.T) {
	userID := uuid.New()
	gate := service.NewFlagDepositAllowlistGate(newCohortEvaluator([]string{userID.String()}))
	if err := gate.EnsureDepositAllowed(context.Background(), userID); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestFlagDepositAllowlistGate_CohortDenied(t *testing.T) {
	allowedID := uuid.New()
	deniedID := uuid.New()
	gate := service.NewFlagDepositAllowlistGate(newCohortEvaluator([]string{allowedID.String()}))
	err := gate.EnsureDepositAllowed(context.Background(), deniedID)
	if err == nil {
		t.Fatal("expected ErrDepositNotAllowlisted, got nil")
	}
	if err != vault.ErrDepositNotAllowlisted {
		t.Fatalf("expected ErrDepositNotAllowlisted, got %v", err)
	}
}

func TestFlagDepositAllowlistGate_PercentageFull(t *testing.T) {
	// 100% means everyone is in.
	gate := service.NewFlagDepositAllowlistGate(newPercentageEvaluator(100))
	if err := gate.EnsureDepositAllowed(context.Background(), uuid.New()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestFlagDepositAllowlistGate_PercentageZero(t *testing.T) {
	// 0% means nobody is in.
	gate := service.NewFlagDepositAllowlistGate(newPercentageEvaluator(0))
	err := gate.EnsureDepositAllowed(context.Background(), uuid.New())
	if err != vault.ErrDepositNotAllowlisted {
		t.Fatalf("expected ErrDepositNotAllowlisted, got %v", err)
	}
}

func TestFlagDepositAllowlistGate_FlagMissingFailsClosed(t *testing.T) {
	// No flags registered — reader returns ErrNotFound; fail-safe is false.
	reader := &staticFlagReader{flags: map[string]flags.Flag{}}
	evaluator := flags.NewEvaluator(reader)
	gate := service.NewFlagDepositAllowlistGate(evaluator)
	err := gate.EnsureDepositAllowed(context.Background(), uuid.New())
	if err != vault.ErrDepositNotAllowlisted {
		t.Fatalf("expected ErrDepositNotAllowlisted when flag missing, got %v", err)
	}
}
