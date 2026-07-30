package timeseries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Ingest(ctx context.Context, point Point) error {
	seriesKey, err := point.Series.Key()
	if err != nil {
		return err
	}
	if point.ObservedAt.IsZero() {
		point.ObservedAt = time.Now().UTC()
	}
	dimensions := []byte("{}")
	if point.Series.Dimensions != nil {
		encoded, err := json.Marshal(point.Series.Dimensions)
		if err != nil {
			return fmt.Errorf("timeseries: encode dimensions: %w", err)
		}
		dimensions = encoded
	}

	const stmt = `
		INSERT INTO timeseries_raw
			(series_key, metric, entity_type, entity_id, observed_at, value, dimensions)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::jsonb, '{}'::jsonb))
		ON CONFLICT (series_key, observed_at)
		DO UPDATE SET
			value = EXCLUDED.value,
			dimensions = EXCLUDED.dimensions,
			created_at = NOW()
	`
	if _, err := s.db.ExecContext(ctx, stmt,
		seriesKey,
		string(point.Series.Metric),
		point.Series.EntityType,
		point.Series.EntityID,
		point.ObservedAt.UTC(),
		point.Value.String(),
		string(dimensions),
	); err != nil {
		return fmt.Errorf("timeseries: ingest: %w", err)
	}
	return nil
}

func (s *PostgresStore) Query(ctx context.Context, req QueryRequest) (QueryResult, error) {
	seriesKey, err := req.Series.Key()
	if err != nil {
		return QueryResult{}, err
	}
	resolution := SelectTier(req, time.Now().UTC())
	if !ValidResolution(resolution) {
		return QueryResult{}, fmt.Errorf("timeseries: invalid query resolution %q", resolution)
	}

	if resolution == ResolutionRaw {
		return s.queryRaw(ctx, seriesKey, req)
	}
	return s.queryRollup(ctx, seriesKey, req, resolution)
}

func (s *PostgresStore) Rollup(ctx context.Context, resolution Resolution, from, to time.Time) error {
	if !ValidResolution(resolution) || resolution == ResolutionRaw {
		return fmt.Errorf("timeseries: invalid rollup resolution %q", resolution)
	}
	if !from.Before(to) {
		return nil
	}

	stmt, args := rollupStatement(resolution, from.UTC(), to.UTC())
	if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("timeseries: rollup %s: %w", resolution, err)
	}
	return nil
}

func (s *PostgresStore) SafeDeleteRawBefore(ctx context.Context, before time.Time) (int64, error) {
	const stmt = `
		DELETE FROM timeseries_raw raw
		WHERE raw.observed_at < $1
		  AND EXISTS (
			SELECT 1
			FROM timeseries_rollups rollup
			WHERE rollup.series_key = raw.series_key
			  AND rollup.resolution = 'minute'
			  AND rollup.bucket_start = date_trunc('minute', raw.observed_at)
		  )
	`
	result, err := s.db.ExecContext(ctx, stmt, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("timeseries: safe raw retention: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("timeseries: raw retention rows affected: %w", err)
	}
	return deleted, nil
}

func (s *PostgresStore) Checkpoint(ctx context.Context, resolution Resolution) (time.Time, bool, error) {
	const stmt = `
		SELECT processed_until
		FROM timeseries_rollup_checkpoints
		WHERE resolution = $1
	`
	var processedUntil time.Time
	if err := s.db.QueryRowContext(ctx, stmt, string(resolution)).Scan(&processedUntil); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("timeseries: read checkpoint: %w", err)
	}
	return processedUntil.UTC(), true, nil
}

func (s *PostgresStore) SetCheckpoint(ctx context.Context, resolution Resolution, processedUntil time.Time) error {
	const stmt = `
		INSERT INTO timeseries_rollup_checkpoints (resolution, processed_until, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (resolution)
		DO UPDATE SET
			processed_until = GREATEST(timeseries_rollup_checkpoints.processed_until, EXCLUDED.processed_until),
			updated_at = NOW()
	`
	if _, err := s.db.ExecContext(ctx, stmt, string(resolution), processedUntil.UTC()); err != nil {
		return fmt.Errorf("timeseries: set checkpoint: %w", err)
	}
	return nil
}

func (s *PostgresStore) queryRaw(ctx context.Context, seriesKey string, req QueryRequest) (QueryResult, error) {
	const stmt = `
		SELECT observed_at, value::text
		FROM timeseries_raw
		WHERE series_key = $1
		  AND observed_at >= $2
		  AND observed_at < $3
		ORDER BY observed_at ASC
	`
	rows, err := s.db.QueryContext(ctx, stmt, seriesKey, req.From.UTC(), req.To.UTC())
	if err != nil {
		return QueryResult{}, fmt.Errorf("timeseries: query raw: %w", err)
	}
	defer rows.Close()

	result := QueryResult{Resolution: ResolutionRaw}
	for rows.Next() {
		var bucket time.Time
		var valueText string
		if err := rows.Scan(&bucket, &valueText); err != nil {
			return QueryResult{}, err
		}
		value, err := decimal.NewFromString(valueText)
		if err != nil {
			return QueryResult{}, fmt.Errorf("timeseries: parse raw value: %w", err)
		}
		result.Points = append(result.Points, RollupPoint{
			SeriesKey:   seriesKey,
			Resolution:  ResolutionRaw,
			BucketStart: bucket.UTC(),
			Open:        value,
			High:        value,
			Low:         value,
			Close:       value,
			Average:     value,
			Last:        value,
			Count:       1,
		})
	}
	return result, rows.Err()
}

