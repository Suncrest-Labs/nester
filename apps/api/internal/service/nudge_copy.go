package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

type LLMCopyClient interface {
	GenerateNudgeCopy(ctx context.Context, req intelligence.NudgeCopyRequest) (*intelligence.NudgeCopyResponse, error)
}

type CompositeCopyGenerator struct {
	Template nudge.TemplateCopyGenerator
	LLM      LLMCopyClient
}

// Generate tries the LLM path for nudge types that opt into it, falling
// back to the deterministic template on any failure or empty response, and
// reports which source actually produced the copy so callers (dispatch
// logging, effectiveness tracking) record the truth rather than the intent.
func (c CompositeCopyGenerator) Generate(nudgeType nudge.NudgeType, facts nudge.Facts, segment usersignal.Segment) (string, string, string, error) {
	def := nudge.Catalog[nudgeType]
	if def.UsesLLMCopy && c.LLM != nil {
		req := intelligence.NudgeCopyRequest{
			NudgeType: string(nudgeType),
			Segment:   string(segment),
			Facts:     facts.AllowedFacts(),
			RequestID: uuid.New().String(),
		}
		resp, err := c.LLM.GenerateNudgeCopy(context.Background(), req)
		if err == nil && resp.Title != "" && resp.Body != "" {
			return resp.Title, resp.Body, "llm", nil
		}
	}
	title, body, err := c.Template.Generate(nudgeType, facts)
	return title, body, "template", err
}

type LLMCopyGenerator struct {
	Client LLMCopyClient
}

func (g LLMCopyGenerator) GenerateNudgeCopy(ctx context.Context, req intelligence.NudgeCopyRequest) (*intelligence.NudgeCopyResponse, error) {
	return g.Client.GenerateNudgeCopy(ctx, req)
}
