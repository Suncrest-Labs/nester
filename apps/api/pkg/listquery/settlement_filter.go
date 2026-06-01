package listquery

import (
	"net/http"
	"strings"
	"time"
)

var settlementSortFields = map[string]bool{
	"created_at":   true,
	"completed_at": true,
	"amount":       true,
	"status":       true,
}

// SettlementListParams combines pagination, sort, and settlement filters.
type SettlementListParams struct {
	Page                PageParams
	Sort                SortParams
	Status              string
	DateFrom            *time.Time
	DateTo              *time.Time
	MinAmount           *string
	DestinationProvider string
	FiatCurrency        string
}

// ParseSettlementList reads list query parameters for GET /api/v1/settlements.
func ParseSettlementList(r *http.Request) (SettlementListParams, error) {
	page, err := ParsePage(r)
	if err != nil {
		return SettlementListParams{}, err
	}
	sort, err := ParseSort(r, "created_at", settlementSortFields)
	if err != nil {
		return SettlementListParams{}, err
	}

	dateFrom, err := ParseTimeQueryStart(r, "date_from")
	if err != nil {
		return SettlementListParams{}, err
	}
	dateTo, err := ParseTimeQuery(r, "date_to")
	if err != nil {
		return SettlementListParams{}, err
	}
	minAmount, err := ParseDecimalQuery(r, "min_amount")
	if err != nil {
		return SettlementListParams{}, err
	}

	return SettlementListParams{
		Page:                page,
		Sort:                sort,
		Status:              strings.TrimSpace(r.URL.Query().Get("status")),
		DateFrom:            dateFrom,
		DateTo:              dateTo,
		MinAmount:           minAmount,
		DestinationProvider: strings.TrimSpace(r.URL.Query().Get("destination_provider")),
		FiatCurrency:        strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("fiat_currency"))),
	}, nil
}
