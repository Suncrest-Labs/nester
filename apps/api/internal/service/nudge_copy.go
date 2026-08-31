package service

import (
	"context"
	"time"

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
//
// The LLM call is bounded by llmCopyTimeout and derived from ctx, so the
// caller can abort it (request cancellation, a tighter deadline further up
// the call chain) instead of the outbound call being uncancellable
// (nester#1198).
func (c CompositeCopyGenerator) Generate(ctx context.Context, nudgeType nudge.NudgeType, facts nudge.Facts, segment usersignal.Segment) (string, string, string, error) {
	def := nudge.Catalog[nudgeType]
	if def.UsesLLMCopy && c.LLM != nil {
		req := intelligence.NudgeCopyRequest{
			NudgeType: string(nudgeType),
			Segment:   string(segment),
			Facts:     facts.AllowedFacts(),
			RequestID: uuid.New().String(),
		}
		llmCtx, cancel := context.WithTimeout(ctx, llmCopyTimeout)
		resp, err := c.LLM.GenerateNudgeCopy(llmCtx, req)
		cancel()
		if err == nil && resp.Title != "" && resp.Body != "" {
			return resp.Title, resp.Body, "llm", nil
		}
	}
	title, body, err := c.Template.Generate(nudgeType, facts)
	return title, body, "template", err
}

// llmCopyTimeout bounds the outbound LLM call for nudge copy generation, so
// a slow/hung provider falls back to the deterministic template within a
// bounded time rather than blocking the caller indefinitely (nester#1198).
const llmCopyTimeout = 10 * time.Second

type LLMCopyGenerator struct {
	Client LLMCopyClient
}

func (g LLMCopyGenerator) GenerateNudgeCopy(ctx context.Context, req intelligence.NudgeCopyRequest) (*intelligence.NudgeCopyResponse, error) {
	return g.Client.GenerateNudgeCopy(ctx, req)
}
