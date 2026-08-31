package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// A malformed identifier must surface as an error rather than being coerced to
// the zero UUID and used as a real identifier (nester#1197). Each affected
// method is exercised separately: silently returning uuid.Nil in a domain
// object is what let a bad value reach a WHERE clause or an ownership check.

func TestBankAccountRepository_ListByUser_RejectsMalformedID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	userID := uuid.New()
	cols := []string{
		"id", "user_id", "bank_name", "bank_code", "account_last4",
		"account_name", "currency", "country", "is_default", "verified_at", "created_at",
	}
	mock.ExpectQuery("SELECT id, user_id, bank_name").
		WithArgs(userID.String()).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(
			"not-a-uuid", userID.String(), "Kuda", "090267", "1234",
			"Ada Lovelace", "NGN", "NG", true, nil, time.Now(),
		))

	repo := NewBankAccountRepository(db)
	accounts, err := repo.ListByUser(context.Background(), userID)
	if err == nil {
		t.Fatalf("ListByUser: expected an error for a malformed id, got accounts=%v", accounts)
	}
	if accounts != nil {
		t.Fatalf("ListByUser: expected no accounts on a parse failure, got %v", accounts)
	}
	if !strings.Contains(err.Error(), "parse id") {
		t.Fatalf("ListByUser: expected a parse error, got %v", err)
	}
}

func TestBankAccountRepository_ListByUser_RejectsMalformedUserID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	userID := uuid.New()
	cols := []string{
		"id", "user_id", "bank_name", "bank_code", "account_last4",
		"account_name", "currency", "country", "is_default", "verified_at", "created_at",
	}
	mock.ExpectQuery("SELECT id, user_id, bank_name").
		WithArgs(userID.String()).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(
			uuid.New().String(), "", "Kuda", "090267", "1234",
			"Ada Lovelace", "NGN", "NG", true, nil, time.Now(),
		))

	repo := NewBankAccountRepository(db)
	if _, err := repo.ListByUser(context.Background(), userID); err == nil {
		t.Fatal("ListByUser: expected an error for a malformed user id")
	} else if !strings.Contains(err.Error(), "parse user id") {
		t.Fatalf("ListByUser: expected a user id parse error, got %v", err)
	}
}

func TestBankAccountRepository_GetByID_RejectsMalformedUserID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	id := uuid.New()
	cols := []string{
		"user_id", "bank_name", "bank_code", "account_number_encrypted",
		"account_name", "currency", "country", "is_default", "verified_at",
		"created_at", "key_version",
	}
	mock.ExpectQuery("SELECT user_id, bank_name").
		WithArgs(id.String()).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(
			"not-a-uuid", "Kuda", "090267", []byte("cipher"),
			"Ada Lovelace", "NGN", "NG", true, nil, time.Now(), "v1",
		))

	repo := NewBankAccountRepository(db)
	account, _, _, err := repo.GetByID(context.Background(), id)
	if err == nil {
		t.Fatal("GetByID: expected an error for a malformed user id")
	}
	if !strings.Contains(err.Error(), "parse user id") {
		t.Fatalf("GetByID: expected a user id parse error, got %v", err)
	}
	// The caller must not receive an account whose owner is the zero UUID: an
	// ownership check against uuid.Nil is reachable deterministically.
	if account.UserID != uuid.Nil || account.ID != uuid.Nil {
		t.Fatalf("GetByID: expected a zero-valued account on failure, got %+v", account)
	}
}

func TestBankAccountRepository_ScanPending_RejectsMalformedID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id, account_number_encrypted, key_version").
		WithArgs("v2", 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "account_number_encrypted", "key_version"}).
			AddRow("not-a-uuid", []byte("cipher"), "v1"))

	repo := NewBankAccountRepository(db)
	rows, err := repo.ScanPending(context.Background(), "v2", 10)
	if err == nil {
		t.Fatalf("ScanPending: expected an error for a malformed id, got rows=%v", rows)
	}
	if !strings.Contains(err.Error(), "parse id") {
		t.Fatalf("ScanPending: expected a parse error, got %v", err)
	}
}
