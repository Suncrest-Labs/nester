// Command rotate-cipher re-encrypts account cipher ciphertext from one key
// version to another, supporting the rotation procedure documented in
// docs/security/key-rotation.md.
//
// Usage:
//   rotate-cipher --from=v1 --to=v2 [--dry-run] [--batch-size=10]
//
// This command reads rows encrypted with the --from version, decrypts them,
// re-encrypts with the --to version, and updates the row in a single
// transaction. It is safe to run while traffic is live, as long as both keys
// are present in ACCOUNT_CIPHER_KEYS.
//
// The --dry-run flag reports what would be re-encrypted without making changes.
// Use it to estimate time and verify the backlog before committing to a rotation.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

func main() {
	var (
		fromVersion = flag.String("from", "", "Source key version (e.g. v1)")
		toVersion   = flag.String("to", "", "Target key version (e.g. v2)")
		dryRun      = flag.Bool("dry-run", false, "Report what would be re-encrypted without making changes")
		batchSize   = flag.Int("batch-size", 10, "Number of rows to process per transaction")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *fromVersion == "" || *toVersion == "" {
		logger.Error("both --from and --to are required")
		fmt.Fprintf(os.Stderr, "Usage: %s --from=v1 --to=v2 [--dry-run] [--batch-size=10]\n", os.Args[0])
		os.Exit(1)
	}

	if *fromVersion == *toVersion {
		logger.Error("--from and --to must be different")
		os.Exit(1)
	}

	if *batchSize < 1 {
		logger.Error("--batch-size must be at least 1")
		os.Exit(1)
	}

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if !cfg.AccountCipher().Configured() {
		logger.Error("ACCOUNT_CIPHER_KEYS is not configured")
		os.Exit(1)
	}

	// Verify both keys are present
	keys := cfg.AccountCipher().Keys()
	if _, ok := keys[*fromVersion]; !ok {
		logger.Error("source key version not found in ACCOUNT_CIPHER_KEYS", "version", *fromVersion)
		os.Exit(1)
	}
	if _, ok := keys[*toVersion]; !ok {
		logger.Error("target key version not found in ACCOUNT_CIPHER_KEYS", "version", *toVersion)
		os.Exit(1)
	}

	cipher, err := crypto.NewAccountCipher(cfg.AccountCipher())
	if err != nil {
		logger.Error("failed to initialize cipher", "error", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.Database().DSN())
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	logger.Info("starting cipher rotation",
		"from", *fromVersion,
		"to", *toVersion,
		"dry_run", *dryRun,
		"batch_size", *batchSize,
	)

	if *dryRun {
		if err := dryRunReport(ctx, pool, *fromVersion, logger); err != nil {
			logger.Error("dry run failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := rotateWebhookSecrets(ctx, pool, cipher, *fromVersion, *toVersion, *batchSize, logger); err != nil {
		logger.Error("rotation failed", "error", err)
		os.Exit(1)
	}

	logger.Info("rotation complete")
}

// dryRunReport prints the number of rows that would be re-encrypted for each
// table, grouped by current key version.
func dryRunReport(ctx context.Context, pool *pgxpool.Pool, fromVersion string, logger *slog.Logger) error {
	logger.Info("DRY RUN: counting rows to re-encrypt")

	// Webhooks
	var webhookCount int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhooks WHERE secret_key_version = $1`, fromVersion).Scan(&webhookCount)
	if err != nil {
		return fmt.Errorf("count webhooks: %w", err)
	}

	logger.Info("dry run complete",
		"webhooks_to_rotate", webhookCount,
		"total_rows", webhookCount,
	)

	if webhookCount == 0 {
		logger.Info("no rows to rotate, rotation is already complete or not needed")
		return nil
	}

	estimatedSeconds := int64(webhookCount) * 3 // Conservative estimate: 3s per row
	logger.Info("estimated time",
		"seconds", estimatedSeconds,
		"minutes", estimatedSeconds/60,
		"duration", time.Duration(estimatedSeconds)*time.Second,
	)

	return nil
}

// rotateWebhookSecrets re-encrypts webhook signing secrets from fromVersion to toVersion.
func rotateWebhookSecrets(
	ctx context.Context,
	pool *pgxpool.Pool,
	cipher *crypto.AccountCipher,
	fromVersion string,
	toVersion string,
	batchSize int,
	logger *slog.Logger,
) error {
	startTime := time.Now()

	// Count total rows
	var totalRows int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhooks WHERE secret_key_version = $1`, fromVersion).Scan(&totalRows)
	if err != nil {
		return fmt.Errorf("count webhooks: %w", err)
	}

	if totalRows == 0 {
		logger.Info("no webhook rows to rotate")
		return nil
	}

	logger.Info("rotating webhook secrets",
		"total_rows", totalRows,
		"batch_size", batchSize,
	)

	processed := 0
	for {
		// Fetch a batch of rows still on the old version
		rows, err := pool.Query(ctx, `
			SELECT id, secret, secret_key_version
			FROM webhooks
			WHERE secret_key_version = $1
			ORDER BY id
			LIMIT $2
		`, fromVersion, batchSize)
		if err != nil {
			return fmt.Errorf("fetch batch: %w", err)
		}

		type row struct {
			id         string
			ciphertext []byte
			version    string
		}

		batch := make([]row, 0, batchSize)
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.ciphertext, &r.version); err != nil {
				rows.Close()
				return fmt.Errorf("scan row: %w", err)
			}
			batch = append(batch, r)
		}
		rows.Close()

		if len(batch) == 0 {
			// No more rows to process
			break
		}

		// Process the batch in a single transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}

		for _, r := range batch {
			// Decrypt with old key
			envelope := crypto.CipherEnvelope{
				Ciphertext: r.ciphertext,
				Version:    r.version,
			}
			plaintext, err := cipher.Decrypt(envelope)
			if err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("decrypt row %s: %w", r.id, err)
			}

			// Re-encrypt with new key
			newEnvelope, err := cipher.Encrypt(plaintext)
			if err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("encrypt row %s: %w", r.id, err)
			}

			// Verify the version is what we expect
			if newEnvelope.Version != toVersion {
				tx.Rollback(ctx)
				return fmt.Errorf("expected version %s but got %s", toVersion, newEnvelope.Version)
			}

			// Update the row
			result, err := tx.Exec(ctx, `
				UPDATE webhooks
				SET secret = $1, secret_key_version = $2
				WHERE id = $3 AND secret_key_version = $4
			`, newEnvelope.Ciphertext, newEnvelope.Version, r.id, fromVersion)
			if err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("update row %s: %w", r.id, err)
			}

			if result.RowsAffected() != 1 {
				tx.Rollback(ctx)
				return fmt.Errorf("update row %s: expected 1 row affected, got %d (concurrent modification?)", r.id, result.RowsAffected())
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit batch: %w", err)
		}

		processed += len(batch)
		elapsed := time.Since(startTime)
		rate := float64(processed) / elapsed.Seconds()
		remaining := totalRows - processed
		eta := time.Duration(float64(remaining)/rate) * time.Second

		logger.Info("batch complete",
			"processed", processed,
			"total", totalRows,
			"percent", fmt.Sprintf("%.1f%%", float64(processed)/float64(totalRows)*100),
			"rate_per_sec", fmt.Sprintf("%.2f", rate),
			"elapsed", elapsed.Round(time.Second),
			"eta", eta.Round(time.Second),
		)

		// Safety: if we processed fewer rows than batch size, we're done
		if len(batch) < batchSize {
			break
		}
	}

	elapsed := time.Since(startTime)
	logger.Info("webhook rotation complete",
		"rows_rotated", processed,
		"duration", elapsed.Round(time.Second),
		"rate_per_sec", fmt.Sprintf("%.2f", float64(processed)/elapsed.Seconds()),
	)

	return nil
}