func (s *PostgresStore) queryRollup(ctx context.Context, seriesKey string, req QueryRequest, resolution Resolution) (QueryResult, error) {
	const stmt = `
		SELECT
			series_key, metric, entity_type, entity_id, bucket_start,
			open::text, high::text, low::text, close::text, average::text, last::text, point_count
		FROM timeseries_rollups
		WHERE series_key = $1
		  AND resolution = $2
		  AND bucket_start >= $3
		  AND bucket_start < $4
		ORDER BY bucket_start ASC
	`
	rows, err := s.db.QueryContext(ctx, stmt, seriesKey, string(resolution), req.From.UTC(), req.To.UTC())
	if err != nil {
		return QueryResult{}, fmt.Errorf("timeseries: query rollups: %w", err)
	}
	defer rows.Close()

	result := QueryResult{Resolution: resolution}
	for rows.Next() {
		point, err := scanRollup(rows, resolution)
		if err != nil {
			return QueryResult{}, err
		}
		result.Points = append(result.Points, point)
	}
	return result, rows.Err()
}

func scanRollup(rows *sql.Rows, resolution Resolution) (RollupPoint, error) {
	var point RollupPoint
	var metric, openText, highText, lowText, closeText, averageText, lastText string
	if err := rows.Scan(
		&point.SeriesKey,
		&metric,
		&point.EntityType,
		&point.EntityID,
		&point.BucketStart,
		&openText,
		&highText,
		&lowText,
		&closeText,
		&averageText,
		&lastText,
		&point.Count,
	); err != nil {
		return RollupPoint{}, err
	}

	values := []*decimal.Decimal{&point.Open, &point.High, &point.Low, &point.Close, &point.Average, &point.Last}
	for i, text := range []string{openText, highText, lowText, closeText, averageText, lastText} {
		value, err := decimal.NewFromString(text)
		if err != nil {
			return RollupPoint{}, fmt.Errorf("timeseries: parse rollup value: %w", err)
		}
		*values[i] = value
	}
	point.Metric = Metric(metric)
	point.Resolution = resolution
	point.BucketStart = point.BucketStart.UTC()
	return point, nil
}

func rollupStatement(resolution Resolution, from, to time.Time) (string, []any) {
	switch resolution {
	case ResolutionMinute:
		return `
			WITH source AS (
				SELECT
					series_key, metric, entity_type, entity_id,
					date_trunc('minute', observed_at) AS bucket_start,
					observed_at AS point_at,
					value
				FROM timeseries_raw
				WHERE observed_at >= $1 AND observed_at < $2
			),
			aggregated AS (
				SELECT
					series_key, metric, entity_type, entity_id, bucket_start,
					(array_agg(value ORDER BY point_at ASC))[1] AS open,
					MAX(value) AS high,
					MIN(value) AS low,
					(array_agg(value ORDER BY point_at DESC))[1] AS close,
					AVG(value) AS average,
					(array_agg(value ORDER BY point_at DESC))[1] AS last,
					COUNT(*) AS point_count
				FROM source
				GROUP BY series_key, metric, entity_type, entity_id, bucket_start
			)
			INSERT INTO timeseries_rollups
				(series_key, metric, entity_type, entity_id, resolution, bucket_start, open, high, low, close, average, last, point_count)
			SELECT series_key, metric, entity_type, entity_id, 'minute', bucket_start, open, high, low, close, average, last, point_count
			FROM aggregated
			ON CONFLICT (series_key, resolution, bucket_start)
			DO UPDATE SET
				open = EXCLUDED.open,
				high = EXCLUDED.high,
				low = EXCLUDED.low,
				close = EXCLUDED.close,
				average = EXCLUDED.average,
				last = EXCLUDED.last,
				point_count = EXCLUDED.point_count,
				updated_at = NOW()
		`, []any{from, to}
	case ResolutionHour:
		return rollupFromRollupsStatement("hour", "minute"), []any{from, to}
	default:
		return rollupFromRollupsStatement("day", "hour"), []any{from, to}
	}
}

func rollupFromRollupsStatement(targetUnit, sourceResolution string) string {
	return fmt.Sprintf(`
		WITH source AS (
			SELECT
				series_key, metric, entity_type, entity_id,
				date_trunc('%s', bucket_start) AS bucket_start,
				bucket_start AS point_at,
				open, high, low, close, average, last, point_count
			FROM timeseries_rollups
			WHERE resolution = '%s'
			  AND bucket_start >= $1
			  AND bucket_start < $2
		),
		aggregated AS (
			SELECT
				series_key, metric, entity_type, entity_id, bucket_start,
				(array_agg(open ORDER BY point_at ASC))[1] AS open,
				MAX(high) AS high,
				MIN(low) AS low,
				(array_agg(close ORDER BY point_at DESC))[1] AS close,
				SUM(average * point_count) / NULLIF(SUM(point_count), 0) AS average,
				(array_agg(last ORDER BY point_at DESC))[1] AS last,
				SUM(point_count) AS point_count
			FROM source
			GROUP BY series_key, metric, entity_type, entity_id, bucket_start
		)
		INSERT INTO timeseries_rollups
			(series_key, metric, entity_type, entity_id, resolution, bucket_start, open, high, low, close, average, last, point_count)
		SELECT series_key, metric, entity_type, entity_id, '%s', bucket_start, open, high, low, close, average, last, point_count
		FROM aggregated
		ON CONFLICT (series_key, resolution, bucket_start)
		DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			average = EXCLUDED.average,
			last = EXCLUDED.last,
			point_count = EXCLUDED.point_count,
			updated_at = NOW()
	`, targetUnit, sourceResolution, targetUnit)
}
