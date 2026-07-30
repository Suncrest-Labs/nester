package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrWatchlistItemNotFound is returned when a watchlist item does not exist.
var ErrWatchlistItemNotFound = errors.New("watchlist item not found")

// ErrWatchlistDuplicate is returned when the user already saved this pool.
var ErrWatchlistDuplicate = errors.New("pool already in watchlist")

// WatchlistItem is a row in the user_watchlist table.
type WatchlistItem struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	PoolID      string     `json:"pool_id"`
	PoolSymbol  string     `json:"pool_symbol"`
	PoolProject string     `json:"pool_project"`
	PoolChain   string     `json:"pool_chain"`
	APYAtSave   float64    `json:"apy_at_save"`
	TVLUsd      float64    `json:"tvl_usd"`
	AddedAt     time.Time  `json:"added_at"`
}

// AddWatchlistItemRequest is the body for POST /api/v1/users/watchlist.
type AddWatchlistItemRequest struct {
	PoolID      string  `json:"pool_id"`
	PoolSymbol  string  `json:"pool_symbol"`
	PoolProject string  `json:"pool_project"`
	PoolChain   string  `json:"pool_chain"`
	APYAtSave   float64 `json:"apy_at_save"`
	TVLUsd      float64 `json:"tvl_usd"`
}

// WatchlistDB is the minimal DB interface the service requires.
type WatchlistDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WatchlistService manages user watchlist CRUD.
type WatchlistService struct {
	db WatchlistDB
}

func NewWatchlistService(db WatchlistDB) *WatchlistService {
	return &WatchlistService{db: db}
}

// Add inserts a new watchlist item for the user. Returns ErrWatchlistDuplicate if already present.
func (s *WatchlistService) Add(ctx context.Context, userID uuid.UUID, req AddWatchlistItemRequest) (WatchlistItem, error) {
	if req.PoolID == "" {
		return WatchlistItem{}, fmt.Errorf("pool_id is required")
	}

	var item WatchlistItem
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO user_watchlist (user_id, pool_id, pool_symbol, pool_project, pool_chain, apy_at_save, tvl_usd)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, pool_id, pool_symbol, pool_project, pool_chain, apy_at_save, tvl_usd, added_at
	`, userID, req.PoolID, req.PoolSymbol, req.PoolProject, req.PoolChain, req.APYAtSave, req.TVLUsd).
		Scan(&item.ID, &item.UserID, &item.PoolID, &item.PoolSymbol, &item.PoolProject,
			&item.PoolChain, &item.APYAtSave, &item.TVLUsd, &item.AddedAt)
	if err != nil {
		if isDuplicateError(err) {
			return WatchlistItem{}, ErrWatchlistDuplicate
		}
		return WatchlistItem{}, fmt.Errorf("insert watchlist item: %w", err)
	}
	return item, nil
}

// List returns all watchlist items for a user, newest first.
func (s *WatchlistService) List(ctx context.Context, userID uuid.UUID) ([]WatchlistItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, pool_id, pool_symbol, pool_project, pool_chain, apy_at_save, tvl_usd, added_at
		FROM user_watchlist
		WHERE user_id = $1
		ORDER BY added_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	defer rows.Close()

	var items []WatchlistItem
	for rows.Next() {
		var item WatchlistItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.PoolID, &item.PoolSymbol,
			&item.PoolProject, &item.PoolChain, &item.APYAtSave, &item.TVLUsd, &item.AddedAt); err != nil {
			return nil, fmt.Errorf("scan watchlist row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("watchlist rows: %w", err)
	}
	if items == nil {
		items = []WatchlistItem{}
	}
	return items, nil
}

// Delete removes a watchlist item by ID, scoped to the user.
func (s *WatchlistService) Delete(ctx context.Context, userID, itemID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM user_watchlist WHERE id = $1 AND user_id = $2
	`, itemID, userID)
	if err != nil {
		return fmt.Errorf("delete watchlist item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWatchlistItemNotFound
	}
	return nil
}

// isDuplicateError detects a PostgreSQL unique-constraint violation (23505).
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint")
}
