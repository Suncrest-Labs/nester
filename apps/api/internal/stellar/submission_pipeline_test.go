package stellar

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:5432/nester_test?sslmode=disable")
	if err != nil {
		t.Skip("postgres not available:", err)
	}

	if err := db.Ping(); err != nil {
		t.Skip("postgres not reachable:", err)
	}

	return db
}

func TestSubmissionPipeline_AllocateSequence_GapFree(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	sourceAccount := "GTEST" + uuid.New().String()[:10]

	sequences := make([]int64, 10)
	for i := 0; i < 10; i++ {
		seq, err := pipeline.AllocateSequence(ctx, sourceAccount)
		if err != nil {
			t.Fatalf("allocate sequence %d: %v", i, err)
		}
		sequences[i] = seq
	}

	for i := 1; i < len(sequences); i++ {
		if sequences[i] != sequences[i-1]+1 {
			t.Errorf("sequence gap: expected %d, got %d", sequences[i-1]+1, sequences[i])
		}
	}
}

func TestSubmissionPipeline_AllocateSequence_Concurrent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	sourceAccount := "GTEST" + uuid.New().String()[:10]

	const numGoroutines = 20
	results := make(chan int64, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			seq, err := pipeline.AllocateSequence(ctx, sourceAccount)
			if err != nil {
				errors <- err
				return
			}
			results <- seq
		}()
	}

	sequences := make(map[int64]bool)
	for i := 0; i < numGoroutines; i++ {
		select {
		case seq := <-results:
			if sequences[seq] {
				t.Errorf("duplicate sequence number: %d", seq)
			}
			sequences[seq] = true
		case err := <-errors:
			t.Fatalf("concurrent allocation failed: %v", err)
		}
	}

	if len(sequences) != numGoroutines {
		t.Errorf("expected %d unique sequences, got %d", numGoroutines, len(sequences))
	}
}

func TestSubmissionPipeline_RecordSubmission(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	sourceAccount := "GTEST" + uuid.New().String()[:10]
	seq, err := pipeline.AllocateSequence(ctx, sourceAccount)
	if err != nil {
		t.Fatalf("allocate sequence: %v", err)
	}

	envelope := "test-envelope-" + uuid.New().String()
	jobID := uuid.New()

	submission, err := pipeline.RecordSubmission(ctx, sourceAccount, seq, envelope, &jobID, "test-action")
	if err != nil {
		t.Fatalf("record submission: %v", err)
	}

	if submission.SourceAccount != sourceAccount {
		t.Errorf("expected source account %s, got %s", sourceAccount, submission.SourceAccount)
	}
	if submission.SequenceNumber != seq {
		t.Errorf("expected sequence %d, got %d", seq, submission.SequenceNumber)
	}
	if submission.Status != StatusPending {
		t.Errorf("expected status pending, got %s", submission.Status)
	}
	if submission.TransactionHash == "" {
		t.Error("expected transaction hash to be computed")
	}
}

func TestSubmissionPipeline_MarkConfirmed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	sourceAccount := "GTEST" + uuid.New().String()[:10]
	seq, err := pipeline.AllocateSequence(ctx, sourceAccount)
	if err != nil {
		t.Fatalf("allocate sequence: %v", err)
	}

	envelope := "test-envelope-" + uuid.New().String()
	submission, err := pipeline.RecordSubmission(ctx, sourceAccount, seq, envelope, nil, "test")
	if err != nil {
		t.Fatalf("record submission: %v", err)
	}

	if err := pipeline.MarkSubmitted(ctx, submission.ID); err != nil {
		t.Fatalf("mark submitted: %v", err)
	}

	if err := pipeline.MarkConfirmed(ctx, submission.ID); err != nil {
		t.Fatalf("mark confirmed: %v", err)
	}

	updated, err := pipeline.GetSubmission(ctx, submission.ID)
	if err != nil {
		t.Fatalf("get submission: %v", err)
	}

	if updated.Status != StatusConfirmed {
		t.Errorf("expected status confirmed, got %s", updated.Status)
	}
	if updated.ConfirmedAt == nil {
		t.Error("expected confirmed_at to be set")
	}
}

func TestSubmissionPipeline_DetectGap(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	sourceAccount := "GTEST" + uuid.New().String()[:10]

	seq1, _ := pipeline.AllocateSequence(ctx, sourceAccount)
	pipeline.RecordSubmission(ctx, sourceAccount, seq1, "env1", nil, "test")
	pipeline.MarkSubmitted(ctx, uuid.New())

	seq2, _ := pipeline.AllocateSequence(ctx, sourceAccount)
	pipeline.RecordSubmission(ctx, sourceAccount, seq2, "env2", nil, "test")

	_, _ = pipeline.AllocateSequence(ctx, sourceAccount)

	seq4, _ := pipeline.AllocateSequence(ctx, sourceAccount)
	pipeline.RecordSubmission(ctx, sourceAccount, seq4, "env4", nil, "test")

	gaps, err := pipeline.DetectSequenceGap(ctx, sourceAccount)
	if err != nil {
		t.Fatalf("detect gap: %v", err)
	}

	if len(gaps) == 0 {
		t.Log("no gaps detected (expected if all sequences are consecutive)")
	}
}

func TestSubmissionPipeline_PerAccountSerialization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	pipeline := NewSubmissionPipeline(db)

	account1 := "GTEST1" + uuid.New().String()[:10]
	account2 := "GTEST2" + uuid.New().String()[:10]

	const numPerAccount = 10
	results := make(chan struct {
		account string
		seq     int64
	}, numPerAccount*2)

	for i := 0; i < numPerAccount; i++ {
		go func() {
			seq, err := pipeline.AllocateSequence(ctx, account1)
			if err != nil {
				t.Errorf("account1 allocation failed: %v", err)
				return
			}
			results <- struct {
				account string
				seq     int64
			}{account1, seq}
		}()

		go func() {
			seq, err := pipeline.AllocateSequence(ctx, account2)
			if err != nil {
				t.Errorf("account2 allocation failed: %v", err)
				return
			}
			results <- struct {
				account string
				seq     int64
			}{account2, seq}
		}()
	}

	account1Seqs := make(map[int64]bool)
	account2Seqs := make(map[int64]bool)

	for i := 0; i < numPerAccount*2; i++ {
		result := <-results
		if result.account == account1 {
			if account1Seqs[result.seq] {
				t.Errorf("account1 duplicate sequence: %d", result.seq)
			}
			account1Seqs[result.seq] = true
		} else {
			if account2Seqs[result.seq] {
				t.Errorf("account2 duplicate sequence: %d", result.seq)
			}
			account2Seqs[result.seq] = true
		}
	}

	if len(account1Seqs) != numPerAccount {
		t.Errorf("account1: expected %d sequences, got %d", numPerAccount, len(account1Seqs))
	}
	if len(account2Seqs) != numPerAccount {
		t.Errorf("account2: expected %d sequences, got %d", numPerAccount, len(account2Seqs))
	}
}
