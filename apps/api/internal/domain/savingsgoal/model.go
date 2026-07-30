package savingsgoal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrGoalNotFound = errors.New("savings goal not found")
	ErrInvalidGoal  = errors.New("invalid savings goal")
	ErrUnauthorized = errors.New("unauthorized")
	// ErrGoalCompleted is returned when an operation is not allowed on a goal
	// that has already reached its target (e.g. changing its deadline).
	ErrGoalCompleted = errors.New("savings goal already completed")
	// ErrGoalPaused is returned when an action requires the goal to be active.
	ErrGoalPaused = errors.New("savings goal is paused")
	// ErrGoalArchived is returned when an operation is not allowed on an archived goal
	// (e.g. adding a new contribution schedule).
	ErrGoalArchived = errors.New("savings goal is archived")
	// ErrInvalidAmount is returned when a goal's target amount is zero, negative,
	// or below MinTargetAmount (#692). Defined here so handlers don't have to
	// import the vault domain to classify amount validation failures.
	ErrInvalidAmount = errors.New("invalid target amount")
	// ErrInvalidContributionLimits is returned when a goal's optional
	// min/max per-contribution limits are invalid, e.g. negative, zero, or
	// min greater than max (#922).
	ErrInvalidContributionLimits = errors.New("invalid contribution limits")
	// ErrContributionOutOfRange is returned when a deposit amount falls
	// outside a goal's configured min/max per-contribution limits (#922).
	ErrContributionOutOfRange = errors.New("contribution amount outside allowed range")
	// ErrGoalNotDeleted is returned by Restore when the goal has no deleted_at
	// set (#924): nothing to restore.
	ErrGoalNotDeleted = errors.New("savings goal is not deleted")
	// ErrRecoveryWindowExpired is returned by Restore when the goal was
	// deleted more than SavingsGoalRecoveryWindow ago (#924): it may already
	// have been (or is about to be) permanently purged.
	ErrRecoveryWindowExpired = errors.New("savings goal recovery window has expired")
)

// SavingsGoalRecoveryWindow is how long a soft-deleted savings goal (#924)
// remains restorable via POST /savings-goals/{id}/restore before the
// scheduled purge job (see internal/scheduler) hard-deletes it permanently.
const SavingsGoalRecoveryWindow = 30 * 24 * time.Hour

// MinTargetAmount is the smallest meaningful goal target (#692). Values above
// zero but below this (e.g. 0.000000001) are rejected as no-op goals.
var MinTargetAmount = decimal.RequireFromString("0.01")

const (
	GoalStatusActive    = "active"
	GoalStatusPaused    = "paused"
	GoalStatusCompleted = "completed"
	// GoalStatusArchived is a terminal state a user can move a goal into (e.g.
	// after it completes) to hide it without deleting it (#684).
	GoalStatusArchived = "archived"
)

// On-chain goal status values, mirroring the savings_goal contract's
// GoalStatus enum (#807). Stored separately from Status: on-chain
// registration happens asynchronously, so a goal can be backend-Active
// while its on-chain twin is still nil (not yet registered), and the two
// only need to agree once registration lands.
const (
	OnchainStatusActive    = "active"
	OnchainStatusCompleted = "completed"
	OnchainStatusAbandoned = "abandoned"
	OnchainStatusExpired   = "expired"
)

// DeriveOnchainGoalID hashes a goal's UUID into the 32-byte identifier the
// savings_goal contract's create_goal expects as goal_id, hex-encoded for
// storage. The contract never generates goal IDs itself (#807): the backend
// remains the source of truth for the ID, and this hash is the only stable,
// deterministic way to turn a UUID into a BytesN<32> without a chain
// round-trip.
func DeriveOnchainGoalID(id uuid.UUID) string {
	sum := sha256.Sum256(id[:])
	return hex.EncodeToString(sum[:])
}

// ParseStatusFilter validates a status used to filter the goal list. An empty
// value means "no filter" and returns ("", nil).
func ParseStatusFilter(value string) (string, error) {
	switch status := strings.ToLower(strings.TrimSpace(value)); status {
	case "":
		return "", nil
	case GoalStatusActive, GoalStatusPaused, GoalStatusCompleted, GoalStatusArchived:
		return status, nil
	default:
		return "", fmt.Errorf("%w: invalid status", ErrInvalidGoal)
	}
}

