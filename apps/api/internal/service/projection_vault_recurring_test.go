package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/projection"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// recurringVaultRepo serves a single fixed vault. Only GetVault is exercised.
type recurringVaultRepo struct {
	vault.Repository
	v vault.Vault
}

func (r *recurringVaultRepo) GetVault(_ context.Context, _ uuid.UUID) (vault.Vault, error) {
	return r.v, nil
}

// recurringGoalRepo resolves GetByVaultID from a map, returning
// ErrGoalNotFound (matching the real repository's contract) when absent.
type recurringGoalRepo struct {
	savingsgoal.Repository
	byVault map[uuid.UUID]savingsgoal.SavingsGoal
}

func (r *recurringGoalRepo) GetByVaultID(_ context.Context, vaultID uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := r.byVault[vaultID]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}

// recurringScheduleRepo resolves GetActiveByGoal from a map, returning
// ErrScheduleNotFound (matching the real repository's contract) when absent.
type recurringScheduleRepo struct {
	savingsschedule.Repository
	byGoal map[uuid.UUID]savingsschedule.SavingsSchedule
}

func (r *recurringScheduleRepo) GetActiveByGoal(_ context.Context, goalID, _ uuid.UUID) (*savingsschedule.SavingsSchedule, error) {
	s, ok := r.byGoal[goalID]
	if !ok {
		return nil, savingsschedule.ErrScheduleNotFound
	}
	return &s, nil
}

// TestCalculateVaultProjection_RecurringContribution is the regression test
// for #1224: a vault whose goal has an active recurring schedule must
// project a higher final balance than an identical vault with no schedule,
// and the response must say which assumption produced which number.
func TestCalculateVaultProjection_RecurringContribution(t *testing.T) {
	userID := uuid.New()
	vaultWithSchedule := uuid.New()
	vaultNoSchedule := uuid.New()
	goalID := uuid.New()

	apy := decimal.NewFromFloat(0.08)
	deposit := decimal.NewFromInt(1000)

	makeVault := func(id uuid.UUID) vault.Vault {
		return vault.Vault{ID: id, UserID: userID, Currency: "USD"}
	}

	goalRepo := &recurringGoalRepo{
		byVault: map[uuid.UUID]savingsgoal.SavingsGoal{
			vaultWithSchedule: {ID: goalID, UserID: userID, VaultID: &vaultWithSchedule},
		},
	}
	scheduleRepo := &recurringScheduleRepo{
		byGoal: map[uuid.UUID]savingsschedule.SavingsSchedule{
			goalID: {
				ID:        uuid.New(),
				UserID:    userID,
				GoalID:    goalID,
				VaultID:   vaultWithSchedule,
				Amount:    decimal.NewFromInt(200),
				Currency:  "USD",
				Frequency: savingsschedule.FrequencyMonthly,
				IsActive:  true,
			},
		},
	}

	newService := func(vaultID uuid.UUID) *ProjectionService {
		return NewProjectionService(
			NewCompoundInterestCalculator(),
			&recurringVaultRepo{v: makeVault(vaultID)},
			(performance.SnapshotRepository)(nil),
			goalRepo,
			scheduleRepo,
		)
	}

	input := func(vaultID uuid.UUID) projection.VaultProjectionInput {
		return projection.VaultProjectionInput{
			VaultID:           vaultID,
			Deposit:           deposit,
			Period:            "12m",
			CompoundFrequency: "monthly",
			APYOverride:       &apy,
		}
	}

	withSchedule, err := newService(vaultWithSchedule).CalculateVaultProjection(context.Background(), input(vaultWithSchedule))
	require.NoError(t, err)

	withoutSchedule, err := newService(vaultNoSchedule).CalculateVaultProjection(context.Background(), input(vaultNoSchedule))
	require.NoError(t, err)

	// Acceptance criterion: projections differ when a schedule exists.
	require.True(t, withSchedule.Summary.FinalBalance.GreaterThan(withoutSchedule.Summary.FinalBalance),
		"a user with an active recurring schedule should project a higher final balance")
	require.True(t, withSchedule.Input.MonthlyContribution.Equal(decimal.NewFromInt(200)),
		"monthly contribution should be resolved from the active schedule")

	// Acceptance criterion: a user with no schedule still gets an unchanged
	// single-deposit projection.
	require.True(t, withoutSchedule.Input.MonthlyContribution.IsZero(),
		"a vault with no linked goal/schedule should fall back to single-deposit")
	require.Equal(t, "single_deposit", withoutSchedule.Assumptions.ContributionSource)

	// Acceptance criterion: the assumptions behind the projection are stated
	// in the response.
	require.Equal(t, "schedule", withSchedule.Assumptions.ContributionSource)
	require.Equal(t, "monthly", withSchedule.Assumptions.Cadence)
	require.True(t, withSchedule.Assumptions.RecurringAmount.Equal(decimal.NewFromInt(200)))
	require.Equal(t, 12, withSchedule.Assumptions.TimelineMonths)
}

// TestCalculateVaultProjection_NoGoalLinked confirms a vault with no linked
// savings goal at all (GetByVaultID returns ErrGoalNotFound) still produces
// the baseline single-deposit projection rather than erroring out.
func TestCalculateVaultProjection_NoGoalLinked(t *testing.T) {
	vaultID := uuid.New()
	apy := decimal.NewFromFloat(0.05)

	svc := NewProjectionService(
		NewCompoundInterestCalculator(),
		&recurringVaultRepo{v: vault.Vault{ID: vaultID, Currency: "USD"}},
		(performance.SnapshotRepository)(nil),
		&recurringGoalRepo{byVault: map[uuid.UUID]savingsgoal.SavingsGoal{}},
		&recurringScheduleRepo{byGoal: map[uuid.UUID]savingsschedule.SavingsSchedule{}},
	)

	out, err := svc.CalculateVaultProjection(context.Background(), projection.VaultProjectionInput{
		VaultID:           vaultID,
		Deposit:           decimal.NewFromInt(500),
		Period:            "6m",
		CompoundFrequency: "monthly",
		APYOverride:       &apy,
	})
	require.NoError(t, err)
	require.True(t, out.Input.MonthlyContribution.IsZero())
	require.Equal(t, "single_deposit", out.Assumptions.ContributionSource)
}
