// Package openapi generates an OpenAPI 3.1 specification for the Nester API
// from the real Go request/response types, rather than a hand-maintained
// document that drifts from the implementation (issue #1058).
//
// Scope of this first slice: the vault lifecycle routes (create, get,
// deposit, withdraw) — the ones with the cleanest, most representative
// request/response shapes, including genuine optional-vs-nullable fields
// (vault.Vault.SoftCapacity, LastHarvestedAt, etc.) that a hand-written spec
// would be prone to getting wrong. Full coverage of all 29 handlers, a
// generated TypeScript client, a breaking-change detector, and contract
// tests for every route are follow-up work — see the PR description for
// what's deferred and why.
//
// Error response bodies are documented using the response.ErrorBody shape
// that already exists and is genuinely used repo-wide today
// (pkg/response/response.go). This intentionally does not invent a new
// error-code taxonomy: issue #1058 itself says the spec should capture "the
// final error shape" from #1048's taxonomy work, which is still open. Per
// route, the actual HTTP status codes the handler can return are listed
// (extracted by reading each handler's writeDomainError switch), so the
// spec is accurate about *when* errors happen even before the taxonomy work
// standardizes their *codes*.
package openapi

import (
	"net/http"
	"reflect"

	"github.com/google/uuid"
	jsonschema "github.com/swaggest/jsonschema-go"
	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi31"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// errorResponse mirrors response.Response for a failed request: Success is
// always false and Error is always populated. Modeled as its own type
// (rather than reusing response.Response as-is) so the generated schema
// documents error and success shapes as visibly distinct cases, matching
// how every handler in this repo actually branches.
type errorResponse struct {
	Success bool                `json:"success" example:"false"`
	Error   *response.ErrorBody `json:"error"`
}

// vaultResponse is response.Response with Data narrowed to vault.Vault, so
// the generated schema states the real shape instead of `data: {}`
// (interface{} reflects to an empty object with swaggest/jsonschema-go).
type vaultResponse struct {
	Success bool        `json:"success" example:"true"`
	Data    vault.Vault `json:"data"`
}

// createVaultRequest mirrors handler.createVaultRequest (unexported in that
// package). Kept as a separate copy here rather than exporting the
// handler's own type: the spec should describe the wire contract, and
// duplicating a small DTO is cheaper than widening the handler package's
// public surface for a generator to import.
type createVaultRequest struct {
	ContractAddress string `json:"contract_address" required:"true" description:"56-character Soroban contract address starting with 'C'"`
	Currency        string `json:"currency" required:"true"`
	Status          string `json:"status,omitempty"`
}

type vaultIDPathParam struct {
	ID uuid.UUID `path:"id"`
}

type depositRequest struct {
	VaultID uuid.UUID `path:"id"`
	Amount  string    `json:"amount" required:"true" description:"Decimal string amount, must be > 0"`
	Asset   string    `json:"asset" required:"true"`
	TxHash  string    `json:"tx_hash,omitempty" description:"On-chain transaction hash; required when deposit verification is configured"`
}

type withdrawRequest struct {
	VaultID uuid.UUID `path:"id"`
	Amount  string    `json:"amount" required:"true"`
	Asset   string    `json:"asset" required:"true"`
	TxHash  string    `json:"tx_hash,omitempty"`
}

// NewReflector builds the reflector with every vault-lifecycle operation
// registered. Returns an error rather than panicking so the CLI entrypoint
// (cmd/openapi-gen) can report a clean failure instead of a stack trace.
func NewReflector() (*openapi31.Reflector, error) {
	r := openapi31.NewReflector()
	r.Spec.Info.
		WithTitle("Nester API").
		WithVersion("1.0.0").
		WithDescription("Generated from Go source (issue #1058). Currently covers the vault lifecycle routes only — see internal/openapi/spec.go for scope.")
	r.DefaultOptions = append(r.DefaultOptions, fixPointerNullability())

	if err := addCreateVault(r); err != nil {
		return nil, err
	}
	if err := addGetVault(r); err != nil {
		return nil, err
	}
	if err := addDepositToVault(r); err != nil {
		return nil, err
	}
	if err := addWithdrawFromVault(r); err != nil {
		return nil, err
	}

	return r, nil
}

func addCreateVault(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodPost, "/api/v1/vaults")
	if err != nil {
		return err
	}
	op.SetSummary("Create a vault")
	op.SetDescription("Registers a new vault for the authenticated user against a Soroban contract address.")
	op.SetTags("Vaults")
	op.AddReqStructure(new(createVaultRequest))
	op.AddRespStructure(new(vaultResponse), openapi.WithHTTPStatus(http.StatusCreated))
	// Status codes taken directly from vault_handler.go's createVault and its
	// call to writeDomainError, not invented: 400 (bad JSON, invalid
	// currency, malformed contract address, or a domain validation error),
	// 401 (missing/invalid auth), 409 (contract address already registered
	// to another vault, nester#1148).
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusUnauthorized))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusConflict))
	return r.AddOperation(op)
}

