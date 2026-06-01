// Package listquery provides shared pagination, sorting, and filter parsing for list APIs.
package listquery

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPage    = 1
	DefaultPerPage = 20
	MaxPerPage     = 100
)

var ErrInvalidQuery = errors.New("invalid list query parameters")

// PageParams holds offset-based pagination. When UseCursor is true, Page is ignored.
type PageParams struct {
	Page    int
	PerPage int
	Cursor  string
}

// SortParams holds whitelisted sort field and direction.
type SortParams struct {
	Field string
	Order string // asc | desc
}

// ParsePage reads ?page=&per_page=&cursor= from the request query.
func ParsePage(r *http.Request) (PageParams, error) {
	q := r.URL.Query()
	p := PageParams{
		Page:    DefaultPage,
		PerPage: DefaultPerPage,
		Cursor:  strings.TrimSpace(q.Get("cursor")),
	}

	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return PageParams{}, fmt.Errorf("%w: page must be a positive integer", ErrInvalidQuery)
		}
		p.Page = v
	}

	if raw := strings.TrimSpace(q.Get("per_page")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return PageParams{}, fmt.Errorf("%w: per_page must be a positive integer", ErrInvalidQuery)
		}
		if v > MaxPerPage {
			return PageParams{}, fmt.Errorf("%w: per_page must not exceed %d", ErrInvalidQuery, MaxPerPage)
		}
		p.PerPage = v
	}

	return p, nil
}

// ParseSort reads ?sort=&order= with a default field when sort is omitted.
func ParseSort(r *http.Request, defaultField string, allowed map[string]bool) (SortParams, error) {
	q := r.URL.Query()
	field := strings.ToLower(strings.TrimSpace(q.Get("sort")))
	if field == "" {
		field = defaultField
	}
	if !allowed[field] {
		return SortParams{}, fmt.Errorf("%w: invalid sort field %q", ErrInvalidQuery, field)
	}

	order := strings.ToLower(strings.TrimSpace(q.Get("order")))
	if order == "" {
		order = "desc"
	}
	switch order {
	case "asc", "desc":
	default:
		return SortParams{}, fmt.Errorf("%w: order must be asc or desc", ErrInvalidQuery)
	}

	return SortParams{Field: field, Order: order}, nil
}

// Offset returns SQL OFFSET for page-based pagination.
func (p PageParams) Offset() int {
	if p.Cursor != "" {
		return 0
	}
	return (p.Page - 1) * p.PerPage
}

// UsesCursor reports whether cursor-based pagination is requested.
func (p PageParams) UsesCursor() bool {
	return p.Cursor != ""
}

// ParseTimeQuery parses an RFC3339 or YYYY-MM-DD timestamp from a query parameter.
// Date-only values use UTC start of day.
func ParseTimeQuery(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	t, inclusiveEnd, err := parseFlexibleTime(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be RFC3339 or YYYY-MM-DD", ErrInvalidQuery, key)
	}
	if inclusiveEnd {
		end := t.Add(24*time.Hour - time.Nanosecond).UTC()
		return &end, nil
	}
	utc := t.UTC()
	return &utc, nil
}

// ParseTimeQueryStart is like ParseTimeQuery but date-only values always start at UTC midnight.
func ParseTimeQueryStart(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	t, _, err := parseFlexibleTime(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be RFC3339 or YYYY-MM-DD", ErrInvalidQuery, key)
	}
	utc := t.UTC()
	return &utc, nil
}

func parseFlexibleTime(raw string) (time.Time, bool, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, false, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, errors.New("invalid time")
}

// ParseDecimalQuery parses a non-negative decimal string from query.
func ParseDecimalQuery(r *http.Request, key string) (*string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	if raw[0] == '-' {
		return nil, fmt.Errorf("%w: %s must be non-negative", ErrInvalidQuery, key)
	}
	return &raw, nil
}
