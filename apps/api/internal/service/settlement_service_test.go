package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// ── In-memory stub repository ───────────────────────────────────────────────

type stubRepo struct {
	data map[uuid.UUID]*offramp.Settlement
}

func newStubRepo() *stubRepo {
	return &stubRepo{data: make(map[uuid.UUID]*offramp.Settlement)}
}

func (r *stubRepo) Create(_ context.Context, model offramp.Settlement) (offramp.Settlement, error) {
	model.CreatedAt = time.Now().UTC()
	cp := model
	r.data[model.ID] = &cp
	return cp, nil
}

func (r *stubRepo) GetByID(_ context.Context, id uuid.UUID) (offramp.Settlement, error) {
	s, ok := r.data[id]
	if !ok {
		return offramp.Settlement{}, offramp.ErrSettlementNotFound
	}
	return *s, nil
}

func (r *stubRepo) GetByUserID(_ context.Context, userID uuid.UUID, filter offramp.SettlementStatus) ([]offramp.Settlement, error) {
	var out []offramp.Settlement
	for _, s := range r.data {
		if s.UserID != userID {
			continue
		}
		if filter != "" && s.Status != filter {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func (r *stubRepo) UpdateStatus(_ context.Context, id uuid.UUID, status offramp.SettlementStatus, completedAt *time.Time) error {
	s, ok := r.data[id]
	if !ok {
		return offramp.ErrSettlementNotFound
	}
	s.Status = status
	s.CompletedAt = completedAt
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func validInput() service.InitiateSettlementInput {
	return service.InitiateSettlementInput{
		UserID:       uuid.New(),
		VaultID:      uuid.New(),
		Amount:       decimal.NewFromFloat(100.00),
		Currency:     "USDC",
		FiatCurrency: "KES",
		FiatAmount:   decimal.NewFromFloat(13000.00),
		ExchangeRate: decimal.NewFromFloat(130.00),
		Destination: offramp.Destination{
			Type:          "mobile_money",
			Provider:      "mpesa",
			AccountNumber: "0712345678",
			AccountName:   "Jane Doe",
		},
	}
}

// ── Tests: InitiateSettlement ─────────────────────────────────────────────────

func TestInitiateSettlement_CreatesInInitiatedStatus(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())

	s, err := svc.InitiateSettlement(context.Background(), validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Status != offramp.StatusInitiated {
		t.Errorf("want status %q, got %q", offramp.StatusInitiated, s.Status)
	}
	if s.ID == uuid.Nil {
		t.Error("expected a non-nil UUID")
	}
}

func TestInitiateSettlement_ReturnsIDAndAllFields(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()

	s, err := svc.InitiateSettlement(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.UserID != in.UserID {
		t.Errorf("user_id mismatch")
	}
	if s.VaultID != in.VaultID {
		t.Errorf("vault_id mismatch")
	}
	if !s.Amount.Equal(in.Amount) {
		t.Errorf("amount mismatch")
	}
	if s.Currency != "USDC" {
		t.Errorf("currency mismatch: got %s", s.Currency)
	}
	if s.Destination.Provider != "mpesa" {
		t.Errorf("destination provider mismatch")
	}
}

func TestInitiateSettlement_NilUserIDReturnsError(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()
	in.UserID = uuid.Nil

	_, err := svc.InitiateSettlement(context.Background(), in)
	if !errors.Is(err, offramp.ErrInvalidSettlement) {
		t.Errorf("expected ErrInvalidSettlement, got %v", err)
	}
}

func TestInitiateSettlement_ZeroAmountReturnsError(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()
	in.Amount = decimal.Zero

	_, err := svc.InitiateSettlement(context.Background(), in)
	if !errors.Is(err, offramp.ErrInvalidAmount) {
		t.Errorf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestInitiateSettlement_EmptyCurrencyReturnsError(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()
	in.Currency = "   "

	_, err := svc.InitiateSettlement(context.Background(), in)
	if !errors.Is(err, offramp.ErrInvalidSettlement) {
		t.Errorf("expected ErrInvalidSettlement, got %v", err)
	}
}

func TestInitiateSettlement_EmptyAccountNumberReturnsError(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()
	in.Destination.AccountNumber = ""

	_, err := svc.InitiateSettlement(context.Background(), in)
	if !errors.Is(err, offramp.ErrInvalidSettlement) {
		t.Errorf("expected ErrInvalidSettlement, got %v", err)
	}
}

func TestInitiateSettlement_BankTransferRequiresBankCode(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	in := validInput()
	in.Destination.Type = "bank_transfer"
	in.Destination.BankCode = "" // missing

	_, err := svc.InitiateSettlement(context.Background(), in)
	if !errors.Is(err, offramp.ErrInvalidSettlement) {
		t.Errorf("expected ErrInvalidSettlement, got %v", err)
	}
}

// ── Tests: GetSettlement ──────────────────────────────────────────────────────

func TestGetSettlement_ReturnsExistingRecord(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())

	created, _ := svc.InitiateSettlement(context.Background(), validInput())
	fetched, err := svc.GetSettlement(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("ID mismatch")
	}
}

func TestGetSettlement_UnknownIDReturnsNotFound(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())

	_, err := svc.GetSettlement(context.Background(), uuid.New())
	if !errors.Is(err, offramp.ErrSettlementNotFound) {
		t.Errorf("expected ErrSettlementNotFound, got %v", err)
	}
}

// ── Tests: GetUserSettlements ─────────────────────────────────────────────────

func TestGetUserSettlements_ReturnsAllForUser(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())

	in := validInput()
	svc.InitiateSettlement(context.Background(), in)
	svc.InitiateSettlement(context.Background(), in)

	list, err := svc.GetUserSettlements(context.Background(), in.UserID, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("want 2 settlements, got %d", len(list))
	}
}

func TestGetUserSettlements_FilterByStatus(t *testing.T) {
	repo := newStubRepo()
	svc := service.NewSettlementService(repo)

	in := validInput()
	s1, _ := svc.InitiateSettlement(context.Background(), in)

	// Advance s1 to liquidity_matched
	svc.UpdateStatus(context.Background(), service.UpdateStatusInput{
		SettlementID: s1.ID,
		CallerID:     in.UserID,
		NewStatus:    offramp.StatusLiquidityMatched,
	})

	// s2 stays in initiated
	svc.InitiateSettlement(context.Background(), in)

	initiated, _ := svc.GetUserSettlements(context.Background(), in.UserID, "initiated")
	if len(initiated) != 1 {
		t.Errorf("want 1 initiated, got %d", len(initiated))
	}

	matched, _ := svc.GetUserSettlements(context.Background(), in.UserID, "liquidity_matched")
	if len(matched) != 1 {
		t.Errorf("want 1 liquidity_matched, got %d", len(matched))
	}
}

func TestGetUserSettlements_InvalidStatusReturnsError(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())

	_, err := svc.GetUserSettlements(context.Background(), uuid.New(), "nonsense")
	if !errors.Is(err, offramp.ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got %v", err)
	}
}

// ── Tests: UpdateStatus / state machine ─────────────────────────────────────

func TestUpdateStatus_FullLifecycleToConfirmed(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())

	transitions := []offramp.SettlementStatus{
		offramp.StatusLiquidityMatched,
		offramp.StatusFiatDispatched,
		offramp.StatusConfirmed,
	}

	for _, next := range transitions {
		updated, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
			SettlementID: s.ID,
			CallerID:     s.UserID,
			NewStatus:    next,
		})
		if err != nil {
			t.Fatalf("transition to %q failed: %v", next, err)
		}
		if updated.Status != next {
			t.Errorf("want status %q, got %q", next, updated.Status)
		}
		s = updated
	}

	if s.CompletedAt == nil {
		t.Error("confirmed settlement should have completed_at set")
	}
}

