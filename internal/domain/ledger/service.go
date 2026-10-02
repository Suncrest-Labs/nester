package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrDuplicateEntry = errors.New("ledger entry already exists")
)

type Entry struct {
	ID         int64
	SourceType string
	SourceID   string
	EntryType  string
	Amount     int64
	Currency   string
	CreatedAt  time.Time
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) PostEntry(ctx context.Context, sourceType, sourceID, entryType string, amount int64, currency string) (*Entry, error) {
	entry := &Entry{
		SourceType: sourceType,
		SourceID:   sourceID,
		EntryType:  entryType,
		Amount:     amount,
		Currency:   currency,
		CreatedAt:  time.Now(),
	}

	err := s.repo.CreateEntry(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("failed to post ledger entry: %w", err)
	}

	return entry,
		nil
}