func addGetVault(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodGet, "/api/v1/vaults/{id}")
	if err != nil {
		return err
	}
	op.SetSummary("Get a vault")
	op.SetDescription("Returns a vault owned by the authenticated caller. Returns 404, not 403, for a vault owned by someone else — see vault_handler.go's getVault for why: a 403 here would let a caller enumerate other users' vault IDs.")
	op.SetTags("Vaults")
	op.AddReqStructure(new(vaultIDPathParam))
	op.AddRespStructure(new(vaultResponse), openapi.WithHTTPStatus(http.StatusOK))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusUnauthorized))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusNotFound))
	return r.AddOperation(op)
}

func addDepositToVault(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodPost, "/api/v1/vaults/{id}/deposit")
	if err != nil {
		return err
	}
	op.SetSummary("Deposit into a vault")
	op.SetDescription("Records a deposit against the vault. When deposit verification is configured, tx_hash is required and the credited amount is read from the on-chain event rather than the request body (nester#1075).")
	op.SetTags("Vaults")
	op.AddReqStructure(new(depositRequest))
	op.AddRespStructure(new(vaultResponse), openapi.WithHTTPStatus(http.StatusCreated))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusUnauthorized))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusNotFound))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusConflict))
	// A well-formed, valid-amount deposit the server still refuses to fund
	// (nester#1152) — the server is not confused about the request, it is
	// declining it.
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusForbidden))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusServiceUnavailable))
	return r.AddOperation(op)
}

func addWithdrawFromVault(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodPost, "/api/v1/vaults/{id}/withdraw")
	if err != nil {
		return err
	}
	op.SetSummary("Withdraw from a vault")
	op.SetTags("Vaults")
	op.AddReqStructure(new(withdrawRequest))
	op.AddRespStructure(new(vaultResponse), openapi.WithHTTPStatus(http.StatusCreated))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusUnauthorized))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusNotFound))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusConflict))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusServiceUnavailable))
	return r.AddOperation(op)
}

// fixPointerNullability works around a real bug in swaggest/jsonschema-go
// (v0.3.78) confirmed by isolated reproduction: when a struct has both a
// plain field of some type T and a *T field of the same underlying type
// (e.g. vault.Vault's TotalDeposited decimal.Decimal alongside SoftCapacity
// *decimal.Decimal), the reflector's per-type schema cache is keyed on T
// regardless of pointer-ness. The first field reflected "wins" the cached
// schema, so if a non-pointer field is walked first, the pointer field
// silently loses its nullable marker in the generated spec — exactly the
// "conflating omitempty with optional/nullable" failure mode issue #1058
// itself warns about, just from a different root cause (a caching bug, not
// this code's own annotations).
//
// The fix hooks jsonschema.InterceptProp's second ("Processed") invocation,
// which receives the real reflect.StructField (so pointer-ness is checked
// directly, bypassing the buggy cache lookup) and the specific per-property
// Schema about to be attached to the parent object. That Schema value is a
// fresh copy per field, but its Type field is a pointer shared with the
// cache entry — so before adding "null", the Type is cloned to avoid
// mutating (and corrupting) the cached schema for every OTHER field that
// happens to share the same underlying type.
func fixPointerNullability() func(*jsonschema.ReflectContext) {
	return jsonschema.InterceptProp(func(params jsonschema.InterceptPropParams) error {
		if !params.Processed || params.Field.Type.Kind() != reflect.Ptr {
			return nil
		}
		s := params.PropertySchema
		if s.Type != nil {
			cloned := *s.Type
			s.Type = &cloned
		}
		s.AddType(jsonschema.Null)
		return nil
	})
}
