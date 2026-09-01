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
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// VaultBalanceApplier credits or debits a vault's balance for a transaction
// that has been confirmed on-chain. Implementations MUST be idempotent on
// txHash so a retried confirmation never double-applies. The production
// implementation is the Postgres vault repository; tests pass a fake.
//
// This is the single boundary through which a vault balance ever moves as a
// result of a deposit/withdrawal: balance is applied only after Horizon
// reports the transaction in a closed, successful ledger — never at submission
// time (issue #496).
type VaultBalanceApplier interface {
	// ApplyConfirmedDeposit credits a confirmed on-chain deposit. capCheck, when
	// non-nil, is evaluated against the launch caps but its result never blocks
	// the credit — see the implementation's doc comment for why an
	// already-confirmed on-chain deposit cannot simply be refused. A non-nil
	// capWarning return means the deposit was credited despite exceeding a
	// launch cap, and should be logged/alerted on by the caller.
	ApplyConfirmedDeposit(
		ctx context.Context,
		vaultID uuid.UUID,
		userID uuid.UUID,
		amount decimal.Decimal,
		txHash string,
		capCheck func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error,
	) (capWarning error, err error)
	ApplyConfirmedWithdrawal(ctx context.Context, vaultID uuid.UUID, userID uuid.UUID, amount decimal.Decimal, txHash string) error
}

// ConfirmedDepositCapEvaluator is the transaction-safe cap evaluation used to
// warn (never block) on a confirmed on-chain deposit that exceeds a launch
// cap. Satisfied by *caps.Checker's EvaluateTotals. Declared here, rather than
// importing domain/caps, to keep this package's dependency-free convention
// (matching depositAuditRepository/capEvaluator in the vault service).
type ConfirmedDepositCapEvaluator interface {
	EvaluateTotals(ctx context.Context, userID uuid.UUID, amount, currentUserTotal, currentGlobalTotal decimal.Decimal) error
}

type TransactionService struct {
	repository transaction.Repository
	horizonURL string
	client     *http.Client
	balance    VaultBalanceApplier
	// vaults resolves the vault a transaction belongs to, so a confirmation
	// can be checked against the vault's real contract address and currency
	// (nester#1145). See SetVaultLookup.
	vaults TransactionVaultLookup
	// capsChecker evaluates a confirmed deposit against the launch caps
	// (nester CodeRabbit finding: confirmed on-chain deposits previously
	// bypassed the caps entirely). Optional: nil disables the check, same as
	// the interactive deposit path. See SetCapsChecker.
	capsChecker ConfirmedDepositCapEvaluator
}

// SetCapsChecker installs the launch-cap evaluator used to flag (not block —
// see VaultBalanceApplier's doc comment) a confirmed on-chain deposit that
// exceeds a launch cap. Production wires the same *caps.Checker instance used
// by VaultService.SetCapsChecker.
func (s *TransactionService) SetCapsChecker(checker ConfirmedDepositCapEvaluator) {
	s.capsChecker = checker
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

// SetHTTPClient replaces the HTTP client used to confirm transactions against
// Horizon. It exists so startup can install the metrics-instrumented,
// circuit-broken transport; a nil client is ignored so callers need not
// branch.
func (s *TransactionService) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.client = client
	}
}

// SetBalanceApplier wires the vault balance applier used to credit/debit a
// vault once a deposit/withdrawal is confirmed on-chain. Optional: when unset,
// status is still reconciled but no balance is moved (used by tests that only
// exercise status transitions).
func (s *TransactionService) SetBalanceApplier(applier VaultBalanceApplier) {
	s.balance = applier
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

	updated, _, err := s.ReconcileTransaction(ctx, model)
	if err != nil {
		return transaction.Transaction{}, err
	}
	return updated, nil
}

// ListPendingOlderThan returns transactions still pending whose age exceeds
// minAge. The background poller calls this each tick; minAge keeps freshly
// submitted transactions (which Horizon hasn't ingested yet) out of the batch.
func (s *TransactionService) ListPendingOlderThan(ctx context.Context, minAge time.Duration) ([]transaction.Transaction, error) {
	cutoff := time.Now().UTC().Add(-minAge)
	return s.repository.ListPendingOlderThan(ctx, cutoff)
}

