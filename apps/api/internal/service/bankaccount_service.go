package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/bankaccount"
)

var (
	supportedCurrencies = map[string]bool{"NGN": true, "GHS": true, "KES": true}
	supportedCountries  = map[string]bool{"NG": true, "GH": true, "KE": true}
	reAccountDigits     = regexp.MustCompile(`^\d{10}$`)
)

// BankAccountService manages saved payout accounts for users.
type BankAccountService struct {
	repo      bankaccount.Repository
	cipher    *crypto.AccountCipher
	resolver  *BankService
}

func NewBankAccountService(
	repo bankaccount.Repository,
	cipher *crypto.AccountCipher,
	resolver *BankService,
) *BankAccountService {
	return &BankAccountService{repo: repo, cipher: cipher, resolver: resolver}
}

// Add validates and persists a new saved bank account.
func (s *BankAccountService) Add(ctx context.Context, userID uuid.UUID, input bankaccount.AddInput) (bankaccount.PublicView, error) {
	if s.cipher == nil {
		return bankaccount.PublicView{}, bankaccount.ErrEncryptionUnavailable
	}
	if userID == uuid.Nil {
		return bankaccount.PublicView{}, bankaccount.ErrInvalidInput
	}

	normalized, err := normalizeAddInput(input)
	if err != nil {
		return bankaccount.PublicView{}, err
	}

	if s.resolver != nil && normalized.Country == "NG" {
		info, resolveErr := s.resolver.ResolveAccount(ctx, normalized.AccountNumber, normalized.BankCode, normalized.Country)
		if resolveErr != nil {
			return bankaccount.PublicView{}, mapResolveError(resolveErr)
		}
		if info != nil && strings.TrimSpace(info.AccountName) != "" {
			normalized.AccountName = strings.TrimSpace(info.AccountName)
		}
	}

	encrypted, err := s.cipher.Encrypt(normalized.AccountNumber)
	if err != nil {
		return bankaccount.PublicView{}, err
	}
	fingerprint := s.cipher.Fingerprint(normalizeAccountKey(normalized.AccountNumber, normalized.BankCode))

	model := bankaccount.BankAccount{
		ID:            uuid.New(),
		UserID:        userID,
		BankName:      normalized.BankName,
		BankCode:      normalized.BankCode,
		AccountNumber: normalized.AccountNumber,
		AccountName:   normalized.AccountName,
		Currency:      normalized.Currency,
		Country:       normalized.Country,
		IsDefault:     normalized.SetAsDefault,
		VerifiedAt:    normalized.VerifiedAt,
	}

	if model.IsDefault {
		if err := s.repo.ClearDefaultForCurrency(ctx, userID, model.Currency); err != nil {
			return bankaccount.PublicView{}, err
		}
	}

	created, err := s.repo.Create(ctx, model, encrypted, fingerprint)
	if err != nil {
		return bankaccount.PublicView{}, err
	}
	return bankaccount.ToPublicView(created), nil
}

// List returns all saved accounts for a user (masked account numbers).
func (s *BankAccountService) List(ctx context.Context, userID uuid.UUID) ([]bankaccount.PublicView, error) {
	if userID == uuid.Nil {
		return nil, bankaccount.ErrInvalidInput
	}
	accounts, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]bankaccount.PublicView, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, bankaccount.ToPublicView(a))
	}
	return out, nil
}

// SetDefault marks one account as the default for its currency.
func (s *BankAccountService) SetDefault(ctx context.Context, userID, accountID uuid.UUID) (bankaccount.PublicView, error) {
	if userID == uuid.Nil || accountID == uuid.Nil {
		return bankaccount.PublicView{}, bankaccount.ErrInvalidInput
	}
	account, _, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return bankaccount.PublicView{}, err
	}
	if account.UserID != userID {
		return bankaccount.PublicView{}, bankaccount.ErrForbidden
	}
	if err := s.repo.ClearDefaultForCurrency(ctx, userID, account.Currency); err != nil {
		return bankaccount.PublicView{}, err
	}
	if err := s.repo.SetDefault(ctx, accountID, userID); err != nil {
		return bankaccount.PublicView{}, err
	}
	account.IsDefault = true
	return bankaccount.ToPublicView(account), nil
}

// Update applies partial updates (default flag, optional bank name label).
func (s *BankAccountService) Update(
	ctx context.Context,
	userID, accountID uuid.UUID,
	input bankaccount.UpdateInput,
) (bankaccount.PublicView, error) {
	if input.SetAsDefault != nil && *input.SetAsDefault {
		return s.SetDefault(ctx, userID, accountID)
	}
	return bankaccount.PublicView{}, bankaccount.ErrInvalidInput
}

// Remove deletes a saved account owned by the user.
func (s *BankAccountService) Remove(ctx context.Context, userID, accountID uuid.UUID) error {
	if userID == uuid.Nil || accountID == uuid.Nil {
		return bankaccount.ErrInvalidInput
	}
	account, _, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.UserID != userID {
		return bankaccount.ErrForbidden
	}
	return s.repo.Delete(ctx, accountID)
}

// ResolveForSettlement loads and decrypts a saved account for settlement initiation.
func (s *BankAccountService) ResolveForSettlement(
	ctx context.Context,
	userID, accountID uuid.UUID,
) (bankaccount.BankAccount, error) {
	if s.cipher == nil {
		return bankaccount.BankAccount{}, bankaccount.ErrEncryptionUnavailable
	}
	account, encrypted, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return bankaccount.BankAccount{}, err
	}
	if account.UserID != userID {
		return bankaccount.BankAccount{}, bankaccount.ErrForbidden
	}
	plain, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return bankaccount.BankAccount{}, err
	}
	account.AccountNumber = plain
	return account, nil
}

func normalizeAddInput(input bankaccount.AddInput) (bankaccount.AddInput, error) {
	input.BankName = strings.TrimSpace(input.BankName)
	input.BankCode = strings.TrimSpace(input.BankCode)
	input.AccountNumber = strings.TrimSpace(input.AccountNumber)
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.Country = strings.ToUpper(strings.TrimSpace(input.Country))

	if input.BankName == "" || input.AccountName == "" || input.AccountNumber == "" {
		return input, bankaccount.ErrInvalidInput
	}
	if !supportedCurrencies[input.Currency] {
		return input, bankaccount.ErrInvalidInput
	}
	if !supportedCountries[input.Country] {
		return input, bankaccount.ErrInvalidInput
	}
	if input.Country == "NG" && !reAccountDigits.MatchString(input.AccountNumber) {
		return input, bankaccount.ErrInvalidInput
	}
	if input.Country == "NG" && input.BankCode == "" {
		return input, bankaccount.ErrInvalidInput
	}
	if input.VerifiedAt == nil {
		now := time.Now().UTC()
		input.VerifiedAt = &now
	}
	return input, nil
}

func normalizeAccountKey(accountNumber, bankCode string) string {
	return strings.TrimSpace(accountNumber) + "|" + strings.TrimSpace(bankCode)
}

func mapResolveError(err error) error {
	switch err {
	case bank.ErrAccountNotFound:
		return bankaccount.ErrInvalidInput
	case bank.ErrInvalidAccountNumber, bank.ErrInvalidBankCode, bank.ErrInvalidCountry:
		return bankaccount.ErrInvalidInput
	default:
		return err
	}
}
