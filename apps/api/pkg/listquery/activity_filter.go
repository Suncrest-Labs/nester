package listquery

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ActivityListParams is the parsed query for GET /api/v1/activity. Unlike
// the other *_filter.go wrappers, this does NOT go through the bracket-style
// filter[field][op]=value grammar: the frontend's history page already has a
// fixed, working contract using flat params (type, from, to, vault, status,
// cursor, limit) and bidirectional cursor pagination — matching that
// contract literally avoids a frontend rewrite that isn't otherwise needed.
// Sort is fixed to created_at (the only sort the page uses) since the feed
// unions five differently-shaped tables.
type ActivityListParams struct {
	Types   []string // normalized lowercase event types, e.g. "deposit"
	Status  string   // normalized lowercase status, e.g. "completed", "" = any
	VaultID string
	From    *time.Time
	To      *time.Time
	Cursor  string
	Backward bool
	Limit   int
	Search  string
}

// activityTypeDisplayToInternal maps the Title-Case labels FilterBar.tsx
// sends (its checkbox keys) to the internal event type stored in ActivityItem.
var activityTypeDisplayToInternal = map[string]string{
	"Deposit":      "deposit",
	"Withdrawal":   "withdrawal",
	"Rebalance":    "rebalance",
	"Settlement":   "settlement",
	"Yield Earned": "yield_earned",
}

// activityStatusDisplayToInternal maps the Title-Case status the frontend's
// status <select> sends to the internal, normalized status value.
var activityStatusDisplayToInternal = map[string]string{
	"Confirmed": "completed",
	"Pending":   "pending",
	"Failed":    "failed",
}

// ParseActivityList reads list query parameters for GET /api/v1/activity.
func ParseActivityList(r *http.Request) (ActivityListParams, error) {
	q := r.URL.Query()

	params := ActivityListParams{
		Limit:  DefaultPerPage,
		Search: strings.TrimSpace(q.Get("q")),
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return ActivityListParams{}, fmt.Errorf("%w: limit must be a positive integer", ErrInvalidQuery)
		}
		if v > MaxPerPage {
			v = MaxPerPage
		}
		params.Limit = v
	}

	if raw := strings.TrimSpace(q.Get("type")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			internal, ok := activityTypeDisplayToInternal[part]
			if !ok {
				return ActivityListParams{}, fmt.Errorf("%w: unknown activity type %q", ErrInvalidQuery, part)
			}
			params.Types = append(params.Types, internal)
		}
	}

	if raw := strings.TrimSpace(q.Get("status")); raw != "" && raw != "All" {
		internal, ok := activityStatusDisplayToInternal[raw]
		if !ok {
			return ActivityListParams{}, fmt.Errorf("%w: unknown activity status %q", ErrInvalidQuery, raw)
		}
		params.Status = internal
	}

	if raw := strings.TrimSpace(q.Get("vault")); raw != "" {
		if _, err := uuid.Parse(raw); err != nil {
			return ActivityListParams{}, fmt.Errorf("%w: vault must be a valid UUID", ErrInvalidQuery)
		}
		params.VaultID = raw
	}

	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		t, _, err := parseFlexibleTime(raw)
		if err != nil {
			return ActivityListParams{}, fmt.Errorf("%w: from must be RFC3339 or YYYY-MM-DD", ErrInvalidQuery)
		}
		utc := t.UTC()
		params.From = &utc
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		t, inclusiveEnd, err := parseFlexibleTime(raw)
		if err != nil {
			return ActivityListParams{}, fmt.Errorf("%w: to must be RFC3339 or YYYY-MM-DD", ErrInvalidQuery)
		}
		if inclusiveEnd {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		utc := t.UTC()
		params.To = &utc
	}

	cursor := strings.TrimSpace(q.Get("cursor"))
	if cursor != "" {
		kc, err := DecodeKeysetCursor(cursor)
		if err != nil {
			return ActivityListParams{}, err
		}
		params.Cursor = cursor
		params.Backward = kc.Backward
	}

	return params, nil
}
