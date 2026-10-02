package postgres

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/audit"
)

// TestPostgresAuditLoggerLog_WritesCorrelationID covers nester#1339: the
// insert must include correlation_id (migration 123) alongside the
// pre-existing columns, and a non-empty CorrelationID must reach the
// database as itself, not silently dropped.
func TestPostgresAuditLoggerLog_WritesCorrelationID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	logger := NewPostgresAuditLogger(db)
	entityID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO audit_logs (user_id, action, entity_type, entity_id, old_value, new_value, ip_address, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)).
		WithArgs(nil, "data_retention.delete", "activity_events", entityID, []byte("null"), []byte("null"), nil, "corr-abc-123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = logger.Log(context.Background(), audit.Entry{
		Action:        "data_retention.delete",
		EntityType:    "activity_events",
		EntityID:      entityID,
		CorrelationID: "corr-abc-123",
	})
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// TestPostgresAuditLoggerLog_EmptyCorrelationIDWritesNull confirms an entry
// with no correlation id (e.g. the existing request-triggered call sites
// that don't set one) still writes NULL rather than an empty string, so
// idx_audit_logs_correlation_id's partial index (WHERE correlation_id IS
// NOT NULL) actually excludes it.
func TestPostgresAuditLoggerLog_EmptyCorrelationIDWritesNull(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	logger := NewPostgresAuditLogger(db)
	entityID := uuid.New()

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO audit_logs (user_id, action, entity_type, entity_id, old_value, new_value, ip_address, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)).
		WithArgs(nil, "session.created", "session", entityID, []byte("null"), []byte("null"), nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = logger.Log(context.Background(), audit.Entry{
		Action:     "session.created",
		EntityType: "session",
		EntityID:   entityID,
	})
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
