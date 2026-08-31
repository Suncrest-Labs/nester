package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
)

func TestDeteriorationRepository_RecordAction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDeteriorationRepository(db)
	action := &deterioration.Action{
		ProtocolSlug: "aave",
		Level:        deterioration.LevelModerate,
		Probability:  0.6,
		Kind:         deterioration.ActionRecommendRebalance,
		Explanation:  "TVL down 42%",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO deterioration_actions (id, protocol_slug, level, probability, kind, vault_id, rebalance_id, explanation, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at
	`)).
		WithArgs(sqlmock.AnyArg(), "aave", "moderate", 0.6, "recommend_rebalance", nil, nil, "TVL down 42%", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	if err := repo.RecordAction(context.Background(), action); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}
	if action.ID == uuid.Nil {
		t.Error("expected an id to be assigned")
	}
}

func TestDeteriorationRepository_RecordAction_WithVaultAndRebalance(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDeteriorationRepository(db)
	vaultID := uuid.New()
	rebalanceID := uuid.New()
	action := &deterioration.Action{
		ProtocolSlug: "aave",
		Level:        deterioration.LevelSevere,
		Probability:  0.9,
		Kind:         deterioration.ActionAutomaticRebalance,
		VaultID:      &vaultID,
		RebalanceID:  &rebalanceID,
		Explanation:  "automatic protective rebalance triggered",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO deterioration_actions (id, protocol_slug, level, probability, kind, vault_id, rebalance_id, explanation, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at
	`)).
		WithArgs(sqlmock.AnyArg(), "aave", "severe", 0.9, "automatic_rebalance", vaultID.String(), rebalanceID.String(), "automatic protective rebalance triggered", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	if err := repo.RecordAction(context.Background(), action); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}
}

func TestDeteriorationRepository_ListActionsByProtocol(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDeteriorationRepository(db)
	id := uuid.New()
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, protocol_slug, level, probability, kind, vault_id, rebalance_id, explanation, error, created_at
		FROM deterioration_actions
		WHERE protocol_slug = $1
		ORDER BY created_at DESC
		LIMIT $2
	`)).WithArgs("aave", 50).WillReturnRows(sqlmock.NewRows([]string{
		"id", "protocol_slug", "level", "probability", "kind", "vault_id", "rebalance_id", "explanation", "error", "created_at",
	}).AddRow(id.String(), "aave", "moderate", 0.6, "recommend_rebalance", nil, nil, "TVL down 42%", nil, now))

	actions, err := repo.ListActionsByProtocol(context.Background(), "aave", 50)
	if err != nil {
		t.Fatalf("ListActionsByProtocol: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].VaultID != nil {
		t.Error("expected nil VaultID for a protocol-wide action")
	}
}

func TestDeteriorationRepository_RecordAssessment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewDeteriorationRepository(db)
	a := deterioration.Assessment{
		ProtocolSlug: "aave",
		Probability:  0.42,
		Level:        deterioration.LevelMild,
		Indicators: deterioration.Indicators{
			TVLOutflowVelocityPct: 15, APYAbnormalityZScore: 0.5, ReportedVsDerivedGapPct: 3, PriceInstability: 0.05, SampleCount: 10,
		},
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO deterioration_assessments (
			protocol_slug, probability, level,
			tvl_outflow_velocity_pct, apy_abnormality_z_score, reported_vs_derived_gap_pct, price_instability, sample_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)).WithArgs("aave", 0.42, "mild", 15.0, 0.5, 3.0, 0.05, 10).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.RecordAssessment(context.Background(), a); err != nil {
		t.Fatalf("RecordAssessment: %v", err)
	}
}
