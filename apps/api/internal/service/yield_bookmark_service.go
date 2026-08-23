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

var (
	ErrYieldBookmarkNotFound  = errors.New("yield bookmark not found")
	ErrYieldBookmarkDuplicate = errors.New("protocol already bookmarked")
)

// YieldBookmark is a saved protocol slug for a user.
type YieldBookmark struct {
	ProtocolSlug string    `json:"protocol_slug"`
	CreatedAt    time.Time `json:"created_at"`
}

// YieldBookmarkWithStats includes live yield data for a bookmarked protocol.
type YieldBookmarkWithStats struct {
	ProtocolSlug string  `json:"protocol_slug"`
	APY          float64 `json:"apy"`
	TVLUsd       float64 `json:"tvl_usd"`
	Symbol       string  `json:"symbol,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// YieldBookmarkDB is the minimal DB surface for bookmark persistence.
type YieldBookmarkDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// YieldBookmarkService manages protocol-level yield bookmarks.
type YieldBookmarkService struct {
	db    YieldBookmarkDB
	yield *YieldService
}

func NewYieldBookmarkService(db YieldBookmarkDB, yield *YieldService) *YieldBookmarkService {
	return &YieldBookmarkService{db: db, yield: yield}
}

func NormalizeProtocolSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func ProtocolSlugFromProject(project string) string {
	s := strings.ToLower(strings.TrimSpace(project))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func (s *YieldBookmarkService) Add(ctx context.Context, userID uuid.UUID, protocolSlug string) (YieldBookmark, error) {
	slug := NormalizeProtocolSlug(protocolSlug)
	if slug == "" {
		return YieldBookmark{}, fmt.Errorf("protocol_slug is required")
	}
	var bm YieldBookmark
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO yield_bookmarks (user_id, protocol_slug)
		VALUES ($1, $2)
		RETURNING protocol_slug, created_at
	`, userID, slug).Scan(&bm.ProtocolSlug, &bm.CreatedAt)
	if err != nil {
		if isDuplicateError(err) {
			return YieldBookmark{}, ErrYieldBookmarkDuplicate
		}
		return YieldBookmark{}, fmt.Errorf("insert yield bookmark: %w", err)
	}
	return bm, nil
}

func (s *YieldBookmarkService) Remove(ctx context.Context, userID uuid.UUID, protocolSlug string) error {
	slug := NormalizeProtocolSlug(protocolSlug)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM yield_bookmarks WHERE user_id = $1 AND protocol_slug = $2
	`, userID, slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrYieldBookmarkNotFound
	}
	return nil
}

func (s *YieldBookmarkService) ListSlugs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT protocol_slug FROM yield_bookmarks
		WHERE user_id = $1 ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

func (s *YieldBookmarkService) ListWithStats(ctx context.Context, userID uuid.UUID, chain string) ([]YieldBookmarkWithStats, error) {
	slugs, err := s.ListSlugs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(slugs) == 0 {
		return []YieldBookmarkWithStats{}, nil
	}

	yieldResp, err := s.yield.GetYieldOpportunities(ctx, chain, 100)
	if err != nil {
		return nil, err
	}

	bySlug := map[string]YieldPool{}
	for _, pool := range yieldResp.Pools {
		slug := ProtocolSlugFromProject(pool.Project)
		if existing, ok := bySlug[slug]; !ok || pool.TVLUsd > existing.TVLUsd {
			bySlug[slug] = pool
		}
	}

	createdAt := map[string]time.Time{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT protocol_slug, created_at FROM yield_bookmarks WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		var at time.Time
		if err := rows.Scan(&slug, &at); err != nil {
			return nil, err
		}
		createdAt[slug] = at
	}

	out := make([]YieldBookmarkWithStats, 0, len(slugs))
	for _, slug := range slugs {
		item := YieldBookmarkWithStats{
			ProtocolSlug: slug,
			CreatedAt:    createdAt[slug],
		}
		if pool, ok := bySlug[slug]; ok {
			item.APY = pool.APY
			item.TVLUsd = pool.TVLUsd
			item.Symbol = pool.Symbol
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *YieldBookmarkService) SortPoolsByBookmarks(pools []YieldPool, bookmarkSlugs []string) []YieldPool {
	if len(bookmarkSlugs) == 0 || len(pools) == 0 {
		return pools
	}
	rank := make(map[string]int, len(bookmarkSlugs))
	for i, slug := range bookmarkSlugs {
		rank[slug] = i
	}
	sorted := make([]YieldPool, len(pools))
	copy(sorted, pools)
	sortYieldPoolsByBookmarks(sorted, rank)
	return sorted
}

func sortYieldPoolsByBookmarks(pools []YieldPool, rank map[string]int) {
	// simple stable sort: bookmarked first by bookmark order, then rest by APY
	for i := 0; i < len(pools); i++ {
		for j := i + 1; j < len(pools); j++ {
			if yieldPoolBookmarkRank(pools[i], rank) > yieldPoolBookmarkRank(pools[j], rank) {
				pools[i], pools[j] = pools[j], pools[i]
			}
		}
	}
}

func yieldPoolBookmarkRank(pool YieldPool, rank map[string]int) int {
	slug := ProtocolSlugFromProject(pool.Project)
	if r, ok := rank[slug]; ok {
		return r
	}
	return 100000 + int(1000-pool.APY)
}
