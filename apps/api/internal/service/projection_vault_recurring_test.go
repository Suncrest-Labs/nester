package service

import (
	"context"
	"errors"
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
// err, when set, is returned instead of a lookup result: the operational
// failure case (a dead database, a cancelled context).
type recurringGoalRepo struct {
	savingsgoal.Repository
	byVault map[uuid.UUID]savingsgoal.SavingsGoal
	err     error
}

func (r *recurringGoalRepo) GetByVaultID(_ context.Context, vaultID uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	if r.err != nil {
		return nil, r.err
	}
	g, ok := r.byVault[vaultID]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}

// recurringScheduleRepo resolves GetActiveByGoal from a map. Absence returns
// (nil, nil), which is what postgres.SavingsScheduleRepository.GetActiveByGoal
// actually does for a linked goal with no active schedule — it maps
// sql.ErrNoRows to a nil schedule and a nil error, never ErrScheduleNotFound.
// This mock previously returned the sentinel, so the "goal exists but has no
// active schedule" path was never exercised against the real contract.
type recurringScheduleRepo struct {
	savingsschedule.Repository
	byGoal map[uuid.UUID]savingsschedule.SavingsSchedule
	err    error
}

func (r *recurringScheduleRepo) GetActiveByGoal(_ context.Context, goalID, _ uuid.UUID) (*savingsschedule.SavingsSchedule, error) {
	if r.err != nil {
		return nil, r.err
	}
	s, ok := r.byGoal[goalID]
	if !ok {
		return nil, nil
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

// TestCalculateVaultProjection_ScheduleLookup covers how the projection
// resolves (or fails to resolve) a recurring schedule. The distinction that
// matters: genuine absence falls back to a single-deposit projection, but an
// operational failure must surface as an error rather than as a successful
// projection that silently assumes the user never contributes again.
func TestCalculateVaultProjection_ScheduleLookup(t *testing.T) {
	dbDown := errors.New("connection refused")

	tests := []struct {
		name string
		// linkGoal seeds a goal for the vault; linkSchedule seeds an active
		// schedule for that goal.
		linkGoal     bool
		linkSchedule bool
		goalErr      error
		scheduleErr  error

		wantErr          error
		wantContribution decimal.Decimal
		wantSource       string
		wantCadence      string
	}{
		{
			name:             "active schedule is rolled into the projection",
			linkGoal:         true,
			linkSchedule:     true,
			wantContribution: decimal.NewFromInt(200),
			wantSource:       "schedule",
			wantCadence:      "monthly",
		},
		{
			// The case the old mock could not reach: postgres returns
			// (nil, nil) here, not a sentinel error.
			name:             "linked goal with no active schedule falls back",
			linkGoal:         true,
			linkSchedule:     false,
			wantContribution: decimal.Zero,
			wantSource:       "single_deposit",
		},
		{
			name:             "no linked goal at all falls back",
			linkGoal:         false,
			wantContribution: decimal.Zero,
			wantSource:       "single_deposit",
		},
		{
			name:    "a failing goal lookup is returned, not swallowed",
			goalErr: dbDown,
			wantErr: dbDown,
		},
		{
			name:        "a failing schedule lookup is returned, not swallowed",
			linkGoal:    true,
			scheduleErr: dbDown,
			wantErr:     dbDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			vaultID := uuid.New()
			goalID := uuid.New()
			apy := decimal.NewFromFloat(0.05)

			goals := map[uuid.UUID]savingsgoal.SavingsGoal{}
			if tt.linkGoal {
				goals[vaultID] = savingsgoal.SavingsGoal{ID: goalID, UserID: userID, VaultID: &vaultID}
			}
			schedules := map[uuid.UUID]savingsschedule.SavingsSchedule{}
			if tt.linkSchedule {
				schedules[goalID] = savingsschedule.SavingsSchedule{
					ID:        uuid.New(),
					UserID:    userID,
					GoalID:    goalID,
					VaultID:   vaultID,
					Amount:    decimal.NewFromInt(200),
					Currency:  "USD",
					Frequency: savingsschedule.FrequencyMonthly,
					IsActive:  true,
				}
			}

			svc := NewProjectionService(
				NewCompoundInterestCalculator(),
				&recurringVaultRepo{v: vault.Vault{ID: vaultID, UserID: userID, Currency: "USD"}},
				(performance.SnapshotRepository)(nil),
				&recurringGoalRepo{byVault: goals, err: tt.goalErr},
				&recurringScheduleRepo{byGoal: schedules, err: tt.scheduleErr},
			)

			out, err := svc.CalculateVaultProjection(context.Background(), projection.VaultProjectionInput{
				VaultID:           vaultID,
				Deposit:           decimal.NewFromInt(500),
				Period:            "6m",
				CompoundFrequency: "monthly",
				APYOverride:       &apy,
			})

			if tt.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantErr,
					"an operational lookup failure must surface, not degrade to a zero-contribution projection")
				require.Nil(t, out)
				return
			}

			require.NoError(t, err)
			require.True(t, out.Input.MonthlyContribution.Equal(tt.wantContribution),
				"monthly contribution: want %s, got %s", tt.wantContribution, out.Input.MonthlyContribution)
			require.Equal(t, tt.wantSource, out.Assumptions.ContributionSource)
			require.Equal(t, tt.wantCadence, out.Assumptions.Cadence)
		})
	}
}