type GoalCategory string

const (
	CategoryEmergencyFund GoalCategory = "emergency_fund"
	CategoryEducation     GoalCategory = "education"
	CategoryHousing       GoalCategory = "housing"
	CategoryTravel        GoalCategory = "travel"
	CategoryBusiness      GoalCategory = "business"
	CategoryHealth        GoalCategory = "health"
	CategoryRetirement    GoalCategory = "retirement"
	CategoryOther         GoalCategory = "other"
)

const (
	CurrencyUSDC = "USDC"
	CurrencyXLM  = "XLM"
)

// SupportedCurrencies lists goal denominations accepted by the savings API.
var SupportedCurrencies = map[string]bool{
	CurrencyUSDC: true,
	CurrencyXLM:  true,
}

// IsSupportedCurrency reports whether currency is a supported savings goal denomination.
func IsSupportedCurrency(currency string) bool {
	return SupportedCurrencies[NormalizeCurrency(currency)]
}

// NormalizeCurrency uppercases and trims a currency code for storage and comparison.
func NormalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// SavingsGoalsSummary aggregates goal totals per currency without cross-currency conversion.
type SavingsGoalsSummary struct {
	TotalSavedUSDC  decimal.Decimal `json:"total_saved_usdc"`
	TotalTargetUSDC decimal.Decimal `json:"total_target_usdc"`
	TotalSavedXLM   decimal.Decimal `json:"total_saved_xlm"`
	TotalTargetXLM  decimal.Decimal `json:"total_target_xlm"`
	GoalCount       int             `json:"goal_count"`
	// ActiveGoalCount counts goals in "active" status only (excludes archived, paused, completed).
	ActiveGoalCount    int `json:"active_goal_count"`
	CompletedGoalCount int `json:"completed_goal_count"`
	// OverallProgressPct is USDC progress (saved/target, capped at 100). USDC
	// only, to avoid cross-currency conversion the rest of this type avoids.
	OverallProgressPct float64 `json:"overall_progress_pct"`
	// NextDeadline is the nearest future deadline across active goals, or nil.
	// No omitempty so it serializes as JSON null when absent.
	NextDeadline *time.Time `json:"next_deadline"`
	// CurrentStreak is the number of consecutive weeks the user made at least one deposit.
	CurrentStreak int `json:"current_streak"`
	// LongestStreak is the user's all-time best consecutive-week streak.
	LongestStreak int `json:"longest_streak"`
}

// ValidateEmoji checks that s is a single emoji character (or empty).
// Returns ErrInvalidGoal if s contains non-emoji runes.
func ValidateEmoji(s string) error {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	for _, r := range runes {
		if !unicode.Is(unicode.So, r) && !unicode.Is(unicode.Sm, r) &&
			!unicode.Is(unicode.Sk, r) && !isEmojiRange(r) {
			return fmt.Errorf("%w: emoji must contain only emoji characters", ErrInvalidGoal)
		}
	}
	return nil
}

func isEmojiRange(r rune) bool {
	return (r >= 0x1F600 && r <= 0x1F64F) || // emoticons
		(r >= 0x1F300 && r <= 0x1F5FF) || // misc symbols
		(r >= 0x1F680 && r <= 0x1F6FF) || // transport
		(r >= 0x1F700 && r <= 0x1F77F) || // alchemical
		(r >= 0x1F780 && r <= 0x1F7FF) || // geometric shapes extended
		(r >= 0x1F800 && r <= 0x1F8FF) || // supplemental arrows
		(r >= 0x1F900 && r <= 0x1F9FF) || // supplemental symbols
		(r >= 0x1FA00 && r <= 0x1FA6F) || // chess symbols
		(r >= 0x1FA70 && r <= 0x1FAFF) || // symbols and pictographs extended
		(r >= 0x2600 && r <= 0x26FF) || // misc symbols
		(r >= 0x2700 && r <= 0x27BF) || // dingbats
		r == 0xFE0F || // variation selector-16
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // flags
		(r >= 0x200D && r <= 0x200D) // zero-width joiner
}

