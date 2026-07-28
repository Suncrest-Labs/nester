package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type NudgeEngineConfig struct {
	Enabled  bool
	Interval time.Duration
}

type ActiveUserLister interface {
	ListActiveGoalUserIDs(ctx context.Context) ([]uuid.UUID, error)
}

type NudgeEvaluator interface {
	EvaluateAndDispatch(ctx context.Context, userID uuid.UUID) error
}

type NudgeEngineJob struct {
	cfg    NudgeEngineConfig
	users  ActiveUserLister
	engine NudgeEvaluator
	logger *slog.Logger
	clock  func() time.Time
}

func NewNudgeEngineJob(cfg NudgeEngineConfig, users ActiveUserLister, engine NudgeEvaluator, logger *slog.Logger) *NudgeEngineJob {
	return &NudgeEngineJob{
		cfg:    cfg,
		users:  users,
		engine: engine,
		logger: logger,
		clock:  time.Now,
	}
}

func (j *NudgeEngineJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		return
	}
	
	j.Tick(ctx) // Immediate first tick
	
	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

func (j *NudgeEngineJob) Tick(ctx context.Context) {
	userIDs, err := j.users.ListActiveGoalUserIDs(ctx)
	if err != nil {
		j.logger.Error("failed to list active users", "error", err)
		return
	}
	
	for _, uid := range userIDs {
		if err := j.engine.EvaluateAndDispatch(ctx, uid); err != nil {
			j.logger.Error("failed to evaluate nudge for user", "user_id", uid, "error", err)
		}
	}
}
