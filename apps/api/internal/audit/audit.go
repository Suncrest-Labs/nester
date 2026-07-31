package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Entry represents a single audit log entry in the database.
type Entry struct {
	ID           uuid.UUID       `json:"id"`
	UserID       *uuid.UUID      `json:"user_id,omitempty"`
	Action       string          `json:"action"`
	EntityType   string          `json:"entity_type"`
	EntityID     uuid.UUID       `json:"entity_id"`
	OldValue     json.RawMessage `json:"old_value,omitempty"`
	NewValue     json.RawMessage `json:"new_value,omitempty"`
	IPAddress    *string         `json:"ip_address,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`

	// Tamper-evident chain fields
	Sequence     int64           `json:"sequence"`
	Actor        string          `json:"actor"`
	Target       string          `json:"target"`
	Detail       json.RawMessage `json:"detail,omitempty"`
	DetailHash   string          `json:"detail_hash"`
	PrevHash     string          `json:"prev_hash"`
	EntryHash    string          `json:"entry_hash"`
	Anchored     bool            `json:"anchored"`
	AnchorTxHash *string         `json:"anchor_tx_hash,omitempty"`
	Redacted     bool            `json:"redacted"`
}

// AuditService defines the methods for managing and verifying the tamper-evident audit log.
type AuditService interface {
	// LogAction appends a new audit log entry to the chain.
	LogAction(ctx context.Context, userID *uuid.UUID, actor, action, target string, detail any, oldValue, newValue json.RawMessage, ipAddress *string) (*Entry, error)
	// VerifyChain verifies the cryptographic integrity of the chain over the range [fromSeq, toSeq].
	VerifyChain(ctx context.Context, fromSeq, toSeq int64) (bool, int64, error)
	// RedactEntry redacts specific sensitive fields in the detail payload of an entry without breaking verifiability.
	RedactEntry(ctx context.Context, seq int64, redactKeys []string) error
	// AnchorLatestEntry anchors the latest entry hash to an external store (file or mock contract).
	AnchorLatestEntry(ctx context.Context) (string, error)
}

// Simple on-chain/file anchoring config
type AnchorConfig struct {
	FilePath        string // Path to external append-only anchor log
	ContractAddress string // If on-chain anchoring is enabled
	Enabled         bool
}

type auditService struct {
	db           *sql.DB
	mu           sync.Mutex // single-writer sequence lock
	anchorConfig AnchorConfig
}

// NewAuditService creates a new tamper-evident audit log service.
func NewAuditService(db *sql.DB, anchorCfg AnchorConfig) AuditService {
	if anchorCfg.FilePath == "" {
		anchorCfg.FilePath = "audit_anchors.log"
	}
	return &auditService{
		db:           db,
		anchorConfig: anchorCfg,
	}
}

// CanonicalSerialize generates a deterministic serialization for an entry to compute its hash.
// Format: SEQ:<seq>\nTS:<rfc3339>\nACTOR:<actor>\nACTION:<action>\nTARGET:<target>\nDETAIL_HASH:<detail_hash>\nPREV_HASH:<prev_hash>
func CanonicalSerialize(seq int64, ts time.Time, actor, action, target, detailHash, prevHash string) []byte {
	tsStr := ts.UTC().Format(time.RFC3339Nano)
	return []byte(fmt.Sprintf(
		"SEQ:%d\nTS:%s\nACTOR:%s\nACTION:%s\nTARGET:%s\nDETAIL_HASH:%s\nPREV_HASH:%s",
		seq, tsStr, actor, action, target, detailHash, prevHash,
	))
}

// HashData returns the SHA-256 hash in hex representation.
func HashData(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// CanonicalizeDetail generates a deterministic JSON representation of any detail interface.
func CanonicalizeDetail(detail any) (string, string, error) {
	if detail == nil {
		return "{}", HashData([]byte("{}")), nil
	}

	var raw json.RawMessage
	switch v := detail.(type) {
	case string:
		if v == "" {
			return "{}", HashData([]byte("{}")), nil
		}
		raw = json.RawMessage(v)
	case []byte:
		if len(v) == 0 {
			return "{}", HashData([]byte("{}")), nil
		}
		raw = json.RawMessage(v)
	case json.RawMessage:
		raw = v
	default:
		b, err := json.Marshal(detail)
		if err != nil {
			return "", "", fmt.Errorf("marshal detail: %w", err)
		}
		raw = b
	}

	// Normalize JSON by parsing to map/slice and re-marshaling (Go's json.Marshal sorts map keys)
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// If not valid JSON, wrap in a raw field
		parsed = map[string]any{"raw": string(raw)}
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize json: %w", err)
	}

	return string(normalized), HashData(normalized), nil
}

