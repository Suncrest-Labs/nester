// Package signing defines the transaction-signing boundary between the API
// process and the process that holds the Stellar operator key.
//
// The central design decision is that the boundary carries a *typed transaction
// intent* rather than raw bytes. A `Sign(raw []byte)` interface would make the
// signer a general signing oracle: any caller able to reach it could obtain a
// signature over anything, which reduces the security value of holding the key
// elsewhere to almost nothing. Instead the caller describes what it wants done
// — contract, function, arguments, network — and the signer rebuilds the
// transaction from that description, validates it against policy, and signs the
// transaction *it* constructed. A signature can therefore only ever cover a
// transaction shape the signer itself produced and approved.
//
// See docs/security/signing-isolation.md for the full boundary design and
// docs/security/signing-threat-model.md for what it does and does not defend.
package signing

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Operation identifies a contract function the signer is permitted to invoke.
//
// This is a closed set, mirroring the complete signing surface found in
// internal/service/soroban_vault_chain_invoker.go. Adding a new signable
// operation is deliberately a code change in the signer, reviewed as such,
// rather than configuration the API can influence at runtime.
type Operation string

const (
	OpPause                Operation = "pause"
	OpUnpause              Operation = "unpause"
	OpRebalance            Operation = "rebalance"
	OpSetWeights           Operation = "set_weights"
	OpDeposit              Operation = "deposit"
	OpWithdraw             Operation = "withdraw"
	OpHarvest              Operation = "harvest"
	OpEmergencyWithdrawAll Operation = "emergency_withdraw_all"
)

// Shape describes the argument structure an operation carries. The signer uses
// it to reject intents whose arguments do not match the operation, which is the
// check that stops a caller from smuggling unexpected arguments into a
// permitted function name.
type Shape string

const (
	// ShapeVoid takes only the operator address as caller.
	ShapeVoid Shape = "void"
	// ShapeI128Pair takes two i128 arguments (amount plus a slippage guard).
	ShapeI128Pair Shape = "i128_pair"
	// ShapeAddressBool takes a Stellar account address and a boolean.
	ShapeAddressBool Shape = "address_bool"
	// ShapeWeights takes a vector of (protocol, weight_bps) pairs.
	ShapeWeights Shape = "weights"
)

// operationShapes is the authoritative mapping from operation to argument
// shape. An intent whose Shape does not match its Operation is rejected before
// any policy limit is consulted.
var operationShapes = map[Operation]Shape{
	OpPause:                ShapeVoid,
	OpUnpause:              ShapeVoid,
	OpRebalance:            ShapeVoid,
	OpEmergencyWithdrawAll: ShapeVoid,
	OpSetWeights:           ShapeWeights,
	OpDeposit:              ShapeI128Pair,
	OpWithdraw:             ShapeI128Pair,
	OpHarvest:              ShapeAddressBool,
}

// ShapeFor returns the argument shape an operation must carry, and whether the
// operation is one the signer recognises at all.
func ShapeFor(op Operation) (Shape, bool) {
	s, ok := operationShapes[op]
	return s, ok
}

// KnownOperations returns every operation the signer can sign, sorted for
// stable output in logs and documentation.
func KnownOperations() []Operation {
	ops := make([]Operation, 0, len(operationShapes))
	for op := range operationShapes {
		ops = append(ops, op)
	}
	// Insertion order over a map is random; sort so callers and tests see a
	// stable list.
	for i := 1; i < len(ops); i++ {
		for j := i; j > 0 && ops[j] < ops[j-1]; j-- {
			ops[j], ops[j-1] = ops[j-1], ops[j]
		}
	}
	return ops
}

// WeightEntry is a single allocation weight in a set_weights intent.
type WeightEntry struct {
	Protocol  string `json:"protocol"`
	WeightBps int32  `json:"weight_bps"`
}