// ValidateContributionLimits checks that a goal's optional min/max
// per-contribution limits (#922) are individually positive and, when both
// are set, that min does not exceed max. Either or both may be nil, meaning
// "no limit" on that side.
func ValidateContributionLimits(min, max *decimal.Decimal) error {
	if min != nil && !min.IsPositive() {
		return fmt.Errorf("%w: min_contribution must be greater than zero", ErrInvalidContributionLimits)
	}
	if max != nil && !max.IsPositive() {
		return fmt.Errorf("%w: max_contribution must be greater than zero", ErrInvalidContributionLimits)
	}
	if min != nil && max != nil && min.GreaterThan(*max) {
		return fmt.Errorf("%w: min_contribution must not exceed max_contribution", ErrInvalidContributionLimits)
	}
	return nil
}

// ValidateContributionAmount checks a deposit amount against a goal's
// optional min/max per-contribution limits (#922). Either limit may be nil,
// meaning that side is unconstrained.
func ValidateContributionAmount(amount decimal.Decimal, min, max *decimal.Decimal) error {
	if min != nil && amount.LessThan(*min) {
		return fmt.Errorf("%w: contribution must be at least %s", ErrContributionOutOfRange, min.String())
	}
	if max != nil && amount.GreaterThan(*max) {
		return fmt.Errorf("%w: contribution must not exceed %s", ErrContributionOutOfRange, max.String())
	}
	return nil
}

func ParseCategory(value string) (GoalCategory, error) {
	category := GoalCategory(strings.ToLower(strings.TrimSpace(value)))
	switch category {
	case CategoryEmergencyFund,
		CategoryEducation,
		CategoryHousing,
		CategoryTravel,
		CategoryBusiness,
		CategoryHealth,
		CategoryRetirement,
		CategoryOther:
		return category, nil
	default:
		return "", fmt.Errorf("%w: invalid category", ErrInvalidGoal)
	}
}

