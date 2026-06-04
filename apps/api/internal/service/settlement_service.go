package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bankaccount"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
)

var (
	renuban  = regexp.MustCompile(`^\d{10}$`)
	rebankCode = regexp.MustCompile(`^\d{3,9}$`)
)

// SavedBankAccountResolver loads decrypted bank details for settlement.
type SavedBankAccountResolver interface {
	ResolveForSettlement(ctx context.Context, userID, accountID uuid.UUID) (bankaccount.BankAccount, error)
}

type SettlementService struct {
	repository   offramp.Repository
	bankAccounts SavedBankAccountResolver
	sep24        *Sep24Resolver // POC: SEP-24 integration
}

func NewSettlementService(repository offramp.Repository, bankAccounts SavedBankAccountResolver) *SettlementService {
	return &SettlementService{repository: repository, bankAccounts: bankAccounts}
}

// SetSep24Resolver injects the SEP-24 resolver for POC testing.
func (s *SettlementService) SetSep24Resolver(resolver *Sep24Resolver) {
	s.sep24 = resolver
}

// InitiateSEP24Withdrawal is a POC method to start the offramp flow via the anchor.
func (s *SettlementService) InitiateSEP24Withdrawal(ctx context.Context, id uuid.UUID) (string, string, error) {
	if s.sep24 == nil {
		return "", "", fmt.Errorf("SEP-24 resolver not configured")
	}

	settlement, err := s.GetSettlement(ctx, id)
	if err != nil {
		return "", "", err
	}

	return s.sep24.InitiateInteractiveWithdrawal(ctx, settlement.FiatCurrency, settlement.FiatAmount)
}

// CheckSEP24Status is a POC method to poll the anchor for transaction status.
func (s *SettlementService) CheckSEP24Status(ctx context.Context, txID string) (string, error) {
	if s.sep24 == nil {
		return "", fmt.Errorf("SEP-24 resolver not configured")
	}
	return s.sep24.PollTransactionStatus(ctx, txID)
}

// InitiateSettlementInput carries caller-supplied data for a new settlement.
type InitiateSettlementInput struct {
	UserID        uuid.UUID
	VaultID       uuid.UUID
	Amount        decimal.Decimal
	Currency      string
	FiatCurrency  string
	FiatAmount    decimal.Decimal
	ExchangeRate  decimal.Decimal
	BankAccountID *uuid.UUID
	Destination   offramp.Destination
}

// UpdateStatusInput carries the target state for a status transition.
type UpdateStatusInput struct {
	SettlementID uuid.UUID
	// CallerID is the authenticated user's UUID from the JWT. The service
	// verifies it matches the settlement's UserID before applying the update.
	CallerID  uuid.UUID
	NewStatus offramp.SettlementStatus
}

// InitiateSettlement validates input, creates a settlement in the `initiated`
// state, and persists it via the repository.
func (s *SettlementService) InitiateSettlement(ctx context.Context, input InitiateSettlementInput) (offramp.Settlement, error) {
	if input.UserID == uuid.Nil || input.VaultID == uuid.Nil {
		return offramp.Settlement{}, offramp.ErrInvalidSettlement
	}

	if input.Amount.Cmp(decimal.Zero) <= 0 {
		return offramp.Settlement{}, offramp.ErrInvalidAmount
	}
	if input.FiatAmount.Cmp(decimal.Zero) <= 0 {
		return offramp.Settlement{}, offramp.ErrInvalidAmount
	}
	if input.ExchangeRate.Cmp(decimal.Zero) <= 0 {
		return offramp.Settlement{}, offramp.ErrInvalidAmount
	}

	if decimalScale(input.Amount) > offramp.MaxAmountScale ||
		decimalScale(input.FiatAmount) > offramp.MaxAmountScale ||
		decimalScale(input.ExchangeRate) > offramp.MaxAmountScale {
		return offramp.Settlement{}, offramp.ErrInvalidPrecision
	}

	if strings.TrimSpace(input.Currency) == "" || strings.TrimSpace(input.FiatCurrency) == "" {
		return offramp.Settlement{}, offramp.ErrInvalidSettlement
	}

	if input.BankAccountID != nil {
		if s.bankAccounts == nil {
			return offramp.Settlement{}, offramp.ErrInvalidSettlement
		}
		saved, err := s.bankAccounts.ResolveForSettlement(ctx, input.UserID, *input.BankAccountID)
		if err != nil {
			return offramp.Settlement{}, mapBankAccountSettlementError(err)
		}
		input.Destination = offramp.Destination{
			Type:          "bank_transfer",
			Provider:      "bank",
			AccountNumber: saved.AccountNumber,
			AccountName:   saved.AccountName,
			BankCode:      saved.BankCode,
		}
		if strings.TrimSpace(input.FiatCurrency) == "" {
			input.FiatCurrency = saved.Currency
		}
	}

	if err := validateDestination(input.Destination); err != nil {
		return offramp.Settlement{}, err
	}

	model := offramp.Settlement{
		ID:           uuid.New(),
		UserID:       input.UserID,
		VaultID:      input.VaultID,
		Amount:       input.Amount,
		Currency:     strings.ToUpper(strings.TrimSpace(input.Currency)),
		FiatCurrency: strings.ToUpper(strings.TrimSpace(input.FiatCurrency)),
		FiatAmount:   input.FiatAmount,
		ExchangeRate: input.ExchangeRate,
		Destination:  input.Destination,
		Status:       offramp.StatusInitiated,
	}

	return s.repository.Create(ctx, model)
}

