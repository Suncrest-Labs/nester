package stellar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PostgresSubmissionStore is the durable SubmissionStore (nester#1085).
//
// Two database guarantees carry the whole design, and neither is reproducible
// in application code:
//
//   - The UNIQUE constraint on idempotency_reference makes Claim atomic across
//     processes, so concurrent duplicate requests collapse to one submission
//     instead of racing a read-then-write.
//   - SELECT ... FOR UPDATE SKIP LOCKED lets several reconcilers sweep at once
//     without ever taking the same submission.
type PostgresSubmissionStore struct {
	db *sql.DB

	// reconcileBackoff spaces out repeated checks of a submission the chain
	// has not decided yet, so an RPC outage does not become a tight loop
	// against a recovering endpoint.
	reconcileBackoff time.Duration
}

// DefaultReconcileBackoff is how long a still-undecided submission waits
// before the reconciler looks at it again. Short relative to the five-minute
// transaction validity window, so a submission that will resolve does so
// promptly.
const DefaultReconcileBackoff = 20 * time.Second

// NewPostgresSubmissionStore builds the durable store.
func NewPostgresSubmissionStore(db *sql.DB) *PostgresSubmissionStore {
	return &PostgresSubmissionStore{db: db, reconcileBackoff: DefaultReconcileBackoff}
}

const submissionColumns = `
	id, idempotency_reference, transaction_hash, valid_until, source_account,
	domain_action, state, attempt, outcome_detail, created_at, submitted_at,
	resolved_at`

// Claim durably records the intent, or returns the existing one.
//
// INSERT ... ON CONFLICT DO NOTHING is the atomic gate — the same mechanism
// the HTTP idempotency middleware uses (nester#835), and for the same reason:
// it is correct across instances, where a check-then-insert is not.
func (s *PostgresSubmissionStore) Claim(ctx context.Context, intent SubmissionIntent) (SubmissionIntent, bool, error) {
	if intent.IdempotencyReference == "" {
		return SubmissionIntent{}, false, errors.New("submission intent requires an idempotency reference")
	}

	created, err := s.insertIntent(ctx, intent)
	if err != nil {
		return SubmissionIntent{}, false, err
	}
	if created != nil {
		return *created, true, nil
	}

	// The reference already exists. Return the original rather than
	// submitting a second time — this is what makes a duplicate request a
	// no-op rather than a double-spend.
	existing, err := s.getByReference(ctx, intent.IdempotencyReference)
	if err != nil {
		return SubmissionIntent{}, false, err
	}

	// The reference was created for a different transaction. Neither reusing
	// the original nor creating a second is honest, so refuse — mirroring the
	// 409 the HTTP idempotency middleware returns on a fingerprint mismatch.
	if existing.TransactionHash != intent.TransactionHash {
		return SubmissionIntent{}, false, fmt.Errorf("%w: reference %q",
			ErrReferenceReused, intent.IdempotencyReference)
	}

	return existing, false, nil
}

// insertIntent returns the created intent, or nil when the reference already
// existed.
func (s *PostgresSubmissionStore) insertIntent(ctx context.Context, intent SubmissionIntent) (*SubmissionIntent, error) {
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = time.Now().UTC()
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO submission_intents
    (idempotency_reference, transaction_hash, valid_until, source_account,
     domain_action, state, created_at, next_reconcile_at)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6)
ON CONFLICT (idempotency_reference) DO NOTHING
RETURNING `+submissionColumns,
		intent.IdempotencyReference,
		intent.TransactionHash,
		intent.ValidUntil,
		intent.SourceAccount,
		intent.DomainAction,
		intent.CreatedAt,
	)

	created, err := scanIntent(row)
	if errors.Is(err, sql.ErrNoRows) {
		// ON CONFLICT DO NOTHING returned no row: someone else holds it.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert submission intent: %w", err)
	}
	return &created, nil
}

func (s *PostgresSubmissionStore) getByReference(ctx context.Context, reference string) (SubmissionIntent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+submissionColumns+` FROM submission_intents WHERE idempotency_reference = $1`,
		reference,
	)

	intent, err := scanIntent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SubmissionIntent{}, ErrIntentNotFound
	}
	if err != nil {
		return SubmissionIntent{}, fmt.Errorf("load submission intent: %w", err)
	}
	return intent, nil
}

// MarkSubmitted records that the envelope was handed to the RPC. It says
// nothing about the outcome, and deliberately does not change state: the
// submission stays pending until the chain decides.
func (s *PostgresSubmissionStore) MarkSubmitted(ctx context.Context, id string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE submission_intents
SET submitted_at = COALESCE(submitted_at, $2),
    attempt      = attempt + 1
WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("mark submitted: %w", err)
	}
	return requireAffected(result, ErrIntentNotFound)
}

// Resolve moves a submission to a terminal state.
//
// The `state = 'pending'` predicate is the guard that makes a late or
// concurrent reconciliation harmless: a submission that has already settled
// cannot be reopened, and one outcome can never overwrite another. The caller
// sees ErrIntentNotFound, which the reconciler treats as "already handled".
func (s *PostgresSubmissionStore) Resolve(ctx context.Context, id string, state SubmissionState, detail string, at time.Time) error {
	if state == SubmissionPending {
		return errors.New("resolve requires a terminal state")
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE submission_intents
SET state              = $2,
    outcome_detail     = $3,
    resolved_at        = $4,
    last_reconciled_at = $4,
    reconcile_attempts = reconcile_attempts + 1
WHERE id = $1 AND state = 'pending'`, id, string(state), detail, at)
	if err != nil {
		return fmt.Errorf("resolve submission: %w", err)
	}
	return requireAffected(result, ErrIntentNotFound)
}

