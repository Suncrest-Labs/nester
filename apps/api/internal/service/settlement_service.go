package service

import (
    "context"
    "fmt"
    "strings"
    "time"
    "encoding/json"
    "bytes"
    "net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
)

type SettlementService struct {
    repository offramp.Repository
}

func NewSettlementService(repository offramp.Repository) *SettlementService {
    return &SettlementService{repository: repository}
}

// InitiateSettlementInput carries caller-supplied data for a new settlement.
type InitiateSettlementInput struct {
	UserID       uuid.UUID
	VaultID      uuid.UUID
	Amount       decimal.Decimal
	Currency     string
	FiatCurrency string
	FiatAmount   decimal.Decimal
	ExchangeRate decimal.Decimal
	Destination  offramp.Destination
}

// UpdateStatusInput carries the target state for a status transition.
type UpdateStatusInput struct {
	SettlementID uuid.UUID
	NewStatus    offramp.SettlementStatus
}

// InitiateSettlement validates input, creates a settlement in the `initiated`
// state, and persists it via the repository.
func (s *SettlementService) InitiateSettlement(ctx context.Context, input InitiateSettlementInput) (offramp.Settlement, error) {
    // --- Pre-flight risk check ---
    riskPayload := map[string]interface{}{
        "transaction": map[string]interface{}{
            "amount":     input.Amount.String(),
            "currency":   input.Currency,
            "to_account": input.Destination.AccountNumber,
        },
        "user_history": map[string]interface{}{}, // TODO: Populate with real user history
        "context":      map[string]interface{}{},
    }
    body, _ := json.Marshal(riskPayload)
    riskURL := "http://localhost:8000/risk/evaluate" // TODO: Make configurable
    req, err := http.NewRequestWithContext(ctx, "POST", riskURL, bytes.NewBuffer(body))
    if err == nil {
        req.Header.Set("Content-Type", "application/json")
        resp, err := http.DefaultClient.Do(req)
        if err == nil && resp.StatusCode == 200 {
            var riskResp struct {
                Score             float64 `json:"score"`
                RecommendedAction string  `json:"recommended_action"`
            }
            json.NewDecoder(resp.Body).Decode(&riskResp)
            resp.Body.Close()
            if riskResp.RecommendedAction == "block" {
                return offramp.Settlement{}, fmt.Errorf("transaction blocked by risk engine (score %.1f)", riskResp.Score)
            }
            if riskResp.RecommendedAction == "hold" {
                // Optionally, set a status or flag for review
            }
        }
    }
    // --- End risk check ---
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

func (s *SettlementService) evaluateRisk(ctx context.Context, model offramp.Settlement) (offramp.SettlementStatus, error) {
	history, err := s.repository.GetByUserID(ctx, model.UserID, "")
	if err != nil {
		return "", err
	}

	avg := decimal.Zero
	count := 0
	velocity24h := 0
	isNovelAccount := true
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)

	for _, prev := range history {
		if prev.Status == offramp.StatusConfirmed {
			avg = avg.Add(prev.Amount)
			count++
		}
		if prev.CreatedAt.After(yesterday) {
			velocity24h++
		}
		if prev.Destination.AccountNumber == model.Destination.AccountNumber {
			isNovelAccount = false
		}
	}

	historyAvg := float64(0)
	if count > 0 {
		historyAvg, _ = avg.Div(decimal.NewFromInt(int64(count))).Float64()
	}

	amount, _ := model.Amount.Float64()

	riskResp, err := s.riskClient.Evaluate(ctx, RiskEvaluationRequest{
		UserID:             model.UserID.String(),
		Amount:             amount,
		HistoryAvg:         historyAvg,
		Velocity24h:        velocity24h,
		IsNovelAccount:     isNovelAccount,
		DestinationAccount: model.Destination.AccountNumber,
	})
	if err != nil {
		return "", err
	}

	switch riskResp.RecommendedAction {
	case "block":
		return offramp.StatusBlocked, nil
	case "hold":
		return offramp.StatusHeld, nil
	case "flag":
		// Flagged transactions still proceed but are marked for review
		// Here we keep it as initiated or we could add a "flagged" flag
		return offramp.StatusInitiated, nil
	default:
		return offramp.StatusInitiated, nil
	}
}

// GetSettlement retrieves a single settlement by ID.
func (s *SettlementService) GetSettlement(ctx context.Context, id uuid.UUID) (offramp.Settlement, error) {
	if id == uuid.Nil {
		return offramp.Settlement{}, offramp.ErrInvalidSettlement
	}
	return s.repository.GetByID(ctx, id)
}

// GetUserSettlements returns all settlements for a user. If statusFilter is
// non-empty it is validated and passed to the repository as a WHERE clause.
func (s *SettlementService) GetUserSettlements(
	ctx context.Context,
	userID uuid.UUID,
	statusFilter string,
) ([]offramp.Settlement, error) {
	if userID == uuid.Nil {
		return nil, offramp.ErrInvalidSettlement
	}

	var parsedFilter offramp.SettlementStatus
	if statusFilter != "" {
		parsed, err := offramp.ParseStatus(statusFilter)
		if err != nil {
			return nil, err
		}
		parsedFilter = parsed
	}

	return s.repository.GetByUserID(ctx, userID, parsedFilter)
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
	if d.Type == "bank_transfer" && strings.TrimSpace(d.BankCode) == "" {
		return offramp.ErrInvalidSettlement
	}
	return nil
}
