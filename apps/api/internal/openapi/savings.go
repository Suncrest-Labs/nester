package openapi

import (
	"net/http"

	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi31"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type savingsSummaryResponse struct {
	Success bool                   `json:"success" example:"true"`
	Data    service.SavingsSummary `json:"data"`
}

type savingsHistoryResponse struct {
	Success bool                          `json:"success" example:"true"`
	Data    []service.SavingsHistoryPoint `json:"data"`
}

type savingsPositionResponse struct {
	Success bool                    `json:"success" example:"true"`
	Data    service.SavingsPosition `json:"data"`
}

type savingsHistoryQuery struct {
	Period string `query:"period" required:"true" enum:"7d,30d,90d"`
}

const savingsAmountNote = "Amounts are decimal USDC strings (the contract's 7-decimal integers divided by 10^7), matching the vault endpoints; sharePrice is USDC per share. Rates are annualized realized percentages, null until at least two snapshots 24h apart exist in the window."

func addSavingsOperations(r *openapi31.Reflector) error {
	for _, add := range []func(*openapi31.Reflector) error{addGetSavings, addGetSavingsHistory, addGetSavingsPosition} {
		if err := add(r); err != nil {
			return err
		}
	}
	return nil
}

func addGetSavings(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodGet, "/api/v1/savings")
	if err != nil {
		return err
	}
	op.SetSummary("Savings rate overview")
	op.SetDescription("Public. Live savings core state plus trailing realized rates. " + savingsAmountNote + " Answers 503 when the savings vault is not configured.")
	op.SetTags("Savings")
	op.AddRespStructure(new(savingsSummaryResponse), openapi.WithHTTPStatus(http.StatusOK))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusServiceUnavailable))
	return r.AddOperation(op)
}

func addGetSavingsHistory(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodGet, "/api/v1/savings/history")
	if err != nil {
		return err
	}
	op.SetSummary("Savings share-price history")
	op.SetDescription("Public. Hourly share-price snapshots over the period, ascending. " + savingsAmountNote)
	op.SetTags("Savings")
	op.AddReqStructure(new(savingsHistoryQuery))
	op.AddRespStructure(new(savingsHistoryResponse), openapi.WithHTTPStatus(http.StatusOK))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusServiceUnavailable))
	return r.AddOperation(op)
}

func addGetSavingsPosition(r *openapi31.Reflector) error {
	op, err := r.NewOperationContext(http.MethodGet, "/api/v1/savings/position")
	if err != nil {
		return err
	}
	op.SetSummary("Savings position of the authenticated wallet")
	op.SetDescription("Share balance from the core contract and its current asset value. deposited is always null: per-wallet deposit totals are not tracked for the savings core. " + savingsAmountNote)
	op.SetTags("Savings")
	op.AddRespStructure(new(savingsPositionResponse), openapi.WithHTTPStatus(http.StatusOK))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusBadRequest))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusUnauthorized))
	op.AddRespStructure(new(errorResponse), openapi.WithHTTPStatus(http.StatusServiceUnavailable))
	return r.AddOperation(op)
}
