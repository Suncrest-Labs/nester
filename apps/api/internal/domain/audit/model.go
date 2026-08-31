// Package audit holds the plain data type for an audit-log entry, kept
// dependency-free so both the service layer (which produces entries) and the
// postgres repository layer (which persists them) can depend on it without
// repository -> service becoming a compile-time import cycle.
package audit

import "github.com/google/uuid"

// Entry is a single row destined for the audit_logs table (migration 011).
type Entry struct {
	UserID     *uuid.UUID
	Action     string
	EntityType string
	EntityID   uuid.UUID
	OldValue   any
	NewValue   any
	IPAddress  string
}
