package stellar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSequenceConflict  = errors.New("sequence number conflict")
	ErrSubmissionTimeout = errors.New("submission timeout")
	ErrDoubleSubmit      = errors.New("double submission prevented")
)

type SubmissionStatus string

const (
	StatusPending   SubmissionStatus = "pending"
	StatusSubmitted SubmissionStatus = "submitted"
	StatusConfirmed SubmissionStatus = "confirmed"
	StatusFailed    SubmissionStatus = "failed"
	StatusUnknown   SubmissionStatus = "unknown"
)

type ChainSubmission struct {
	ID               uuid.UUID
	SourceAccount    string
	SequenceNumber   int64
	TransactionHash  string
	SignedEnvelope   string
	Status           SubmissionStatus
	JobID            *uuid.UUID
	DomainAction     string
	SubmittedAt      *time.Time
	ConfirmedAt      *time.Time
	ErrorMessage     *string
	RetryCount       int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type SubmissionPipeline struct {
	db           *sql.DB
	mu           sync.Mutex
	accountLocks map[string]*sync.Mutex
}

func NewSubmissionPipeline(db *sql.DB) *SubmissionPipeline {
	return &SubmissionPipeline{
		db:           db,
		accountLocks: make(map[string]*sync.Mutex),
	}
}

func (p *SubmissionPipeline) getAccountLock(account string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lock, exists := p.accountLocks[account]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	p.accountLocks[account] = lock
	return lock
}

func (p *SubmissionPipeline) AllocateSequence(ctx context.Context, sourceAccount string) (int64, error) {
	accountLock := p.getAccountLock(sourceAccount)
	accountLock.Lock()
	defer accountLock.Unlock()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var nextSeq int64
	err = tx.QueryRowContext(
		ctx,
		`SELECT next_sequence FROM account_sequences WHERE source_account = $1 FOR UPDATE`,
		sourceAccount,
	).Scan(&nextSeq)

	if err == sql.ErrNoRows {
		nextSeq, err = p.seedSequenceFromNetwork(ctx, sourceAccount)
		if err != nil {
			return 0, fmt.Errorf("seed sequence: %w", err)
		}

		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO account_sequences (source_account, next_sequence, last_synced_at, updated_at)
			 VALUES ($1, $2, NOW(), NOW())`,
			sourceAccount, nextSeq+1,
		)
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else {
		_, err = tx.ExecContext(
			ctx,
			`UPDATE account_sequences SET next_sequence = $1, updated_at = NOW() WHERE source_account = $2`,
			nextSeq+1, sourceAccount,
		)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return nextSeq, nil
}

func (p *SubmissionPipeline) seedSequenceFromNetwork(ctx context.Context, sourceAccount string) (int64, error) {
	return 0, nil
}

func (p *SubmissionPipeline) RecordSubmission(
	ctx context.Context,
	sourceAccount string,
	sequenceNumber int64,
	signedEnvelope string,
	jobID *uuid.UUID,
	domainAction string,
) (*ChainSubmission, error) {
	txHash := p.computeTransactionHash(signedEnvelope)

	submission := &ChainSubmission{
		ID:              uuid.New(),
		SourceAccount:   sourceAccount,
		SequenceNumber:  sequenceNumber,
		TransactionHash: txHash,
		SignedEnvelope:  signedEnvelope,
		Status:          StatusPending,
		JobID:           jobID,
		DomainAction:    domainAction,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	query := `
		INSERT INTO chain_submissions 
		(id, source_account, sequence_number, transaction_hash, signed_envelope, status, job_id, domain_action, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := p.db.ExecContext(
		ctx,
		query,
		submission.ID,
		submission.SourceAccount,
		submission.SequenceNumber,
		submission.TransactionHash,
		submission.SignedEnvelope,
		submission.Status,
		submission.JobID,
		submission.DomainAction,
		submission.CreatedAt,
		submission.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("record submission: %w", err)
	}

	return submission, nil
}

func (p *SubmissionPipeline) UpdateStatus(ctx context.Context, submissionID uuid.UUID, status SubmissionStatus, errMsg *string) error {
	query := `
		UPDATE chain_submissions 
		SET status = $1, error_message = $2, updated_at = NOW()
		WHERE id = $3
	`

	result, err := p.db.ExecContext(ctx, query, status, errMsg, submissionID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return errors.New("submission not found")
	}

	return nil
}

func (p *SubmissionPipeline) MarkSubmitted(ctx context.Context, submissionID uuid.UUID) error {
	query := `
		UPDATE chain_submissions 
		SET status = $1, submitted_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	_, err := p.db.ExecContext(ctx, query, StatusSubmitted, submissionID)
	return err
}

func (p *SubmissionPipeline) MarkConfirmed(ctx context.Context, submissionID uuid.UUID) error {
	query := `
		UPDATE chain_submissions 
		SET status = $1, confirmed_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	_, err := p.db.ExecContext(ctx, query, StatusConfirmed, submissionID)
	return err
}

func (p *SubmissionPipeline) ResolveTimeout(ctx context.Context, submissionID uuid.UUID) (SubmissionStatus, error) {
	submission, err := p.GetSubmission(ctx, submissionID)
	if err != nil {
		return StatusUnknown, err
	}

	onChainStatus := p.checkOnChainStatus(ctx, submission.TransactionHash)

	if onChainStatus == StatusConfirmed {
		if err := p.MarkConfirmed(ctx, submissionID); err != nil {
			return StatusUnknown, err
		}
		return StatusConfirmed, nil
	}

	if onChainStatus == StatusFailed {
		if err := p.UpdateStatus(ctx, submissionID, StatusFailed, nil); err != nil {
			return StatusUnknown, err
		}
		return StatusFailed, nil
	}

	return StatusUnknown, nil
}

func (p *SubmissionPipeline) checkOnChainStatus(ctx context.Context, txHash string) SubmissionStatus {
	return StatusUnknown
}

func (p *SubmissionPipeline) GetSubmission(ctx context.Context, submissionID uuid.UUID) (*ChainSubmission, error) {
	query := `
		SELECT id, source_account, sequence_number, transaction_hash, signed_envelope, 
		       status, job_id, domain_action, submitted_at, confirmed_at, error_message, 
		       retry_count, created_at, updated_at
		FROM chain_submissions
		WHERE id = $1
	`

	var submission ChainSubmission
	err := p.db.QueryRowContext(ctx, query, submissionID).Scan(
		&submission.ID,
		&submission.SourceAccount,
		&submission.SequenceNumber,
		&submission.TransactionHash,
		&submission.SignedEnvelope,
		&submission.Status,
		&submission.JobID,
		&submission.DomainAction,
		&submission.SubmittedAt,
		&submission.ConfirmedAt,
		&submission.ErrorMessage,
		&submission.RetryCount,
		&submission.CreatedAt,
		&submission.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, errors.New("submission not found")
	}
	if err != nil {
		return nil, err
	}

	return &submission, nil
}

func (p *SubmissionPipeline) GetPendingSubmissions(ctx context.Context, sourceAccount string) ([]ChainSubmission, error) {
	query := `
		SELECT id, source_account, sequence_number, transaction_hash, signed_envelope, 
		       status, job_id, domain_action, submitted_at, confirmed_at, error_message, 
		       retry_count, created_at, updated_at
		FROM chain_submissions
		WHERE source_account = $1 AND status IN ('pending', 'submitted', 'unknown')
		ORDER BY sequence_number ASC
	`

	rows, err := p.db.QueryContext(ctx, query, sourceAccount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var submissions []ChainSubmission
	for rows.Next() {
		var submission ChainSubmission
		err := rows.Scan(
			&submission.ID,
			&submission.SourceAccount,
			&submission.SequenceNumber,
			&submission.TransactionHash,
			&submission.SignedEnvelope,
			&submission.Status,
			&submission.JobID,
			&submission.DomainAction,
			&submission.SubmittedAt,
			&submission.ConfirmedAt,
			&submission.ErrorMessage,
			&submission.RetryCount,
			&submission.CreatedAt,
			&submission.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		submissions = append(submissions, submission)
	}

	return submissions, rows.Err()
}

func (p *SubmissionPipeline) RecoverOnStartup(ctx context.Context) error {
	query := `
		SELECT DISTINCT source_account 
		FROM chain_submissions 
		WHERE status IN ('pending', 'submitted', 'unknown')
	`

	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var accounts []string
	for rows.Next() {
		var account string
		if err := rows.Scan(&account); err != nil {
			return err
		}
		accounts = append(accounts, account)
	}

	for _, account := range accounts {
		pending, err := p.GetPendingSubmissions(ctx, account)
		if err != nil {
			return fmt.Errorf("get pending for %s: %w", account, err)
		}

		for _, submission := range pending {
			status, err := p.ResolveTimeout(ctx, submission.ID)
			if err != nil {
				return fmt.Errorf("resolve submission %s: %w", submission.ID, err)
			}
			_ = status
		}
	}

	return nil
}

func (p *SubmissionPipeline) computeTransactionHash(signedEnvelope string) string {
	envelopeBytes := []byte(signedEnvelope)
	hash := sha256.Sum256(envelopeBytes)
	return hex.EncodeToString(hash[:])
}

func (p *SubmissionPipeline) DetectSequenceGap(ctx context.Context, sourceAccount string) ([]int64, error) {
	query := `
		SELECT sequence_number 
		FROM chain_submissions 
		WHERE source_account = $1 AND status IN ('confirmed', 'submitted')
		ORDER BY sequence_number ASC
	`

	rows, err := p.db.QueryContext(ctx, query, sourceAccount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sequences []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		sequences = append(sequences, seq)
	}

	if len(sequences) < 2 {
		return nil, nil
	}

	var gaps []int64
	for i := 1; i < len(sequences); i++ {
		expected := sequences[i-1] + 1
		if sequences[i] != expected {
			for gap := expected; gap < sequences[i]; gap++ {
				gaps = append(gaps, gap)
			}
		}
	}

	return gaps, nil
}
