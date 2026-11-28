package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
)

// fakeYieldHarvestRepo records the filter it was called with so the service's
// filter plumbing can be asserted without a database.
type fakeYieldHarvestRepo struct {
	lastFilter yieldharvest.ListFilter
	items      []yieldharvest.YieldHarvest
	calls      int
}

func (f *fakeYieldHarvestRepo) Create(context.Context, yieldharvest.CreateInput) (yieldharvest.YieldHarvest, error) {
	return yieldharvest.YieldHarvest{}, nil
}

func (f *fakeYieldHarvestRepo) ListForUser(_ context.Context, filter yieldharvest.ListFilter) ([]yieldharvest.YieldHarvest, error) {
	f.calls++
	f.lastFilter = filter
	return f.items, nil
}

func TestYieldHarvestService_ListHarvestsForwardsVaultFilter(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	repo := &fakeYieldHarvestRepo{}
	svc := NewYieldHarvestService(repo)

	out, err := svc.ListHarvests(context.Background(), ListYieldHarvestsInput{
		UserID:  userID,
		VaultID: &vaultID,
		Limit:   5,
	})
	if err != nil {
		t.Fatalf("ListHarvests() error = %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected no items, got %d", len(out.Items))
	}
	if repo.lastFilter.VaultID == nil {
		t.Fatal("expected vault filter forwarded to repository, got nil")
	}
	if *repo.lastFilter.VaultID != vaultID {
		t.Errorf("vault filter = %s, want %s", *repo.lastFilter.VaultID, vaultID)
	}
	if repo.lastFilter.UserID != userID {
		t.Errorf("user filter = %s, want %s", repo.lastFilter.UserID, userID)
	}
	// One extra row is requested so a next page can be detected.
	if repo.lastFilter.Limit != 6 {
		t.Errorf("limit = %d, want 6", repo.lastFilter.Limit)
	}
}

func TestYieldHarvestService_ListHarvestsWithoutVaultFilter(t *testing.T) {
	repo := &fakeYieldHarvestRepo{}
	svc := NewYieldHarvestService(repo)

	if _, err := svc.ListHarvests(context.Background(), ListYieldHarvestsInput{UserID: uuid.New()}); err != nil {
		t.Fatalf("ListHarvests() error = %v", err)
	}
	if repo.lastFilter.VaultID != nil {
		t.Errorf("expected no vault filter, got %s", *repo.lastFilter.VaultID)
	}
}

func TestYieldHarvestService_ListHarvestsPaginatesWithCursor(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC()
	repo := &fakeYieldHarvestRepo{
		items: []yieldharvest.YieldHarvest{
			{ID: uuid.New(), UserID: userID, Amount: decimal.NewFromInt(2), Currency: "USDC", HarvestedAt: now},
			{ID: uuid.New(), UserID: userID, Amount: decimal.NewFromInt(1), Currency: "USDC", HarvestedAt: now.Add(-time.Hour)},
		},
	}
	svc := NewYieldHarvestService(repo)

	out, err := svc.ListHarvests(context.Background(), ListYieldHarvestsInput{UserID: userID, Limit: 1})
	if err != nil {
		t.Fatalf("ListHarvests() error = %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.NextCursor == "" {
		t.Fatal("expected a next cursor when more rows exist")
	}

	// The cursor must round-trip so the next page continues where this one ended.
	if _, err := svc.ListHarvests(context.Background(), ListYieldHarvestsInput{
		UserID: userID,
		Limit:  1,
		Cursor: out.NextCursor,
	}); err != nil {
		t.Fatalf("ListHarvests() with cursor error = %v", err)
	}
	if repo.lastFilter.CursorTime == nil || repo.lastFilter.CursorID == nil {
		t.Fatal("expected cursor to be decoded and forwarded")
	}
	if *repo.lastFilter.CursorID != out.Items[0].ID {
		t.Errorf("cursor id = %s, want %s", repo.lastFilter.CursorID, out.Items[0].ID)
	}
}
