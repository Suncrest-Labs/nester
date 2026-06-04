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

// SavingsPlanRequest represents the user goal input.
type SavingsPlanRequest struct {
	GoalUSDC                   float64 `json:"goal_usdc"`
	TimeHorizonMonths          int     `json:"time_horizon_months"`
	MaxMonthlyContributionUSDC float64 `json:"max_monthly_contribution_usdc"`
	VaultID                    string  `json:"vault_id,omitempty"`
}

// ScheduleEntry represents one month in the savings plan.
type ScheduleEntry struct {
	Month           int     `json:"month"`
	Deposit         float64 `json:"deposit"`
	ExpectedBalance float64 `json:"expected_balance"`
	YieldEarned     float64 `json:"yield_earned"`
}

// MilestoneProjection represents a checkpoint in the plan.
type MilestoneProjection struct {
	Month           int     `json:"month"`
	ExpectedBalance float64 `json:"expected_balance"`
}

// SavingsPlanResponse represents the generated savings schedule.
type SavingsPlanResponse struct {
	Achievable             bool                  `json:"achievable"`
	RequiredMonthlyDeposit float64               `json:"required_monthly_deposit"`
	MonthlySchedule        []ScheduleEntry       `json:"monthly_schedule"`
	TotalYieldEarned       float64               `json:"total_yield_earned"`
	Narrative              string                `json:"narrative"`
	Milestones             []MilestoneProjection `json:"milestones"`
}
