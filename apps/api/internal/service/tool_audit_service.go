package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/toolaudit"
)

// ToolAuditRepository persists a chained invocation atomically: it must read
// the caller's latest hash, compute the new entry's hash, and insert it as
// one indivisible operation (e.g. under a per-user advisory lock inside a
// transaction) so concurrent invocations for the same user can't both read
// the same prevHash and silently fork the tamper-evident chain.
type ToolAuditRepository interface {
	InsertChained(ctx context.Context, inv toolaudit.ToolInvocation) (toolaudit.ToolInvocation, error)
}

type ToolAuditService struct {
	repo ToolAuditRepository
}

func NewToolAuditService(repo ToolAuditRepository) *ToolAuditService {
	return &ToolAuditService{repo: repo}
}

func (s *ToolAuditService) Record(ctx context.Context, input toolaudit.ToolInvocation) (toolaudit.ToolInvocation, error) {
	if input.ID == "" {
		input.ID = uuid.New().String()
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}

	return s.repo.InsertChained(ctx, input)
}