// ReconcileTransaction checks the on-chain status of a single transaction
// against Horizon and persists a terminal status (completed/failed) if one has
// been reached. It returns the latest transaction view and whether the status
// actually changed. Transactions already in a terminal state, and those still
// pending on-chain, are returned unchanged with changed=false. This is the
// single source of truth for status reconciliation, shared by GetTransaction
// (on-demand) and the background poller.
func (s *TransactionService) ReconcileTransaction(ctx context.Context, model transaction.Transaction) (transaction.Transaction, bool, error) {
	switch model.Status {
	case transaction.StatusCompleted, transaction.StatusFailed:
		return model, false, nil
	}

	horizonStatus, confirmedAt, errorReason, err := s.lookupHorizonTransaction(ctx, model.TxHash)
	if err != nil {
		if errors.Is(err, errTransactionPending) {
			return model, false, nil
		}
		return model, false, err
	}

	switch horizonStatus {
	case transaction.StatusCompleted:
		// A successful transaction proves only that something succeeded. Before
		// crediting anything, confirm the transaction actually moved the
		// claimed asset, in the claimed amount, to this vault's contract
		// address, and replace the client-supplied amount with the on-chain one
		// (nester#1145).
		verified, reason, verifyErr := s.verifyConfirmedClaim(ctx, model)
		if verifyErr != nil {
			if errors.Is(verifyErr, transaction.ErrChainClaimMismatch) {
				// The claim is false: mark the transaction failed with the
				// typed reason and never touch the balance.
				updated, updateErr := s.repository.UpdateStatus(ctx, model.TxHash, transaction.StatusFailed, confirmedAt, reason)
				if updateErr != nil {
					return model, false, updateErr
				}
				return updated, true, nil
			}
			// Transient (Horizon unavailable, vault not loadable): leave the
			// transaction pending so the next poll retries rather than
			// crediting on an unverified claim.
			return model, false, verifyErr
		}
		model.Amount = verified.Amount

		// Credit/debit the vault BEFORE marking the transaction completed.
		// If the balance change fails, we return the error and leave the
		// transaction pending so the next poll retries; the applier is
		// idempotent, so a retry after a partial failure cannot double-apply.
		if err := s.applyConfirmedBalance(ctx, model); err != nil {
			return model, false, err
		}
		updated, updateErr := s.repository.UpdateStatus(ctx, model.TxHash, horizonStatus, confirmedAt, errorReason)
		if updateErr != nil {
			return model, false, updateErr
		}
		return updated, true, nil
	case transaction.StatusFailed:
		// Failed/reverted on-chain: record the failure reason and never touch
		// the balance.
		updated, updateErr := s.repository.UpdateStatus(ctx, model.TxHash, horizonStatus, confirmedAt, errorReason)
		if updateErr != nil {
			return model, false, updateErr
		}
		return updated, true, nil
	default:
		return model, false, nil
	}
}

// applyConfirmedBalance moves the vault balance for a confirmed deposit or
// withdrawal. It is a no-op for other transaction types and
// when no applier is configured.
func (s *TransactionService) applyConfirmedBalance(ctx context.Context, model transaction.Transaction) error {
	if s.balance == nil {
		return nil
	}
	if model.Type != transaction.TypeDeposit && model.Type != transaction.TypeWithdrawal {
		return nil
	}

	// transaction.Transaction carries no user id of its own (it is keyed by
	// vault + tx hash), so it is resolved from the vault here — the same
	// lookup verifyConfirmedClaim already uses for deposits. Needed so the
	// balance-audit entry records who the change belongs to, and so a
	// deposit's cap check has a user to evaluate. When no lookup is wired
	// (tests exercising only status transitions), userID stays the zero
	// value, matching this path's pre-existing best-effort behaviour.
	var userID uuid.UUID
	if s.vaults != nil {
		if v, err := s.vaults.GetVault(ctx, model.VaultID); err == nil {
			userID = v.UserID
		}
	}

	switch model.Type {
	case transaction.TypeDeposit:
		var capCheck func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error
		if s.capsChecker != nil {
			capCheck = func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error {
				return s.capsChecker.EvaluateTotals(ctx, userID, model.Amount, currentUserTotal, currentGlobalTotal)
			}
		}
		capWarning, err := s.balance.ApplyConfirmedDeposit(ctx, model.VaultID, userID, model.Amount, model.TxHash, capCheck)
		if capWarning != nil {
			// Non-fatal: the deposit already happened on-chain and was
			// credited regardless (see VaultBalanceApplier's doc comment).
			// Logged loudly so it can be alerted on — this is the only
			// enforcement point left once money has already moved on-chain.
			logpkg.FromContext(ctx).Error("confirmed deposit exceeded launch cap",
				"vault_id", model.VaultID, "user_id", userID, "amount", model.Amount.String(),
				"tx_hash", model.TxHash, "error", capWarning)
		}
		return err
	default: // transaction.TypeWithdrawal
		return s.balance.ApplyConfirmedWithdrawal(ctx, model.VaultID, userID, model.Amount, model.TxHash)
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

const (
	defaultTransactionListLimit = 20
	maxTransactionListLimit     = 100
)

// ListUserTransactionsInput carries validated query params for the list endpoint.
type ListUserTransactionsInput struct {
	UserID  uuid.UUID
	VaultID uuid.UUID // optional
	Type    string    // "deposit" | "withdrawal" | ""
	Status  string    // "pending" | "confirmed" | "failed" | "" — "confirmed" maps to "completed"
	Limit   int
	Offset  int
}

// ListUserTransactions returns paginated transactions for the authenticated user.
// The "confirmed" status value accepted from callers is mapped to the internal
// "completed" value before querying.
func (s *TransactionService) ListUserTransactions(ctx context.Context, input ListUserTransactionsInput) ([]transaction.Transaction, int, error) {
	if input.UserID == uuid.Nil {
		return nil, 0, transaction.ErrInvalidTransaction
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultTransactionListLimit
	}
	if limit > maxTransactionListLimit {
		limit = maxTransactionListLimit
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	// Map external "confirmed" status to internal "completed".
	dbStatus := input.Status
	if dbStatus == "confirmed" {
		dbStatus = string(transaction.StatusCompleted)
	}

	filter := transaction.ListFilter{
		UserID:  input.UserID,
		VaultID: input.VaultID,
		Type:    input.Type,
		Status:  dbStatus,
		Limit:   limit,
		Offset:  offset,
	}

	return s.repository.ListUserTransactions(ctx, filter)
}

func isSupportedTransactionType(value transaction.TransactionType) bool {
	switch value {
	case transaction.TypeDeposit, transaction.TypeWithdrawal:
		return true
	default:
		return false
	}
}
