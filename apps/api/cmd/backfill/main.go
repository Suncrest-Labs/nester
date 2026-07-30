// Command backfill runs a historical chain backfill or rebuild over a
// ledger range (nester#840), reusing the same event-indexer handler
// dispatch the forward indexer uses (internal/stellar/backfill.go).
//
// Usage:
//
//	go run ./cmd/backfill -from=1000 -to=2000 -initiated-by=alice
//	go run ./cmd/backfill -from=1000 -to=2000 -initiated-by=alice -dry-run
//	go run ./cmd/backfill -from=1000 -to=2000 -initiated-by=alice -mode=rebuild -contracts=CABC...,CDEF...
//	go run ./cmd/backfill -resume=<run-id>
//
// Configuration is read from the same environment as the API. -initiated-by
// is required for every new run (not for -resume, which continues an
// existing audited run) — this tool is operator-initiated and every run is
// recorded to backfill_runs regardless of outcome.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	from := flag.Uint64("from", 0, "first ledger to backfill (inclusive)")
	to := flag.Uint64("to", 0, "last ledger to backfill (inclusive)")
	contracts := flag.String("contracts", "", "comma-separated contract ids (empty = all vault contracts)")
	mode := flag.String("mode", "backfill", `"backfill" or "rebuild"`)
	dryRun := flag.Bool("dry-run", false, "report what would be processed without writing anything")
	initiatedBy := flag.String("initiated-by", "", "operator identity, for the audit trail (required for a new run)")
	resume := flag.String("resume", "", "resume a previously interrupted run by its id, instead of starting a new one")
	throttle := flag.Duration("throttle", 500*time.Millisecond, "pause between batches, so live indexing is not starved of DB/RPC capacity")
	timeout := flag.Duration("timeout", 0, "overall timeout for the run (0 = no timeout)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger, err := logpkg.New(cfg.Log(), version)
	if err != nil {
		return err
	}

	pgPool, err := repository.NewPostgresDB(cfg.Database())
	if err != nil {
		return err
	}
	defer pgPool.Pool.Close()

	db := stdlib.OpenDBFromPool(pgPool.Pool)
	defer db.Close()

	repo := postgres.NewBackfillRepository(db)
	runner := &stellar.Runner{
		DB:       db,
		Repo:     repo,
		RPCURL:   cfg.Stellar().RPCURL(),
		Logger:   logger,
		Throttle: *throttle,
	}

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	start := time.Now()

	var run *backfill.Run
	if *resume != "" {
		runID, parseErr := uuid.Parse(*resume)
		if parseErr != nil {
			return fmt.Errorf("-resume must be a valid run id (UUID): %w", parseErr)
		}
		run, err = runner.Resume(ctx, runID)
	} else {
		if *initiatedBy == "" {
			flag.Usage()
			return fmt.Errorf("-initiated-by is required for a new run")
		}
		var contractIDs []string
		if strings.TrimSpace(*contracts) != "" {
			for _, c := range strings.Split(*contracts, ",") {
				if c = strings.TrimSpace(c); c != "" {
					contractIDs = append(contractIDs, c)
				}
			}
		}
		run, err = runner.Start(ctx, stellar.StartInput{
			FromLedger:  *from,
			ToLedger:    *to,
			ContractIDs: contractIDs,
			Mode:        backfill.Mode(*mode),
			DryRun:      *dryRun,
			InitiatedBy: *initiatedBy,
		})
	}

	if run != nil {
		logger.Info("backfill run",
			"run_id", run.ID,
			"status", run.Status,
			"from_ledger", run.FromLedger,
			"to_ledger", run.ToLedger,
			"mode", run.Mode,
			"dry_run", run.DryRun,
			"events_processed", run.EventsProcessed,
			"events_skipped_duplicate", run.EventsSkippedDuplicate,
			"duration", time.Since(start).String(),
		)
	}
	if err != nil {
		return fmt.Errorf("backfill run failed: %w", err)
	}
	return nil
}
