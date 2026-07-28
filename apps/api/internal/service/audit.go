package service

import (
	"context"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/audit"
)

// AuditEntry is a single row destined for the audit_logs table (migration
// 011). Kept narrow — this is a hook point for the companion audit-log
// issue, not a general audit framework. It's an alias of domain/audit.Entry
// (not a new type) so postgres.PostgresAuditLogger can implement AuditLogger
// by depending only on the domain package, keeping repository -> service
// out of the import graph.
type AuditEntry = audit.Entry

type AuditLogger interface {
	Log(ctx context.Context, entry AuditEntry) error
}

// NoopAuditLogger discards audit entries. Used only if a Postgres connection
// isn't available to wire the real logger.
type NoopAuditLogger struct{}

func (NoopAuditLogger) Log(context.Context, AuditEntry) error { return nil }
