package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set; skipping integration tests")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setupTestTables(t *testing.T, db *sql.DB) {
	t.Helper()

	// Drop audit_logs first to start clean
	_, err := db.Exec(`DROP TABLE IF EXISTS audit_logs CASCADE`)
	if err != nil {
		t.Fatalf("failed to drop audit_logs: %v", err)
	}

	// 1. Apply 011_create_audit_logs_table.up.sql
	migration011Path := filepath.Join("..", "..", "migrations", "011_create_audit_logs_table.up.sql")
	content011, err := os.ReadFile(migration011Path)
	if err != nil {
		t.Fatalf("failed to read 011 migration: %v", err)
	}

	// Replace "user_id UUID REFERENCES users(id)" to just "user_id UUID" to avoid needing users table seed
	migration011SQL := strings.Replace(string(content011), "REFERENCES users(id) ON DELETE SET NULL", "", 1)

	_, err = db.Exec(migration011SQL)
	if err != nil {
		t.Fatalf("failed to apply 011 migration: %v", err)
	}

	// 2. Apply 097_add_tamper_evident_audit_fields.up.sql
	migration097Path := filepath.Join("..", "..", "migrations", "097_add_tamper_evident_audit_fields.up.sql")
	content097, err := os.ReadFile(migration097Path)
	if err != nil {
		t.Fatalf("failed to read 060 migration: %v", err)
	}

	_, err = db.Exec(string(content097))
	if err != nil {
		t.Fatalf("failed to apply 060 migration: %v", err)
	}
}

func TestAuditLogTamperEvidence(t *testing.T) {
	db := openTestDB(t)
	setupTestTables(t, db)

	// Clean up temporary anchor file
	anchorFile := filepath.Join(t.TempDir(), "audit_anchors.log")
	t.Cleanup(func() { _ = os.Remove(anchorFile) })

	service := NewAuditService(db, AnchorConfig{FilePath: anchorFile, Enabled: true})
	ctx := context.Background()

	userID := uuid.New()
	ipAddress := "127.0.0.1"

	// 1. Append entries and check continuity
	entry1, err := service.LogAction(ctx, &userID, "admin1", "update_role", "user_123", map[string]any{"role": "superuser"}, nil, nil, &ipAddress)
	if err != nil {
		t.Fatalf("LogAction 1 failed: %v", err)
	}
	if entry1.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", entry1.Sequence)
	}
	if entry1.PrevHash != "" {
		t.Errorf("expected sequence 1 prev_hash to be empty, got %s", entry1.PrevHash)
	}

	entry2, err := service.LogAction(ctx, nil, "system", "fund_movement", "vault_abc", map[string]any{"amount": 1000}, nil, nil, nil)
	if err != nil {
		t.Fatalf("LogAction 2 failed: %v", err)
	}
	if entry2.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", entry2.Sequence)
	}
	if entry2.PrevHash != entry1.EntryHash {
		t.Errorf("expected entry 2 prev_hash to match entry 1 entry_hash, got %s, want %s", entry2.PrevHash, entry1.EntryHash)
	}

	entry3, err := service.LogAction(ctx, &userID, "user123", "login", "auth_service", map[string]any{"success": true}, nil, nil, &ipAddress)
	if err != nil {
		t.Fatalf("LogAction 3 failed: %v", err)
	}
	if entry3.Sequence != 3 {
		t.Errorf("expected sequence 3, got %d", entry3.Sequence)
	}

	// 2. Verify clean chain
	ok, brokenSeq, err := service.VerifyChain(ctx, 1, 3)
	if err != nil {
		t.Fatalf("VerifyChain failed: %v", err)
	}
	if !ok {
		t.Errorf("expected chain to be valid, but verify failed at sequence %d", brokenSeq)
	}

	// 3. Test tamper detection (modify entry 2 details in database)
	_, err = db.ExecContext(ctx, "UPDATE audit_logs SET detail = '{\"amount\": 9999}' WHERE sequence = 2")
	if err != nil {
		t.Fatalf("failed to tamper entry: %v", err)
	}

	ok, brokenSeq, err = service.VerifyChain(ctx, 1, 3)
	if err == nil && ok {
		t.Errorf("expected verify to fail, but it succeeded")
	}
	if brokenSeq != 2 {
		t.Errorf("expected break to be reported at sequence 2, got %d (err: %v)", brokenSeq, err)
	}

	// Restore original state
	_, err = db.ExecContext(ctx, "UPDATE audit_logs SET detail = $1 WHERE sequence = 2", entry2.Detail)
	if err != nil {
		t.Fatalf("failed to restore entry: %v", err)
	}

	// Check that it's verified again
	ok, _, err = service.VerifyChain(ctx, 1, 3)
	if !ok || err != nil {
		t.Fatalf("failed to verify restored chain: %v", err)
	}

	// 4. Test gap detection (delete entry 2)
	_, err = db.ExecContext(ctx, "DELETE FROM audit_logs WHERE sequence = 2")
	if err != nil {
		t.Fatalf("failed to delete entry: %v", err)
	}

	ok, brokenSeq, err = service.VerifyChain(ctx, 1, 3)
	if ok || err == nil {
		t.Errorf("expected verify to fail due to sequence gap")
	}
	// Verify reports break at 3 because sequence sequence goes 1 -> 3
	if brokenSeq != 3 {
		t.Errorf("expected break to be reported at sequence 3 (due to sequence gap), got %d (err: %v)", brokenSeq, err)
	}
}

