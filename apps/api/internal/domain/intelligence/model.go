package intelligence

import "time"

// Recommendation represents an advisory suggestion from Prometheus.
type Recommendation struct {
	Type        string    `json:"type"` // "rebalance", "yield_alert", "risk_warning"
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	CreatedAt   time.Time `json:"created_at"`
}

// SentimentReport represents the aggregate market sentiment from AI analysis.
type SentimentReport struct {
	Score       float64   `json:"score"` // -1.0 (very bearish) to 1.0 (very bullish)
	Summary     string    `json:"summary"`
	TopFactors  []string  `json:"top_factors"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PortfolioInsights represents AI-generated analysis of a user's holdings.
type PortfolioInsights struct {
	RiskScore       float64   `json:"risk_score"`
	Diversification float64   `json:"diversification"`
	Suggestions     []string  `json:"suggestions"`
	GeneratedAt     time.Time `json:"generated_at"`
}
