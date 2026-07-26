package harvest

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// HarvestCommand is the intent passed to the Executor for one vault harvest.
type HarvestCommand struct {
	Payload HarvestJobPayload
}

// HarvestOutcome summarizes what an executed harvest did.
type HarvestOutcome struct {
	// NetYield is the net yield claimed, in the vault currency. Zero means the
	// harvest was a no-op (yield already claimed) — still a success.
	NetYield string
	TxHash   string
}

// Executor performs the on-chain harvest for one vault. Implementations MUST be
// idempotent: a re-delivered job (lease expiry, retry) must not double-claim —
// the vault-service harvest naturally no-ops when there is no unclaimed yield.
type Executor interface {
	Harvest(ctx context.Context, cmd HarvestCommand) (HarvestOutcome, error)
}

// JobHandler adapts the harvest Executor to the job-queue Handler interface.
type JobHandler struct {
	exec   Executor
	logger *slog.Logger
}

// NewJobHandler constructs a JobHandler. logger may be nil.
func NewJobHandler(exec Executor, logger *slog.Logger) *JobHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &JobHandler{exec: exec, logger: logger}
}

// Handle decodes the harvest payload and executes it. A malformed payload is a
// permanent failure (retrying cannot fix it) and is dead-lettered; a transient
// executor error is returned as-is so the queue retries with backoff.
func (h *JobHandler) Handle(ctx context.Context, job jobqueue.Job) error {
	var p HarvestJobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return jobqueue.Permanent(err)
	}
	out, err := h.exec.Harvest(ctx, HarvestCommand{Payload: p})
	if err != nil {
		return err
	}
	h.logger.Info("harvest executed",
		"vault_id", p.VaultID,
		"net_yield", out.NetYield,
		"tx_hash", out.TxHash,
	)
	return nil
}