func TestAuditRedaction(t *testing.T) {
	db := openTestDB(t)
	setupTestTables(t, db)

	service := NewAuditService(db, AnchorConfig{FilePath: filepath.Join(t.TempDir(), "audit_anchors.log"), Enabled: true})
	ctx := context.Background()

	// Append entry with sensitive data
	entry, err := service.LogAction(ctx, nil, "system", "auth_event", "auth", map[string]any{
		"token":     "sensitive_jwt_token",
		"device_id": "phone123",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Verify before redaction
	ok, _, err := service.VerifyChain(ctx, 1, 1)
	if !ok || err != nil {
		t.Fatalf("VerifyChain before redaction failed: %v", err)
	}

	// Redact the "token" field
	err = service.RedactEntry(ctx, entry.Sequence, []string{"token"})
	if err != nil {
		t.Fatalf("RedactEntry failed: %v", err)
	}

	// Query entry to check that the token is redacted
	var detailJSON []byte
	var redacted bool
	err = db.QueryRowContext(ctx, "SELECT detail, redacted FROM audit_logs WHERE sequence = $1", entry.Sequence).Scan(&detailJSON, &redacted)
	if err != nil {
		t.Fatalf("query redacted entry failed: %v", err)
	}

	if !redacted {
		t.Errorf("expected redacted flag to be true")
	}

	var detailMap map[string]any
	if err := json.Unmarshal(detailJSON, &detailMap); err != nil {
		t.Fatalf("unmarshal detail failed: %v", err)
	}

	if detailMap["token"] != "[REDACTED]" {
		t.Errorf("expected token to be [REDACTED], got %v", detailMap["token"])
	}
	if detailMap["device_id"] != "phone123" {
		t.Errorf("expected device_id to be preserved, got %v", detailMap["device_id"])
	}

	// Verify that the chain still verifies!
	ok, brokenSeq, err := service.VerifyChain(ctx, 1, 2) // includes the redaction log entry itself
	if !ok || err != nil {
		t.Errorf("VerifyChain failed after redaction (broken at seq %d): %v", brokenSeq, err)
	}
}

func TestAuditAnchoring(t *testing.T) {
	db := openTestDB(t)
	setupTestTables(t, db)

	anchorFile := filepath.Join(t.TempDir(), "audit_anchors.log")
	service := NewAuditService(db, AnchorConfig{FilePath: anchorFile, Enabled: true})
	ctx := context.Background()

	// Append entry
	entry, err := service.LogAction(ctx, nil, "system", "auth_event", "auth", map[string]any{"data": "val"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Anchor
	txHash, err := service.AnchorLatestEntry(ctx)
	if err != nil {
		t.Fatalf("AnchorLatestEntry failed: %v", err)
	}

	if !strings.HasPrefix(txHash, "file:") {
		t.Errorf("expected anchor tx hash to have file prefix, got %s", txHash)
	}

	// Check anchored flag in DB
	var anchored bool
	var dbTxHash string
	err = db.QueryRowContext(ctx, "SELECT anchored, anchor_tx_hash FROM audit_logs WHERE sequence = $1", entry.Sequence).Scan(&anchored, &dbTxHash)
	if err != nil {
		t.Fatalf("query audit_logs failed: %v", err)
	}

	if !anchored {
		t.Errorf("expected anchored to be true")
	}
	if dbTxHash != txHash {
		t.Errorf("expected anchor_tx_hash %s, got %s", txHash, dbTxHash)
	}

	// Verify contents of the anchor file
	data, err := os.ReadFile(anchorFile)
	if err != nil {
		t.Fatalf("failed to read anchor file: %v", err)
	}

	fileContent := string(data)
	if !strings.Contains(fileContent, "SEQ:1") || !strings.Contains(fileContent, entry.EntryHash) {
		t.Errorf("expected anchor file to contain seq 1 and hash %s, got %s", entry.EntryHash, fileContent)
	}
}