// Intent is a request to sign one contract invocation. It is the only thing
// that crosses the signing boundary in the request direction.
//
// Every field is data the signer validates. Notably absent: any field carrying
// pre-built transaction bytes, an XDR blob, or a hash to sign. Their absence is
// the property that prevents the signer from being used as a generic oracle.
type Intent struct {
	// ID uniquely identifies this intent. It is the replay-protection key and
	// the correlation identifier that ties the signer audit event back to the
	// API request that caused it.
	ID string `json:"id"`

	// Operation is the contract function to invoke.
	Operation Operation `json:"operation"`

	// Shape declares the argument structure. It must match Operation.
	Shape Shape `json:"shape"`

	// ContractAddress is the target contract (C...).
	ContractAddress string `json:"contract_address"`

	// NetworkPassphrase pins the network. A mismatch against the signer
	// configured network is rejected, which prevents a testnet-configured
	// caller from obtaining a mainnet signature and vice versa.
	NetworkPassphrase string `json:"network_passphrase"`

	// Arg0 and Arg1 carry the i128 pair for ShapeI128Pair operations.
	// For deposit: amount and minimum shares out. For withdraw: shares and
	// minimum assets out.
	Arg0 int64 `json:"arg0,omitempty"`
	Arg1 int64 `json:"arg1,omitempty"`

	// Address carries the Stellar account for ShapeAddressBool operations.
	Address string `json:"address,omitempty"`

	// Flag carries the boolean for ShapeAddressBool operations.
	Flag bool `json:"flag,omitempty"`

	// Weights carries the allocation vector for ShapeWeights operations.
	Weights []WeightEntry `json:"weights,omitempty"`

	// IssuedAt is when the API created this intent. Together with the policy
	// MaxIntentAge it bounds how long a captured intent stays usable.
	IssuedAt time.Time `json:"issued_at"`

	// RequestID correlates the intent with the originating API request in
	// logs and audit records. It carries no authority.
	RequestID string `json:"request_id,omitempty"`
}

// Rejection categorises why an intent was refused. The category is recorded in
// audit events and counted as a metric, so it uses a small, low-cardinality set
// of stable values rather than free-form error text.
type Rejection string

const (
	RejectUnknownOperation   Rejection = "unknown_operation"
	RejectShapeMismatch      Rejection = "shape_mismatch"
	RejectContractNotAllowed Rejection = "contract_not_allowed"
	RejectNetworkMismatch    Rejection = "network_mismatch"
	RejectInvalidAddress     Rejection = "invalid_address"
	RejectAmountOutOfPolicy  Rejection = "amount_out_of_policy"
	RejectWeightsInvalid     Rejection = "weights_invalid"
	RejectIntentExpired      Rejection = "intent_expired"
	RejectIntentReplayed     Rejection = "intent_replayed"
	RejectMalformed          Rejection = "malformed"
	RejectKillSwitchActive   Rejection = "kill_switch_active"
	RejectUnauthorized       Rejection = "unauthorized"
)

// PolicyError is returned when an intent fails validation. It carries a stable
// category for metrics and a human-readable reason for operators.
//
// The reason text is written for an on-call engineer reading an audit record.
// It never includes key material, and it never echoes back attacker-controlled
// data verbatim beyond the bounded identifiers already present in the intent.
type PolicyError struct {
	Category Rejection
	Reason   string
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("signing policy rejected intent: %s (%s)", e.Reason, e.Category)
}

// Is supports errors.Is comparison against ErrPolicyRejected.
func (e *PolicyError) Is(target error) bool {
	return target == ErrPolicyRejected
}

// ErrPolicyRejected is the sentinel every policy rejection matches, so callers
// can distinguish a refusal from an infrastructure failure without inspecting
// the category.
var ErrPolicyRejected = errors.New("signing policy rejected intent")

func reject(cat Rejection, format string, args ...any) *PolicyError {
	return &PolicyError{Category: cat, Reason: fmt.Sprintf(format, args...)}
}