// Get returns one intent by ID.
func (s *PostgresSubmissionStore) Get(ctx context.Context, id string) (SubmissionIntent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+submissionColumns+` FROM submission_intents WHERE id = $1`, id)

	intent, err := scanIntent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SubmissionIntent{}, ErrIntentNotFound
	}
	if err != nil {
		return SubmissionIntent{}, fmt.Errorf("load submission intent: %w", err)
	}
	return intent, nil
}

// ClaimPendingForReconcile takes a batch of due submissions for this worker.
//
// SKIP LOCKED means a second reconciler running concurrently takes a
// different batch rather than blocking or duplicating work. Pushing
// next_reconcile_at forward in the same transaction means a submission the
// chain has not yet decided is not re-checked immediately.
func (s *PostgresSubmissionStore) ClaimPendingForReconcile(ctx context.Context, limit int, now time.Time) ([]SubmissionIntent, error) {
	if limit <= 0 {
		limit = DefaultReconcileBatch
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The only concatenated fragment is prefixColumns, which builds a fixed
	// column list from a compile-time literal alias; every value is bound
	// through $1/$2/$3. No caller input reaches the statement text.
	// #nosec G202 -- column list is a constant, values are parameterised.
	rows, err := tx.QueryContext(ctx, `
WITH due AS (
    SELECT id
    FROM submission_intents
    WHERE state = 'pending' AND next_reconcile_at <= $1
    ORDER BY next_reconcile_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE submission_intents s
SET next_reconcile_at = $3
FROM due
WHERE s.id = due.id
RETURNING `+prefixColumns("s")+`
`, now, limit, now.Add(s.reconcileBackoff))
	if err != nil {
		return nil, fmt.Errorf("claim pending submissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var intents []SubmissionIntent
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan submission intent: %w", err)
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return intents, nil
}

// prefixColumns qualifies the shared column list for a joined UPDATE.
func prefixColumns(alias string) string {
	return alias + `.id, ` + alias + `.idempotency_reference, ` + alias + `.transaction_hash, ` +
		alias + `.valid_until, ` + alias + `.source_account, ` + alias + `.domain_action, ` +
		alias + `.state, ` + alias + `.attempt, ` + alias + `.outcome_detail, ` +
		alias + `.created_at, ` + alias + `.submitted_at, ` + alias + `.resolved_at`
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntent(row rowScanner) (SubmissionIntent, error) {
	var intent SubmissionIntent
	err := row.Scan(
		&intent.ID,
		&intent.IdempotencyReference,
		&intent.TransactionHash,
		&intent.ValidUntil,
		&intent.SourceAccount,
		&intent.DomainAction,
		&intent.State,
		&intent.Attempt,
		&intent.OutcomeDetail,
		&intent.CreatedAt,
		&intent.SubmittedAt,
		&intent.ResolvedAt,
	)
	return intent, err
}

func requireAffected(result sql.Result, notFound error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound
	}
	return nil
}

var _ SubmissionStore = (*PostgresSubmissionStore)(nil)
