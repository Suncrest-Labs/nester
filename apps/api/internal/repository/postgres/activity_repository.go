package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/activity"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

type ActivityRepository struct {
	db *sql.DB
}

func NewActivityRepository(db *sql.DB) *ActivityRepository {
	return &ActivityRepository{db: db}
}

// List returns one page of the unified activity feed for userID: deposits,
// withdrawals, and rebalances from vault_transactions, settlements from
// settlements, and yield harvests from yield_harvests, merged and ordered by
// created_at. vault_transactions.type='harvest' rows are deliberately
// excluded — the same harvest event is already represented via
// yield_harvests, and including both would double-count it.
func (r *ActivityRepository) List(ctx context.Context, userID uuid.UUID, filter activity.ListFilter) (items []activity.Item, nextCursor, prevCursor string, err error) {
	where, args := buildActivityWhere(userID, filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = listquery.DefaultPerPage
	}

	scanOrderDir := "DESC"
	hasCursor := filter.Cursor != ""
	backward := false
	if hasCursor {
		kc, decErr := listquery.DecodeKeysetCursor(filter.Cursor)
		if decErr != nil {
			return nil, "", "", decErr
		}
		backward = kc.Backward
		cursorTime, timeErr := time.Parse(time.RFC3339Nano, kc.SortValue)
		if timeErr != nil {
			return nil, "", "", fmt.Errorf("%w: invalid cursor timestamp", listquery.ErrInvalidQuery)
		}
		var keysetFrag string
		var keysetArgs []any
		keysetFrag, scanOrderDir, keysetArgs = listquery.KeysetClause(cursorTime, kc.ID, backward, "created_at", "desc", len(args))
		where = where + " AND " + keysetFrag
		args = append(args, keysetArgs...)
	}

	query := fmt.Sprintf(`
		WITH activity_feed AS (
			SELECT vt.id, vt.type AS type, vt.amount, v.currency,
			       'completed'::text AS status, vt.created_at,
			       vt.vault_id, COALESCE(v.name, v.currency || ' Vault') AS vault_name,
			       COALESCE(vt.transaction_hash, '') AS ref, vt.search_vector AS search_vector
			  FROM vault_transactions vt
			  JOIN vaults v ON v.id = vt.vault_id
			 WHERE vt.user_id = $1 AND vt.type IN ('deposit', 'withdrawal', 'rebalance')

			UNION ALL

			SELECT s.id, 'settlement' AS type, s.amount, s.currency,
			       CASE s.status
			           WHEN 'confirmed' THEN 'completed'
			           WHEN 'failed' THEN 'failed'
			           ELSE 'pending'
			       END AS status,
			       s.created_at, s.vault_id, COALESCE(v.name, v.currency || ' Vault') AS vault_name,
			       ''::text AS ref, s.search_vector AS search_vector
			  FROM settlements s
			  JOIN vaults v ON v.id = s.vault_id
			 WHERE s.user_id = $1

			UNION ALL

			SELECT yh.id, 'yield_earned' AS type, yh.amount, yh.currency,
			       'completed'::text AS status, yh.harvested_at AS created_at,
			       yh.vault_id, COALESCE(v.name, v.currency || ' Vault') AS vault_name,
			       COALESCE(yh.tx_hash, '') AS ref, to_tsvector('english', '') AS search_vector
			  FROM yield_harvests yh
			  JOIN vaults v ON v.id = yh.vault_id
			 WHERE yh.user_id = $1
		)
		SELECT id, type, amount, currency, status, created_at, vault_id, vault_name, ref
		FROM activity_feed
		WHERE %s
		ORDER BY created_at %s, id %s
		LIMIT $%d
	`, where, scanOrderDir, scanOrderDir, len(args)+1) // #nosec G201 -- where/scanOrderDir built only from the allowlisted clauses in buildActivityWhere and listquery.KeysetClause, never client-controlled strings; values are $N placeholders

	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", "", mapRepositoryError(err)
	}
	defer rows.Close()

	for rows.Next() {
		it, scanErr := scanActivityItem(rows)
		if scanErr != nil {
			return nil, "", "", scanErr
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, "", "", err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	if backward {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	if len(items) == 0 {
		return items, "", "", nil
	}

	if backward {
		// Scanned ascending toward the cursor, then reversed to visual
		// (desc) order: the last item is the one closest to where the
		// caller started — paginating forward from it returns to the page
		// they came from, so a next cursor always exists here.
		last := items[len(items)-1]
		nextCursor = listquery.EncodeKeysetCursor(listquery.KeysetCursor{
			SortValue: last.CreatedAt.UTC().Format(time.RFC3339Nano), ID: last.ID, Backward: false,
		})
		if hasMore {
			first := items[0]
			prevCursor = listquery.EncodeKeysetCursor(listquery.KeysetCursor{
				SortValue: first.CreatedAt.UTC().Format(time.RFC3339Nano), ID: first.ID, Backward: true,
			})
		}
		return items, nextCursor, prevCursor, nil
	}

	if hasCursor {
		first := items[0]
		prevCursor = listquery.EncodeKeysetCursor(listquery.KeysetCursor{
			SortValue: first.CreatedAt.UTC().Format(time.RFC3339Nano), ID: first.ID, Backward: true,
		})
	}
	if hasMore {
		last := items[len(items)-1]
		nextCursor = listquery.EncodeKeysetCursor(listquery.KeysetCursor{
			SortValue: last.CreatedAt.UTC().Format(time.RFC3339Nano), ID: last.ID, Backward: false,
		})
	}
	return items, nextCursor, prevCursor, nil
}

func buildActivityWhere(userID uuid.UUID, filter activity.ListFilter) (string, []any) {
	clauses := []string{"user_id = $1"}
	args := []any{userID.String()}

	if len(filter.Types) > 0 {
		placeholders := make([]string, len(filter.Types))
		for i, t := range filter.Types {
			args = append(args, string(t))
			placeholders[i] = fmt.Sprintf("$%d", len(args))
		}
		clauses = append(clauses, fmt.Sprintf("type IN (%s)", strings.Join(placeholders, ", ")))
	}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.VaultID != "" {
		args = append(args, filter.VaultID)
		clauses = append(clauses, fmt.Sprintf("vault_id = $%d", len(args)))
	}
	if filter.From != nil {
		args = append(args, filter.From.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if filter.To != nil {
		args = append(args, filter.To.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, filter.Search)
		clauses = append(clauses, fmt.Sprintf("search_vector @@ plainto_tsquery('english', $%d)", len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func scanActivityItem(row scanner) (activity.Item, error) {
	var (
		id        string
		itemType  string
		amount    string
		currency  string
		status    string
		createdAt time.Time
		vaultID   string
		vaultName string
		ref       string
	)
	if err := row.Scan(&id, &itemType, &amount, &currency, &status, &createdAt, &vaultID, &vaultName, &ref); err != nil {
		return activity.Item{}, err
	}
	amt, err := decimal.NewFromString(amount)
	if err != nil {
		return activity.Item{}, err
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return activity.Item{}, err
	}
	parsedVaultID, err := uuid.Parse(vaultID)
	if err != nil {
		return activity.Item{}, err
	}
	return activity.Item{
		ID:        parsedID,
		Type:      activity.EventType(itemType),
		Amount:    amt,
		Currency:  currency,
		Status:    activity.Status(status),
		CreatedAt: createdAt.UTC(),
		VaultID:   parsedVaultID,
		VaultName: vaultName,
		Ref:       ref,
	}, nil
}