// Validate performs structural checks that do not depend on deployment policy:
// that the operation is known, that the declared shape matches it, and that the
// arguments the shape requires are present and well-formed.
//
// Policy limits (allowed contracts, amount bounds, expiry) are applied
// separately by Policy.Evaluate. Splitting them keeps the structural rules
// testable without constructing a full policy, and makes it explicit that both
// layers must pass.
func (i *Intent) Validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return reject(RejectMalformed, "intent id is required")
	}
	shape, known := ShapeFor(i.Operation)
	if !known {
		return reject(RejectUnknownOperation, "operation %q is not signable", i.Operation)
	}
	if i.Shape != shape {
		return reject(RejectShapeMismatch,
			"operation %q requires shape %q but intent declared %q", i.Operation, shape, i.Shape)
	}
	if err := validateContractAddress(i.ContractAddress); err != nil {
		return err
	}
	if strings.TrimSpace(i.NetworkPassphrase) == "" {
		return reject(RejectMalformed, "network passphrase is required")
	}
	if i.IssuedAt.IsZero() {
		return reject(RejectMalformed, "issued_at is required")
	}

	switch shape {
	case ShapeVoid:
		// A void operation carries no arguments. Reject any that are set —
		// silently ignoring them would let a caller probe for parsing
		// differences between the API and the signer.
		if i.Arg0 != 0 || i.Arg1 != 0 || i.Address != "" || len(i.Weights) > 0 {
			return reject(RejectShapeMismatch,
				"operation %q takes no arguments but the intent carries some", i.Operation)
		}
	case ShapeI128Pair:
		if i.Address != "" || len(i.Weights) > 0 {
			return reject(RejectShapeMismatch,
				"operation %q takes only two integer arguments", i.Operation)
		}
		// The primary amount must be positive. A zero or negative amount is
		// never a legitimate deposit or withdrawal and is rejected here rather
		// than relying on the contract to refuse it.
		if i.Arg0 <= 0 {
			return reject(RejectAmountOutOfPolicy,
				"operation %q requires a positive amount, got %d", i.Operation, i.Arg0)
		}
		// The slippage guard may be zero (deposit passes 0) but never negative.
		if i.Arg1 < 0 {
			return reject(RejectAmountOutOfPolicy,
				"operation %q slippage guard must not be negative, got %d", i.Operation, i.Arg1)
		}
	case ShapeAddressBool:
		if i.Arg0 != 0 || i.Arg1 != 0 || len(i.Weights) > 0 {
			return reject(RejectShapeMismatch,
				"operation %q takes an address and a boolean only", i.Operation)
		}
		if err := validateAccountAddress(i.Address); err != nil {
			return err
		}
	case ShapeWeights:
		if i.Arg0 != 0 || i.Arg1 != 0 || i.Address != "" {
			return reject(RejectShapeMismatch,
				"operation %q takes a weight vector only", i.Operation)
		}
		if err := validateWeights(i.Weights); err != nil {
			return err
		}
	}
	return nil
}

// maxWeightEntries bounds the allocation vector. The strategy contract has a
// small, fixed protocol set; an oversized vector is either a bug or an attempt
// to build an expensive transaction, and neither should be signed.
const maxWeightEntries = 32

// totalWeightBps is the exact total an allocation vector must sum to (100%).
const totalWeightBps = 10_000

func validateWeights(weights []WeightEntry) error {
	if len(weights) == 0 {
		return reject(RejectWeightsInvalid, "weight vector must not be empty")
	}
	if len(weights) > maxWeightEntries {
		return reject(RejectWeightsInvalid,
			"weight vector has %d entries, limit is %d", len(weights), maxWeightEntries)
	}
	seen := make(map[string]struct{}, len(weights))
	var total int64
	for idx, w := range weights {
		name := strings.TrimSpace(w.Protocol)
		if name == "" {
			return reject(RejectWeightsInvalid, "weight entry %d has an empty protocol", idx)
		}
		if len(name) > 64 {
			return reject(RejectWeightsInvalid, "weight entry %d protocol name is too long", idx)
		}
		if _, dup := seen[name]; dup {
			// A duplicated protocol would let a caller express a total that
			// looks valid per-entry while the contract sees something else.
			return reject(RejectWeightsInvalid, "weight entry %d duplicates protocol %q", idx, name)
		}
		seen[name] = struct{}{}
		if w.WeightBps < 0 || w.WeightBps > totalWeightBps {
			return reject(RejectWeightsInvalid,
				"weight entry %d is %d bps, must be within 0..%d", idx, w.WeightBps, totalWeightBps)
		}
		total += int64(w.WeightBps)
	}
	if total != totalWeightBps {
		return reject(RejectWeightsInvalid,
			"weights sum to %d bps, must sum to exactly %d", total, totalWeightBps)
	}
	return nil
}

// validateContractAddress checks the Soroban contract address form. Strkey
// decoding is performed by the signer when it builds the transaction; this is
// the cheap structural gate that rejects obvious garbage before any work.
func validateContractAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return reject(RejectMalformed, "contract address is required")
	}
	if !strings.HasPrefix(addr, "C") {
		return reject(RejectInvalidAddress, "contract address must be a C-prefixed strkey")
	}
	if len(addr) != strkeyLen {
		return reject(RejectInvalidAddress, "contract address has an invalid length")
	}
	return nil
}

// validateAccountAddress checks the Stellar account address form.
func validateAccountAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return reject(RejectMalformed, "account address is required")
	}
	if !strings.HasPrefix(addr, "G") {
		return reject(RejectInvalidAddress, "account address must be a G-prefixed strkey")
	}
	if len(addr) != strkeyLen {
		return reject(RejectInvalidAddress, "account address has an invalid length")
	}
	return nil
}

// strkeyLen is the encoded length of both C- and G-prefixed Stellar strkeys.
const strkeyLen = 56
