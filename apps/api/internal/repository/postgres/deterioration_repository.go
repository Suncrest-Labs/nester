package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
)

// DeteriorationRepository is the Postgres-backed deterioration.Repository
// (#857): the audit trail for actions taken on a deterioration assessment,
// plus the assessment history used for calibration review.
type DeteriorationRepository struct {
	db *sql.DB
}

func NewDeteriorationRepository(db *sql.DB) *DeteriorationRepository {
	return &DeteriorationRepository{db: db}
}

func (r *DeteriorationRepository) RecordAction(ctx context.Context, action *deterioration.Action) error {
	if action.ID == uuid.Nil {
		action.ID = uuid.New()
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO deterioration_actions (id, protocol_slug, level, probability, kind, vault_id, rebalance_id, explanation, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at
	`,
		action.ID, action.ProtocolSlug, string(action.Level), action.Probability, string(action.Kind),
		nullUUID(action.VaultID), nullUUID(action.RebalanceID), action.Explanation, nullSQLString(action.Error),
	).Scan(&action.CreatedAt)
}

func (r *DeteriorationRepository) ListActionsByProtocol(ctx context.Context, slug string, limit int) ([]deterioration.Action, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, protocol_slug, level, probability, kind, vault_id, rebalance_id, explanation, error, created_at
		FROM deterioration_actions
		WHERE protocol_slug = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, slug, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []deterioration.Action
	for rows.Next() {
		var (
			idStr, protocolSlug, level, kind, explanation string
			probability                                   float64
			vaultIDStr, rebalanceIDStr                    sql.NullString
			errText                                       sql.NullString
			createdAt                                     time.Time
		)
		if err := rows.Scan(&idStr, &protocolSlug, &level, &probability, &kind, &vaultIDStr, &rebalanceIDStr, &explanation, &errText, &createdAt); err != nil {
			return nil, err
		}
		// uuid.Parse rather than MustParse: a malformed stored value (legacy
		// row, manual edit, migration drift) should fail this read, not
		// panic the process — this is an audit-trail table.
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, err
		}
		action := deterioration.Action{
			ID:           id,
			ProtocolSlug: protocolSlug,
			Level:        deterioration.Level(level),
			Probability:  probability,
			Kind:         deterioration.ActionKind(kind),
			Explanation:  explanation,
			CreatedAt:    createdAt,
		}
		if vaultIDStr.Valid {
			v, err := uuid.Parse(vaultIDStr.String)
			if err != nil {
				return nil, err
			}
			action.VaultID = &v
		}
		if rebalanceIDStr.Valid {
			v, err := uuid.Parse(rebalanceIDStr.String)
			if err != nil {
				return nil, err
			}
			action.RebalanceID = &v
		}
		if errText.Valid {
			action.Error = errText.String
		}
		out = append(out, action)
	}
	return out, rows.Err()
}

func (r *DeteriorationRepository) RecordAssessment(ctx context.Context, a deterioration.Assessment) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO deterioration_assessments (
			protocol_slug, probability, level,
			tvl_outflow_velocity_pct, apy_abnormality_z_score, reported_vs_derived_gap_pct, price_instability, sample_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		a.ProtocolSlug, a.Probability, string(a.Level),
		a.Indicators.TVLOutflowVelocityPct, a.Indicators.APYAbnormalityZScore, a.Indicators.ReportedVsDerivedGapPct, a.Indicators.PriceInstability, a.Indicators.SampleCount,
	)
	return err
}
