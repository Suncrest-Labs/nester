package service

import (
	"context"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

// CompositeCopyGenerator produces nudge copy from the deterministic
// template catalog. The LLM copy path was removed with the intelligence
// service; the source tag stays so dispatch logging keeps its shape.
type CompositeCopyGenerator struct {
	Template nudge.TemplateCopyGenerator
}

func (c CompositeCopyGenerator) Generate(ctx context.Context, nudgeType nudge.NudgeType, facts nudge.Facts, segment usersignal.Segment) (string, string, string, error) {
	_ = ctx
	_ = segment
	title, body, err := c.Template.Generate(nudgeType, facts)
	return title, body, "template", err
}
