package usersignal

type EngagementTier string

const (
	TierHighlyEngaged EngagementTier = "highly_engaged"
	TierEngaged       EngagementTier = "engaged"
	TierAtRisk        EngagementTier = "at_risk"
	TierDormant       EngagementTier = "dormant"
)

type EngagementScore struct {
	Score float64 // 0.0 to 1.0
}