func TestUpdateStatus_FullLifecycleToFailed(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())

	updated, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     s.UserID,
		NewStatus:    offramp.StatusFailed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != offramp.StatusFailed {
		t.Errorf("want failed, got %s", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Error("failed settlement should have completed_at set")
	}
}

func TestUpdateStatus_FailedAfterLiquidityMatched(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())
	svc.UpdateStatus(ctx, service.UpdateStatusInput{SettlementID: s.ID, CallerID: s.UserID, NewStatus: offramp.StatusLiquidityMatched})

	updated, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     s.UserID,
		NewStatus:    offramp.StatusFailed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != offramp.StatusFailed {
		t.Errorf("want failed, got %s", updated.Status)
	}
}

func TestUpdateStatus_InvalidTransitionRejected(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())

	// initiated → fiat_dispatched is not a valid transition
	_, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     s.UserID,
		NewStatus:    offramp.StatusFiatDispatched,
	})
	if !errors.Is(err, offramp.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdateStatus_CannotLeaveConfirmed(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())
	svc.UpdateStatus(ctx, service.UpdateStatusInput{SettlementID: s.ID, CallerID: s.UserID, NewStatus: offramp.StatusLiquidityMatched})
	svc.UpdateStatus(ctx, service.UpdateStatusInput{SettlementID: s.ID, CallerID: s.UserID, NewStatus: offramp.StatusFiatDispatched})
	svc.UpdateStatus(ctx, service.UpdateStatusInput{SettlementID: s.ID, CallerID: s.UserID, NewStatus: offramp.StatusConfirmed})

	_, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     s.UserID,
		NewStatus:    offramp.StatusInitiated,
	})
	if !errors.Is(err, offramp.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition from terminal state, got %v", err)
	}
}

func TestUpdateStatus_CannotLeaveFailed(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())
	svc.UpdateStatus(ctx, service.UpdateStatusInput{SettlementID: s.ID, CallerID: s.UserID, NewStatus: offramp.StatusFailed})

	_, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     s.UserID,
		NewStatus:    offramp.StatusInitiated,
	})
	if !errors.Is(err, offramp.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition from terminal state, got %v", err)
	}
}

func TestUpdateStatus_ForbiddenForNonOwner(t *testing.T) {
	svc := service.NewSettlementService(newStubRepo())
	ctx := context.Background()

	s, _ := svc.InitiateSettlement(ctx, validInput())
	attacker := uuid.New() // different from s.UserID

	_, err := svc.UpdateStatus(ctx, service.UpdateStatusInput{
		SettlementID: s.ID,
		CallerID:     attacker,
		NewStatus:    offramp.StatusLiquidityMatched,
	})
	if !errors.Is(err, offramp.ErrForbidden) {
		t.Errorf("expected ErrForbidden for non-owner, got %v", err)
	}
}
