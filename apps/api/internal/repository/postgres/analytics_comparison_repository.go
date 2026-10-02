package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/analytics"
)

// AnalyticsComparisonRepository aggregates apy_history by protocol (via each
// vault's allocations) for the protocol yield comparison endpoint (#1324).
type AnalyticsComparisonRepository struct {
	db *sql.DB
}

// NewAnalyticsComparisonRepository constructs an AnalyticsComparisonRepository.
func NewAnalyticsComparisonRepository(db *sql.DB) *AnalyticsComparisonRepository {
	return &AnalyticsComparisonRepository{db: db}
}

// ProtocolYieldHistory averages apy_history.realized_apy across all vaults
// allocated to each protocol, bucketed by day, since the given time. A vault
// can hold allocations to multiple protocols; each protocol's series only
// reflects days where at least one allocated vault has an apy_history row.
func (r *AnalyticsComparisonRepository) ProtocolYieldHistory(ctx context.Context, since time.Time) ([]analytics.ProtocolYieldSeries, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.protocol,
		       date_trunc('day', h.calculated_at) AS day,
		       AVG(h.realized_apy) AS avg_apy
		FROM apy_history h
		JOIN allocations a ON a.vault_id = h.vault_id
		WHERE h.period = 'all' AND h.calculated_at >= $1
		GROUP BY a.protocol, day
		ORDER BY a.protocol, day`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seriesByProtocol := map[string]*analytics.ProtocolYieldSeries{}
	var order []string

	for rows.Next() {
		var protocol string
		var day time.Time
		var avgAPY float64
		if err := rows.Scan(&protocol, &day, &avgAPY); err != nil {
			return nil, err
		}
		s, ok := seriesByProtocol[protocol]
		if !ok {
			s = &analytics.ProtocolYieldSeries{Protocol: protocol}
			seriesByProtocol[protocol] = s
			order = append(order, protocol)
		}
		s.Points = append(s.Points, analytics.ProtocolYieldPoint{
			Date: day.UTC().Format("2006-01-02"),
			APY:  avgAPY,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]analytics.ProtocolYieldSeries, 0, len(order))
	for _, protocol := range order {
		result = append(result, *seriesByProtocol[protocol])
	}
	return result, nil
}
