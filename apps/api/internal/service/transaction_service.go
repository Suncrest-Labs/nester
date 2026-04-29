package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

type TransactionService struct {
	repository transaction.Repository
	horizonURL  string
	client      *http.Client
}

type RegisterTransactionInput struct {
	VaultID  uuid.UUID
	Type     transaction.TransactionType
	Amount   decimal.Decimal
	Currency string
	TxHash   string
}

func NewTransactionService(repository transaction.Repository, horizonURL string) *TransactionService {
	return &TransactionService{
		repository: repository,
		horizonURL: strings.TrimRight(strings.TrimSpace(horizonURL), "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *TransactionService) RegisterTransaction(ctx context.Context, input RegisterTransactionInput) (transaction.Transaction, error) {
	if input.VaultID == uuid.Nil || input.Amount.Cmp(decimal.Zero) <= 0 || strings.TrimSpace(input.Currency) == "" || strings.TrimSpace(input.TxHash) == "" {
		return transaction.Transaction{}, transaction.ErrInvalidTransaction
	}
	normalizedType := transaction.TransactionType(strings.ToLower(strings.TrimSpace(string(input.Type))))
	if !isSupportedTransactionType(normalizedType) {
		return transaction.Transaction{}, transaction.ErrInvalidType
	}

	model := transaction.Transaction{
		ID:        uuid.New(),
		VaultID:   input.VaultID,
		Type:      normalizedType,
		Amount:    input.Amount,
		Currency:  strings.ToUpper(strings.TrimSpace(input.Currency)),
		TxHash:    strings.TrimSpace(input.TxHash),
		Status:    transaction.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	return s.repository.Upsert(ctx, model)
}

func (s *TransactionService) GetTransaction(ctx context.Context, hash string) (transaction.Transaction, error) {
	if strings.TrimSpace(hash) == "" {
		return transaction.Transaction{}, transaction.ErrInvalidTransaction
	}

	model, err := s.repository.GetByHash(ctx, hash)
	if err != nil {
		return transaction.Transaction{}, err
	}

	switch model.Status {
	case transaction.StatusCompleted, transaction.StatusFailed:
		return model, nil
	}

	horizonStatus, confirmedAt, errorReason, err := s.lookupHorizonTransaction(ctx, hash)
	if err != nil {
		if errors.Is(err, errTransactionPending) {
			return model, nil
		}
		return transaction.Transaction{}, err
	}

	switch horizonStatus {
	case transaction.StatusCompleted, transaction.StatusFailed:
		updated, updateErr := s.repository.UpdateStatus(ctx, hash, horizonStatus, confirmedAt, errorReason)
		if updateErr != nil {
			return transaction.Transaction{}, updateErr
		}
		return updated, nil
	default:
		return model, nil
	}
}

type horizonTransactionResponse struct {
	Successful bool   `json:"successful"`
	CreatedAt  string `json:"created_at"`
	ResultXdr  string `json:"result_xdr"`
}

var errTransactionPending = errors.New("transaction pending")

func (s *TransactionService) lookupHorizonTransaction(ctx context.Context, hash string) (transaction.TransactionStatus, *time.Time, string, error) {
	if s.horizonURL == "" {
		return transaction.StatusPending, nil, "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/transactions/%s", s.horizonURL, hash), nil)
	if err != nil {
		return "", nil, "", err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return transaction.StatusPending, nil, "", errTransactionPending
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, "", fmt.Errorf("horizon status lookup failed: %s", resp.Status)
	}

	var payload horizonTransactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", nil, "", err
	}

	confirmedAt, err := time.Parse(time.RFC3339, payload.CreatedAt)
	if err != nil {
		return "", nil, "", err
	}

	if payload.Successful {
		return transaction.StatusCompleted, &confirmedAt, "", nil
	}

	return transaction.StatusFailed, &confirmedAt, strings.TrimSpace(payload.ResultXdr), nil
}

func isSupportedTransactionType(value transaction.TransactionType) bool {
	switch value {
	case transaction.TypeDeposit, transaction.TypeWithdrawal, transaction.TypeSettlement:
		return true
	default:
		return false
	}
}