// GetSettlement retrieves a single settlement by ID.
func (s *SettlementService) GetSettlement(ctx context.Context, id uuid.UUID) (offramp.Settlement, error) {
	if id == uuid.Nil {
		return offramp.Settlement{}, offramp.ErrInvalidSettlement
	}
	return s.repository.GetByID(ctx, id)
}

// ListUserSettlements returns a paginated, filterable list of settlements for a user.
func (s *SettlementService) ListUserSettlements(
	ctx context.Context,
	userID uuid.UUID,
	filter offramp.UserListFilter,
) ([]offramp.Settlement, int, string, error) {
	if userID == uuid.Nil {
		return nil, 0, "", offramp.ErrInvalidSettlement
	}
	if filter.Status != "" {
		if _, err := offramp.ParseStatus(filter.Status); err != nil {
			return nil, 0, "", err
		}
	}
	sortField := filter.SortField
	if sortField == "" {
		sortField = "created_at"
	}
	sortOrder := filter.SortOrder
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if filter.Cursor != "" && (sortField != "created_at" || sortOrder == "asc") {
		return nil, 0, "", offramp.ErrInvalidSettlement
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 {
		filter.PerPage = 20
	}
	return s.repository.ListByUserID(ctx, userID, filter)
}

// UpdateStatus validates the state transition and persists the new status.
// Terminal states (confirmed, failed) set completed_at to now.
func (s *SettlementService) UpdateStatus(ctx context.Context, input UpdateStatusInput) (offramp.Settlement, error) {
	if input.SettlementID == uuid.Nil {
		return offramp.Settlement{}, offramp.ErrInvalidSettlement
	}

	current, err := s.repository.GetByID(ctx, input.SettlementID)
	if err != nil {
		return offramp.Settlement{}, err
	}

	if current.UserID != input.CallerID {
		return offramp.Settlement{}, offramp.ErrSettlementNotFound
	}

	if !current.CanTransitionTo(input.NewStatus) {
		return offramp.Settlement{}, offramp.ErrInvalidTransition
	}

	var completedAt *time.Time
	if input.NewStatus == offramp.StatusConfirmed || input.NewStatus == offramp.StatusFailed {
		now := time.Now().UTC()
		completedAt = &now
	}

	if err := s.repository.UpdateStatus(ctx, input.SettlementID, input.NewStatus, completedAt); err != nil {
		return offramp.Settlement{}, err
	}

	return s.repository.GetByID(ctx, input.SettlementID)
}

func validateDestination(d offramp.Destination) error {
	if strings.TrimSpace(d.Type) == "" ||
		strings.TrimSpace(d.Provider) == "" ||
		strings.TrimSpace(d.AccountNumber) == "" ||
		strings.TrimSpace(d.AccountName) == "" {
		return offramp.ErrInvalidSettlement
	}
	if !renuban.MatchString(d.AccountNumber) {
		return offramp.ErrInvalidSettlement
	}
	if d.Type == "bank_transfer" {
		if strings.TrimSpace(d.BankCode) == "" {
			return offramp.ErrInvalidSettlement
		}
		if !rebankCode.MatchString(d.BankCode) {
			return offramp.ErrInvalidSettlement
		}
	}
	return nil
}

func mapBankAccountSettlementError(err error) error {
	switch {
	case errors.Is(err, bankaccount.ErrNotFound):
		return offramp.ErrInvalidSettlement
	case errors.Is(err, bankaccount.ErrForbidden):
		return offramp.ErrForbidden
	default:
		return err
	}
}
