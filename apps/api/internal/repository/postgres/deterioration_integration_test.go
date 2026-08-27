package postgres

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"

	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// applyDeteriorationIntegrationMigrations wipes and applies only the
// migrations deterioration_actions/deterioration_assessments and
// protocol_tvl_snapshots need: users -> vaults -> vault_rebalances (for
// deterioration_actions' foreign keys) plus the two feature migrations
// themselves.
func applyDeteriorationIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	// The full migration chain in numeric order — see testutil.ApplyAllMigrations
	// for why no per-test subset is used.
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))
}

func TestProtocolTVLRepository_ListSince_Integration(t *testing.T) {
	db := openIntegrationDB(t)
	applyDeteriorationIntegrationMigrations(t, db)

	repo := NewProtocolTVLRepository(db)
	ctx := context.Background()

	if err := repo.InsertSnapshot(ctx, "aave", 1_000_000); err != nil {
		t.Fatalf("InsertSnapshot 1: %v", err)
	}
	if err := repo.InsertSnapshot(ctx, "aave", 900_000); err != nil {
		t.Fatalf("InsertSnapshot 2: %v", err)
	}
	if err := repo.InsertSnapshot(ctx, "compound", 500_000); err != nil {
		t.Fatalf("InsertSnapshot (other protocol): %v", err)
	}

	snaps, err := repo.ListSince(ctx, "aave", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots for aave, got %d", len(snaps))
	}
	// Oldest first.
	if snaps[0].TVLUSD != 1_000_000 || snaps[1].TVLUSD != 900_000 {
		t.Errorf("expected snapshots in insertion order, got %v", snaps)
	}
}

func TestProtocolTVLRepository_ListSince_ExcludesOlderSnapshots(t *testing.T) {
	db := openIntegrationDB(t)
	applyDeteriorationIntegrationMigrations(t, db)

	repo := NewProtocolTVLRepository(db)
	ctx := context.Background()

	if _, err := db.Exec(`
		INSERT INTO protocol_tvl_snapshots (protocol_slug, tvl_usd, snapshotted_at)
		VALUES ('aave', 2000000, NOW() - INTERVAL '48 hours')
	`); err != nil {
		t.Fatalf("seed old snapshot: %v", err)
	}
	if err := repo.InsertSnapshot(ctx, "aave", 1_000_000); err != nil {
		t.Fatalf("InsertSnapshot: %v", err)
	}

	snaps, err := repo.ListSince(ctx, "aave", time.Now().Add(-6*time.Hour))
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected only the recent snapshot within the window, got %d", len(snaps))
	}
	if snaps[0].TVLUSD != 1_000_000 {
		t.Errorf("expected the recent snapshot's value, got %v", snaps[0].TVLUSD)
	}
}

func TestDeteriorationRepository_RecordAndListActions_Integration(t *testing.T) {
	db := openIntegrationDB(t)
	applyDeteriorationIntegrationMigrations(t, db)

	repo := NewDeteriorationRepository(db)
	ctx := context.Background()

	action := &deterioration.Action{
		ProtocolSlug: "aave",
		Level:        deterioration.LevelModerate,
		Probability:  0.6,
		Kind:         deterioration.ActionRecommendRebalance,
		Explanation:  "TVL down 40% in the window",
	}
	if err := repo.RecordAction(ctx, action); err != nil {
		t.Fatalf("RecordAction: %v", err)
	}
	if action.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated after insert")
	}

	actions, err := repo.ListActionsByProtocol(ctx, "aave", 10)
	if err != nil {
		t.Fatalf("ListActionsByProtocol: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Explanation != "TVL down 40% in the window" {
		t.Errorf("unexpected explanation: %q", actions[0].Explanation)
	}
	if actions[0].VaultID != nil {
		t.Error("expected nil VaultID for a protocol-wide recommendation")
	}
}

func TestDeteriorationRepository_RecordAssessment_Integration(t *testing.T) {
	db := openIntegrationDB(t)
	applyDeteriorationIntegrationMigrations(t, db)

	repo := NewDeteriorationRepository(db)
	ctx := context.Background()

	a := deterioration.Assessment{
		ProtocolSlug: "aave",
		Probability:  0.42,
		Level:        deterioration.LevelMild,
		Indicators: deterioration.Indicators{
			TVLOutflowVelocityPct:   15,
			APYAbnormalityZScore:    0.5,
			ReportedVsDerivedGapPct: 3,
			PriceInstability:        0.05,
			SampleCount:             10,
		},
	}
	if err := repo.RecordAssessment(ctx, a); err != nil {
		t.Fatalf("RecordAssessment: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deterioration_assessments WHERE protocol_slug = $1`, "aave").Scan(&count); err != nil {
		t.Fatalf("count assessments: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 recorded assessment, got %d", count)
	}
}
