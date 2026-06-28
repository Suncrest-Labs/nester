package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
)

// YieldHarvestRecord is the input type used by VaultService to record a harvest.
type YieldHarvestRecord struct {
	UserID      uuid.UUID
	VaultID     uuid.UUID
	Amount      decimal.Decimal
	Currency    string
	HarvestedAt time.Time
	TxHash      string
}

// YieldHarvestRecorder is implemented by YieldHarvestService and consumed by VaultService.
type YieldHarvestRecorder interface {
	RecordHarvest(ctx context.Context, record YieldHarvestRecord) error
}

// ListYieldHarvestsInput carries pagination parameters for GET /yields/harvests.
type ListYieldHarvestsInput struct {
	UserID uuid.UUID
	Cursor string
	Limit  int
}

// ListYieldHarvestsOutput is the paginated response.
type ListYieldHarvestsOutput struct {
	Items      []yieldharvest.YieldHarvest `json:"items"`
	NextCursor string                      `json:"next_cursor"`
}

// YieldHarvestService handles business logic for yield harvest records.
type YieldHarvestService struct {
	repo yieldharvest.Repository
}

// NewYieldHarvestService constructs a YieldHarvestService.
func NewYieldHarvestService(repo yieldharvest.Repository) *YieldHarvestService {
	return &YieldHarvestService{repo: repo}
}

// RecordHarvest persists a harvest record. Implements YieldHarvestRecorder.
func (s *YieldHarvestService) RecordHarvest(ctx context.Context, record YieldHarvestRecord) error {
	if record.Amount.IsZero() || record.Amount.IsNegative() {
		return nil
	}
	harvestedAt := record.HarvestedAt
	if harvestedAt.IsZero() {
		harvestedAt = time.Now().UTC()
	}
	_, err := s.repo.Create(ctx, yieldharvest.CreateInput{
		UserID:      record.UserID,
		VaultID:     record.VaultID,
		Amount:      record.Amount,
		Currency:    record.Currency,
		HarvestedAt: harvestedAt,
		TxHash:      record.TxHash,
	})
	return err
}

// ListHarvests returns a cursor-paginated list of harvest records for the authenticated user.
func (s *YieldHarvestService) ListHarvests(ctx context.Context, input ListYieldHarvestsInput) (ListYieldHarvestsOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	filter := yieldharvest.ListFilter{
		UserID: input.UserID,
		Limit:  limit + 1, // fetch one extra to determine if there is a next page
	}

	if input.Cursor != "" {
		cursorTime, cursorID, err := decodeCursor(input.Cursor)
		if err != nil {
			return ListYieldHarvestsOutput{}, fmt.Errorf("invalid cursor: %w", err)
		}
		filter.CursorTime = &cursorTime
		filter.CursorID = &cursorID
	}

	items, err := s.repo.ListForUser(ctx, filter)
	if err != nil {
		return ListYieldHarvestsOutput{}, err
	}

	var nextCursor string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		nextCursor = encodeCursor(last.HarvestedAt, last.ID)
	}

	return ListYieldHarvestsOutput{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// encodeCursor encodes harvested_at and id as a base64 opaque cursor.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor reverses encodeCursor.
func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("cursor id: %w", err)
	}
	return t, id, nil
}
