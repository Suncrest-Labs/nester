package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/toolaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type mockToolAuditRepo struct {
	latestHash string
	latestErr  error
	insertErr  error
	inserted   *toolaudit.ToolInvocation
}

func (m *mockToolAuditRepo) InsertChained(ctx context.Context, inv toolaudit.ToolInvocation) (toolaudit.ToolInvocation, error) {
	if m.latestErr != nil {
		return toolaudit.ToolInvocation{}, m.latestErr
	}
	if m.insertErr != nil {
		return toolaudit.ToolInvocation{}, m.insertErr
	}
	inv.PrevHash = m.latestHash
	inv.EntryHash = inv.ComputeHash(m.latestHash)
	m.inserted = &inv
	return inv, nil
}

func TestToolAuditService_Record(t *testing.T) {
	repo := &mockToolAuditRepo{
		latestHash: "genesis-hash",
	}
	svc := service.NewToolAuditService(repo)

	input := toolaudit.ToolInvocation{
		UserID:         "user-1",
		RequestID:      "req-1",
		ConversationID: "conv-1",
		ToolName:       "test-tool",
		Arguments:      []byte(`{"foo":"bar"}`),
	}

	result, err := svc.Record(context.Background(), input)

	assert.NoError(t, err)
	assert.NotEmpty(t, result.ID)
	assert.False(t, result.CreatedAt.IsZero())
	assert.Equal(t, "genesis-hash", result.PrevHash)
	assert.NotEmpty(t, result.EntryHash)
	
	assert.Equal(t, result, *repo.inserted)
}