func (s *auditService) LogAction(
	ctx context.Context,
	userID *uuid.UUID,
	actor, action, target string,
	detail any,
	oldValue, newValue json.RawMessage,
	ipAddress *string,
) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Begin transaction to guarantee sequential gap-free writes
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 2. Query the latest entry to get sequence and hash (prevent gaps under lock)
	var lastSeq int64
	var lastHash string
	err = tx.QueryRowContext(ctx, `
		SELECT sequence, entry_hash
		FROM audit_logs
		ORDER BY sequence DESC
		LIMIT 1
		FOR UPDATE
	`).Scan(&lastSeq, &lastHash)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query last audit entry: %w", err)
	}

	nextSeq := int64(1)
	prevHash := ""
	if err == nil {
		nextSeq = lastSeq + 1
		prevHash = lastHash
	}

	// 3. Compute hashes
	detailJSON, detailHash, err := CanonicalizeDetail(detail)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	serialData := CanonicalSerialize(nextSeq, now, actor, action, target, detailHash, prevHash)
	entryHash := HashData(serialData)

	entry := &Entry{
		ID:         uuid.New(),
		UserID:     userID,
		Action:     action,
		EntityType: "audit",
		EntityID:   uuid.Nil, // will use entry ID or nil
		OldValue:   oldValue,
		NewValue:   newValue,
		IPAddress:  ipAddress,
		CreatedAt:  now,
		Sequence:   nextSeq,
		Actor:      actor,
		Target:     target,
		Detail:     json.RawMessage(detailJSON),
		DetailHash: detailHash,
		PrevHash:   prevHash,
		EntryHash:  entryHash,
		Anchored:   false,
		Redacted:   false,
	}
	entry.EntityID = entry.ID

	// 4. Insert entry
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, user_id, action, entity_type, entity_id, old_value, new_value, ip_address, created_at,
			sequence, actor, target, detail, detail_hash, prev_hash, entry_hash, anchored, redacted
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`,
		entry.ID, entry.UserID, entry.Action, entry.EntityType, entry.EntityID,
		entry.OldValue, entry.NewValue, entry.IPAddress, entry.CreatedAt,
		entry.Sequence, entry.Actor, entry.Target, entry.Detail, entry.DetailHash,
		entry.PrevHash, entry.EntryHash, entry.Anchored, entry.Redacted,
	)
	if err != nil {
		return nil, fmt.Errorf("insert audit log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit audit log transaction: %w", err)
	}

	return entry, nil
}

func (s *auditService) VerifyChain(ctx context.Context, fromSeq, toSeq int64) (bool, int64, error) {
	if fromSeq <= 0 {
		fromSeq = 1
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, created_at, actor, action, target, detail, detail_hash, prev_hash, entry_hash, redacted
		FROM audit_logs
		WHERE sequence >= $1 AND sequence <= $2
		ORDER BY sequence ASC
	`, fromSeq, toSeq)
	if err != nil {
		return false, 0, fmt.Errorf("query audit logs for verification: %w", err)
	}
	defer rows.Close()

	var expectedPrevHash string
	var expectedSeq int64 = fromSeq
	first := true

	for rows.Next() {
		var entry Entry
		err := rows.Scan(
			&entry.Sequence, &entry.CreatedAt, &entry.Actor, &entry.Action, &entry.Target,
			&entry.Detail, &entry.DetailHash, &entry.PrevHash, &entry.EntryHash, &entry.Redacted,
		)
		if err != nil {
			return false, 0, fmt.Errorf("scan audit log row: %w", err)
		}

		// Verify sequence is strictly sequential and gap-free
		if entry.Sequence != expectedSeq {
			return false, entry.Sequence, fmt.Errorf("chain sequence gap: expected %d, got %d", expectedSeq, entry.Sequence)
		}

		// Verify prev_hash link
		if !first {
			if entry.PrevHash != expectedPrevHash {
				return false, entry.Sequence, fmt.Errorf("prev_hash mismatch at sequence %d: expected %s, got %s", entry.Sequence, expectedPrevHash, entry.PrevHash)
			}
		} else {
			// If verifying from the middle of the chain, we assert that the row's prev_hash matches what is in DB
			// But for the very first entry (sequence = 1), prev_hash must be empty
			if entry.Sequence == 1 && entry.PrevHash != "" {
				return false, entry.Sequence, fmt.Errorf("sequence 1 must have empty prev_hash, got %s", entry.PrevHash)
			}
		}

		// If not redacted, verify detail_hash matches detail payload
		if !entry.Redacted {
			_, recomputedDetailHash, err := CanonicalizeDetail(entry.Detail)
			if err != nil {
				return false, entry.Sequence, fmt.Errorf("canonicalize detail at sequence %d: %w", entry.Sequence, err)
			}
			if recomputedDetailHash != entry.DetailHash {
				return false, entry.Sequence, fmt.Errorf("detail_hash mismatch at sequence %d: recomputed %s, stored %s", entry.Sequence, recomputedDetailHash, entry.DetailHash)
			}
		}

		// Verify recomputed entry_hash matches stored entry_hash
		serialData := CanonicalSerialize(entry.Sequence, entry.CreatedAt, entry.Actor, entry.Action, entry.Target, entry.DetailHash, entry.PrevHash)
		recomputedHash := HashData(serialData)
		if recomputedHash != entry.EntryHash {
			return false, entry.Sequence, fmt.Errorf("entry_hash mismatch at sequence %d: recomputed %s, stored %s", entry.Sequence, recomputedHash, entry.EntryHash)
		}

		expectedPrevHash = entry.EntryHash
		expectedSeq++
		first = false
	}

	if err = rows.Err(); err != nil {
		return false, 0, err
	}

	return true, 0, nil
}

