package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// TransactionVaultLookup resolves the vault a transaction belongs to, so a
// confirmation can be checked against the vault's real contract address and
// currency rather than against values the client supplied.
//
// It is the same narrow seam the transaction handler already uses; production
// passes the Postgres vault repository.
type TransactionVaultLookup interface {
	GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error)
}

// SetVaultLookup wires the vault lookup used to verify that a confirmed
// transaction actually moved the claimed asset, in the claimed amount, to the
// vault's contract address (nester#1145).
//
// When unset, claim verification is skipped and a successful transaction is
// confirmed on its success flag alone — the pre-#1145 behaviour, retained only
// so existing tests that exercise status transitions need no vault fixture.
// Production always wires it.
func (s *TransactionService) SetVaultLookup(lookup TransactionVaultLookup) {
	s.vaults = lookup
}

// horizonOperation is one operation of a Horizon transaction. Only the fields
// needed to prove a value transfer are decoded.
//
// Horizon renders the native asset as {"asset_type":"native"} with no code or
// issuer, and every other asset as
// {"asset_type":"credit_alphanum4|12","asset_code":"USDC","asset_issuer":"G..."}.
type horizonOperation struct {
	Type        string `json:"type"`
	AssetType   string `json:"asset_type"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
	Amount      string `json:"amount"`
	From        string `json:"from"`
	To          string `json:"to"`
	// Account/Into carry the destination for account-merge and
	// create-account style operations, which do not populate To.
	Account   string `json:"account"`
	Into      string `json:"into"`
	AssetName string `json:"asset_name"`
}

// destination is the account the operation credited, normalising across the
// operation shapes Horizon uses.
func (o horizonOperation) destination() string {
	for _, candidate := range []string{o.To, o.Into, o.Account} {
		if c := strings.TrimSpace(candidate); c != "" {
			return c
		}
	}
	return ""
}

// assetCode is the asset the operation moved, in the same vocabulary as
// vault.Currency. Horizon's native asset is XLM.
func (o horizonOperation) assetCode() string {
	if strings.EqualFold(strings.TrimSpace(o.AssetType), "native") {
		return "XLM"
	}
	return strings.ToUpper(strings.TrimSpace(o.AssetCode))
}

// isValueTransfer reports whether the operation type can move asset value.
// Horizon exposes many operation types (set_options, manage_data, ...) that
// never credit a balance; treating those as a transfer would let a hash with
// an incidental matching amount pass.
func (o horizonOperation) isValueTransfer() bool {
	switch strings.ToLower(strings.TrimSpace(o.Type)) {
	case "payment", "create_account", "account_merge",
		"path_payment_strict_receive", "path_payment_strict_send",
		"invoke_host_function":
		return true
	default:
		return false
	}
}

type horizonOperationsResponse struct {
	Embedded struct {
		Records []horizonOperation `json:"records"`
	} `json:"_embedded"`
}

// chainClaim is the assertion a pending transaction makes about the chain:
// "this hash moved Amount of Asset to Destination".
type chainClaim struct {
	Destination string
	Asset       string
	Amount      decimal.Decimal
}

// verifiedChainAmount is the amount actually moved on-chain, as read from the
// matching operation. It is what gets credited — never the request body.
type verifiedChainAmount struct {
	Amount decimal.Decimal
}

// verifyChainClaim confirms that hash actually moved the claimed asset, in the
// claimed amount, to the claimed destination, and returns the on-chain amount.
//
// A successful transaction proves only that *something* succeeded. Without this
// check any real successful hash could be posted with an arbitrary amount and
// the poller would credit it in full (nester#1145).
func (s *TransactionService) verifyChainClaim(ctx context.Context, hash string, claim chainClaim) (verifiedChainAmount, string, error) {
	ops, err := s.lookupHorizonOperations(ctx, hash)
	if err != nil {
		return verifiedChainAmount{}, "", err
	}

	// Walk the operations narrowing by destination, then asset, then amount,
	// so the recorded reason names the first thing that actually diverged
	// rather than a generic "no match".
	destinationMatched := false
	assetMatched := false

	for _, op := range ops {
		if !op.isValueTransfer() {
			continue
		}
		if !strings.EqualFold(op.destination(), claim.Destination) {
			continue
		}
		destinationMatched = true

		if claim.Asset != "" && !strings.EqualFold(op.assetCode(), claim.Asset) {
			continue
		}
		assetMatched = true

		onChain, err := decimal.NewFromString(strings.TrimSpace(op.Amount))
		if err != nil || onChain.Cmp(decimal.Zero) <= 0 {
			continue
		}
		if !onChain.Equal(claim.Amount) {
			continue
		}
		return verifiedChainAmount{Amount: onChain}, "", nil
	}

	switch {
	case !destinationMatched:
		// Nothing in the transaction paid the vault at all. This is the
		// unrelated-but-successful hash.
		if len(ops) == 0 {
			return verifiedChainAmount{}, transaction.ReasonNoMatchingOperation, transaction.ErrChainClaimMismatch
		}
		return verifiedChainAmount{}, transaction.ReasonDestinationMismatch, transaction.ErrChainClaimMismatch
	case !assetMatched:
		return verifiedChainAmount{}, transaction.ReasonAssetMismatch, transaction.ErrChainClaimMismatch
	default:
		return verifiedChainAmount{}, transaction.ReasonAmountMismatch, transaction.ErrChainClaimMismatch
	}
}

// lookupHorizonOperations fetches the operations of a transaction. Horizon
// paginates; the limit is well above the protocol maximum of 100 operations
// per transaction, so a single page always holds them all.
func (s *TransactionService) lookupHorizonOperations(ctx context.Context, hash string) ([]horizonOperation, error) {
	url := fmt.Sprintf("%s/transactions/%s/operations?limit=200", s.horizonURL, hash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errTransactionPending
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("horizon operations lookup failed: %s", resp.Status)
	}

	var payload horizonOperationsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Embedded.Records, nil
}

// verifyConfirmedClaim checks a Horizon-confirmed transaction against what the
// client claimed, and returns the amount actually moved on-chain.
//
// Only deposits are value claims against the vault's contract address: a
// withdrawal moves value *out* of the vault to the user's own account, so it
// is not checked here and keeps its recorded amount.
func (s *TransactionService) verifyConfirmedClaim(ctx context.Context, model transaction.Transaction) (verifiedChainAmount, string, error) {
	if s.vaults == nil || s.balance == nil {
		// Claim verification is not wired (see SetVaultLookup) or no balance
		// moves at all; keep the recorded amount.
		return verifiedChainAmount{Amount: model.Amount}, "", nil
	}
	if model.Type != transaction.TypeDeposit {
		return verifiedChainAmount{Amount: model.Amount}, "", nil
	}

	v, err := s.vaults.GetVault(ctx, model.VaultID)
	if err != nil {
		// Cannot establish the expected destination. Treat as transient: the
		// caller leaves the transaction pending rather than crediting it.
		return verifiedChainAmount{}, transaction.ReasonVaultUnresolvable, fmt.Errorf("resolve vault for chain claim: %w", err)
	}

	destination := strings.TrimSpace(v.ContractAddress)
	if destination == "" {
		return verifiedChainAmount{}, transaction.ReasonVaultUnresolvable, fmt.Errorf("%w: vault %s has no contract address", vault.ErrUnverifiedChainTx, model.VaultID)
	}

	return s.verifyChainClaim(ctx, model.TxHash, chainClaim{
		Destination: destination,
		Asset:       strings.ToUpper(strings.TrimSpace(v.Currency)),
		Amount:      model.Amount,
	})
}