type SavingsGoal struct {
	ID           uuid.UUID       `json:"id"`
	UserID       uuid.UUID       `json:"user_id"`
	VaultID      *uuid.UUID      `json:"vault_id,omitempty"`
	TargetAmount decimal.Decimal `json:"target_amount"`
	Currency     string          `json:"currency"`
	Deadline     time.Time       `json:"deadline"`
	Description  string          `json:"description,omitempty"`
	// Name is a user-supplied display label (#738). Defaults to first 50 chars of Description.
	Name string `json:"name,omitempty"`
	// Emoji is a single Unicode emoji icon (#738).
	Emoji    string       `json:"emoji,omitempty"`
	Category GoalCategory `json:"category"`
	// Icon is an optional icon identifier (e.g. lucide icon name) displayed in the UI.
	// When blank the UI falls back to the category default via DefaultIconForCategory.
	Icon string `json:"icon,omitempty"`
	// Color is an optional UI color hint (e.g. "emerald", "blue", "#3b82f6").
	// When blank the UI falls back to the category default.
	Color string `json:"color,omitempty"`
	// Status is one of "active", "paused", "completed" (#718, #716).
	Status                string          `json:"status"`
	CurrentAmount         decimal.Decimal `json:"current_amount"`
	ProgressPct           float64         `json:"progress_pct"`
	NotifiedMilestones    []int           `json:"-"`
	DeadlineRemindersSent []int           `json:"-"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
	// Completion fields (#716).
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CompletionAction string     `json:"completion_action,omitempty"`
	// Velocity stats (#714).
	AvgWeeklyDeposit        decimal.Decimal `json:"avg_weekly_deposit"`
	ProjectedDaysToComplete *int            `json:"projected_days_to_completion,omitempty"`
	OnTrack                 bool            `json:"on_track"`
	// Sharing fields.
	ShareToken     *uuid.UUID `json:"share_token,omitempty"`
	ShareEnabledAt *time.Time `json:"share_enabled_at,omitempty"`
	IsShared       bool       `json:"is_shared"`
	// AutoCompound controls whether harvested yield is routed back into the
	// goal's vault position (true, the default) or credited to YieldBalance
	// instead of being reinvested (false).
	AutoCompound bool `json:"auto_compound"`
	// YieldBalance accumulates harvested yield that was NOT compounded because
	// AutoCompound is false. It is held separately from CurrentAmount/vault balance.
	YieldBalance decimal.Decimal `json:"yield_balance"`
	// On-chain linkage (#807). OnchainGoalID is nil until asynchronous
	// registration against the savings_goal contract succeeds.
	OnchainGoalID *string `json:"onchain_goal_id,omitempty"`
	OnchainStatus *string `json:"onchain_status,omitempty"`
	// MinContribution/MaxContribution are optional per-contribution limits
	// (#922), useful for merchants/employers seeding structured savings
	// plans (e.g. "at least $50 per deposit" or "no more than $500 per
	// deposit"). Nil means no limit on that side. Enforced at deposit time
	// by ValidateContributionAmount.
	MinContribution *decimal.Decimal `json:"min_contribution,omitempty"`
	MaxContribution *decimal.Decimal `json:"max_contribution,omitempty"`
	// DeletedAt is set when the user deletes the goal (#924). The goal is
	// hidden from all normal reads while set, but remains restorable via
	// Restore until SavingsGoalRecoveryWindow elapses, after which the
	// scheduled purge job hard-deletes it.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// SharedGoalView is the read-only public projection of a savings goal exposed
// via the unauthenticated share link. It deliberately omits user PII.
type SharedGoalView struct {
	Name          string          `json:"name"`
	Emoji         string          `json:"emoji,omitempty"`
	TargetAmount  decimal.Decimal `json:"target_amount"`
	Currency      string          `json:"currency"`
	CurrentAmount decimal.Decimal `json:"current_amount"`
	ProgressPct   float64         `json:"progress_pct"`
	Deadline      time.Time       `json:"deadline"`
	Category      GoalCategory    `json:"category"`
	Status        string          `json:"status"`
}

// GoalDeposit records a single allocation in a multi-goal deposit split (#719).
type GoalDeposit struct {
	ID       uuid.UUID
	GoalID   uuid.UUID
	UserID   uuid.UUID
	Amount   decimal.Decimal
	Currency string
}

type GoalContribution struct {
	ID        uuid.UUID       `json:"id"`
	GoalID    uuid.UUID       `json:"goal_id"`
	UserID    uuid.UUID       `json:"user_id"`
	Amount    decimal.Decimal `json:"amount"`
	Currency  string          `json:"currency"`
	Type      string          `json:"type"`
	TxHash    string          `json:"tx_hash,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Repository interface {
	Create(ctx context.Context, goal *SavingsGoal) error
	ListByUser(ctx context.Context, userID uuid.UUID, category, search string) ([]SavingsGoal, error)
	GetByID(ctx context.Context, id uuid.UUID) (*SavingsGoal, error)
	Update(ctx context.Context, goal *SavingsGoal) error
	Delete(ctx context.Context, id, userID uuid.UUID) error
	SumVaultBalance(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error)
	UpdateMilestones(ctx context.Context, goalID uuid.UUID, milestones []int) error
	UpdateDeadlineReminders(ctx context.Context, goalID uuid.UUID, reminders []int) error
	ListActiveApproachingDeadline(ctx context.Context, maxDays int) ([]SavingsGoal, error)
	ListContributions(ctx context.Context, goalID, userID uuid.UUID, params interface{}) ([]GoalContribution, int, string, error)
	// SumRecentDeposits sums vault deposit amounts for the user in the given currency since `since` (#714).
	SumRecentDeposits(ctx context.Context, userID uuid.UUID, currency string, since time.Time) (decimal.Decimal, error)
	// UpdateStatus sets the goal's status column (#718).
	UpdateStatus(ctx context.Context, goalID, userID uuid.UUID, status string) error
	// MarkCompleted sets completed_at and completion_action (#716).
	MarkCompleted(ctx context.Context, goalID, userID uuid.UUID, action string) error
	// RecordGoalDeposits atomically inserts all deposits in a single DB transaction (#719).
	RecordGoalDeposits(ctx context.Context, deposits []GoalDeposit) error
	// SumGoalDeposits returns the total deposited to a specific goal via deposit-split (#719).
	SumGoalDeposits(ctx context.Context, goalID uuid.UUID) (decimal.Decimal, error)
	// SetShareToken enables public sharing by persisting a UUID token on the goal.
	SetShareToken(ctx context.Context, goalID, userID uuid.UUID, token uuid.UUID) error
	// ClearShareToken removes the share token, revoking public access.
	ClearShareToken(ctx context.Context, goalID, userID uuid.UUID) error
	// GetByShareToken returns the goal whose share_token matches. Returns ErrGoalNotFound if none.
	GetByShareToken(ctx context.Context, token uuid.UUID) (*SavingsGoal, error)
	// GetByVaultID returns the goal linked to the given vault, if any. Returns
	// ErrGoalNotFound if no goal links to that vault.
	GetByVaultID(ctx context.Context, vaultID uuid.UUID) (*SavingsGoal, error)
	// CreditYieldBalance atomically adds amount to the goal's yield_balance,
	// used when harvested yield is not auto-compounded back into the vault.
	CreditYieldBalance(ctx context.Context, goalID uuid.UUID, amount decimal.Decimal) error
	// UpdateOnchainLink persists the result of asynchronously registering the
	// goal against the savings_goal contract (#807).
	UpdateOnchainLink(ctx context.Context, goalID uuid.UUID, onchainGoalID, onchainStatus string) error
	// Restore clears deleted_at, undoing a soft delete (#924). Returns
	// ErrGoalNotFound if no matching row exists for id/userID at all (deleted
	// or not); callers should have already validated the recovery window.
	Restore(ctx context.Context, id, userID uuid.UUID) error
	// GetByIDIncludingDeleted looks up a goal by ID regardless of its
	// deleted_at value (#924), so Restore can check the recovery window
	// against a goal GetByID would otherwise filter out.
	GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*SavingsGoal, error)
	// ListDeletedOlderThan returns soft-deleted goals whose deleted_at is
	// older than cutoff (#924), for the scheduled hard-delete purge job.
	ListDeletedOlderThan(ctx context.Context, cutoff time.Time) ([]SavingsGoal, error)
	// HardDelete permanently removes a goal row (#924). Used only by the
	// recovery-window purge job, never from a user-facing request path.
	HardDelete(ctx context.Context, id uuid.UUID) error
}