func (s *auditService) RedactEntry(ctx context.Context, seq int64, redactKeys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Fetch the entry details
	var detailJSON []byte
	var detailHash string
	var redacted bool
	err = tx.QueryRowContext(ctx, `
		SELECT detail, detail_hash, redacted
		FROM audit_logs
		WHERE sequence = $1
	`, seq).Scan(&detailJSON, &detailHash, &redacted)

	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("audit log entry not found: sequence %d", seq)
		}
		return fmt.Errorf("query audit log entry: %w", err)
	}

	// 2. Parse the detail map
	var detailMap map[string]any
	if err := json.Unmarshal(detailJSON, &detailMap); err != nil {
		return fmt.Errorf("unmarshal detail for redaction: %w", err)
	}

	// 3. Redact the fields
	redactedAny := false
	for _, key := range redactKeys {
		if _, exists := detailMap[key]; exists {
			detailMap[key] = "[REDACTED]"
			redactedAny = true
		}
	}

	if !redactedAny {
		// Nothing to redact, complete successfully
		return nil
	}

	// 4. Serialize the redacted detail (keys sorted automatically by Go JSON marshal)
	newDetailBytes, err := json.Marshal(detailMap)
	if err != nil {
		return fmt.Errorf("marshal redacted detail: %w", err)
	}

	// 5. Update DB row: replace detail payload, flag as redacted, but KEEP the original detail_hash!
	_, err = tx.ExecContext(ctx, `
		UPDATE audit_logs
		SET detail = $1, redacted = TRUE
		WHERE sequence = $2
	`, json.RawMessage(newDetailBytes), seq)
	if err != nil {
		return fmt.Errorf("update redacted audit log entry: %w", err)
	}

	// 6. Log the redaction action itself as a new audit entry to make it auditable
	// Note: to avoid infinite recursion or lock deadlock, we insert it directly in this txn.
	// But wait, the audit trail of the redaction needs a sequence number!
	// It's cleaner to commit this transaction first, and then log the redaction as a new audit action.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit redaction: %w", err)
	}

	// Log the redaction action separately (acquires mu/lock and logs normally)
	_, _ = s.LogAction(ctx, nil, "system", "redaction", fmt.Sprintf("audit_logs:sequence:%d", seq), map[string]any{
		"target_sequence": seq,
		"redacted_fields": redactKeys,
	}, nil, nil, nil)

	return nil
}

func (s *auditService) AnchorLatestEntry(ctx context.Context) (string, error) {
	// Query latest entry_hash
	var seq int64
	var entryHash string
	err := s.db.QueryRowContext(ctx, `
		SELECT sequence, entry_hash
		FROM audit_logs
		ORDER BY sequence DESC
		LIMIT 1
	`).Scan(&seq, &entryHash)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("no audit log entries to anchor")
		}
		return "", fmt.Errorf("query latest audit log entry: %w", err)
	}

	// Write to external append-only log file (file anchor)
	anchorRecord := fmt.Sprintf("[%s] SEQ:%d HASH:%s\n", time.Now().UTC().Format(time.RFC3339), seq, entryHash)

	// Ensure directory exists
	dir := filepath.Dir(s.anchorConfig.FilePath)
	if dir != "." && dir != "/" {
		_ = os.MkdirAll(dir, 0755)
	}

	f, err := os.OpenFile(s.anchorConfig.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open anchor log file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(anchorRecord); err != nil {
		return "", fmt.Errorf("write to anchor log file: %w", err)
	}

	// Mark latest entry as anchored in DB
	_, err = s.db.ExecContext(ctx, `
		UPDATE audit_logs
		SET anchored = TRUE, anchor_tx_hash = $1
		WHERE sequence = $2
	`, "file:"+s.anchorConfig.FilePath, seq)
	if err != nil {
		return "", fmt.Errorf("update anchored status in DB: %w", err)
	}

	return "file:" + s.anchorConfig.FilePath, nil
}