// categoryIconDefaults maps each GoalCategory to a default icon name and color
// that the UI can use when the user has not supplied a custom icon or color.
var categoryIconDefaults = map[GoalCategory][2]string{
	CategoryEmergencyFund: {"shield-check", "emerald"},
	CategoryEducation:     {"graduation-cap", "blue"},
	CategoryHousing:       {"home", "orange"},
	CategoryTravel:        {"plane", "sky"},
	CategoryBusiness:      {"briefcase", "violet"},
	CategoryHealth:        {"heart-pulse", "rose"},
	CategoryRetirement:    {"landmark", "amber"},
	CategoryOther:         {"piggy-bank", "slate"},
}

// DefaultIconForCategory returns the default icon name for the given category.
// Returns "piggy-bank" when the category is unknown.
func DefaultIconForCategory(cat GoalCategory) string {
	if v, ok := categoryIconDefaults[cat]; ok {
		return v[0]
	}
	return "piggy-bank"
}

// DefaultColorForCategory returns the default tailwind color name for the given category.
// Returns "slate" when the category is unknown.
func DefaultColorForCategory(cat GoalCategory) string {
	if v, ok := categoryIconDefaults[cat]; ok {
		return v[1]
	}
	return "slate"
}

// GoalTemplate represents a savings goal configuration a user can start
// from. The original set (#778) ships hardcoded via a migration seed; admins
// can additionally publish curated templates at runtime (#919) so the
// catalog can grow without a redeploy.
type GoalTemplate struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Category        GoalCategory    `json:"category"`
	SuggestedAmount decimal.Decimal `json:"suggested_amount"`
	Currency        string          `json:"currency"`
	SuggestedMonths int             `json:"suggested_months"`
	Icon            string          `json:"icon"`
	// IsCustom is true for templates published by an admin (#919) and false
	// for the pre-built defaults seeded by migration 056.
	IsCustom bool `json:"is_custom"`
	// CreatedBy is the admin user who published the template. Nil for the
	// pre-built defaults.
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type TemplateRepository interface {
	List(ctx context.Context) ([]GoalTemplate, error)
	GetByID(ctx context.Context, id uuid.UUID) (*GoalTemplate, error)
	// Create persists an admin-published template (#919).
	Create(ctx context.Context, template *GoalTemplate) error
	// Update modifies an existing admin-published template (#919). Templates
	// seeded as pre-built defaults (IsCustom == false) may still be edited by
	// admins, matching how other admin-managed catalog data works.
	Update(ctx context.Context, template *GoalTemplate) error
	// Delete removes a template from the catalog (#919).
	Delete(ctx context.Context, id uuid.UUID) error
}
