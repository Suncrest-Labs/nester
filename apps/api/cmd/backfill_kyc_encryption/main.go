// Command backfill_kyc_encryption encrypts existing plaintext KYC document
// fields (id_number, front_object_key, back_object_key) that were stored before
// the encryption migration was applied.
//
// It is safe, idempotent and resumable:
//   - Only rows where id_number_encrypted IS NULL are processed.
//   - Each row is committed independently so an interrupted run continues from
//     where it left off.
//   - A second run after completion finds zero rows to process and does nothing.
//
// Usage:
//
//	go run ./cmd/backfill_kyc_encryption [-batch-size=100] [-timeout=5m]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
)

func main() {
	batchSize := flag.Int("batch-size", 100, "rows to process per batch")
	timeout := flag.Duration("timeout", 0, "max duration for the entire backfill (0 = no timeout)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if !cfg.AccountCipher().Configured() {
		logger.Error("account cipher is not configured; set BANK_ACCOUNT_ENCRYPTION_KEY or ACCOUNT_CIPHER_KEYS")
		os.Exit(1)
	}

	cc := cfg.AccountCipher()
	ciph, err := crypto.NewAccountCipherWithKeys(cc.ActiveVersion(), cc.Keys(), cc.FingerprintKey())
	if err != nil {
		logger.Error("failed to create cipher", "error", err)
		os.Exit(1)
	}

	pg, err := repository.NewPostgresDB(cfg.Database())
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pg.Pool.Close()

	db := stdlib.OpenDBFromPool(pg.Pool)
	defer db.Close()

	repo := postgres.NewUserRepository(db)

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	processed, err := runBackfill(ctx, logger, repo, ciph, *batchSize)
	if err != nil {
		logger.Error("backfill failed", "error", err, "processed", processed)
		os.Exit(1)
	}

	logger.Info("backfill complete", "processed", processed)
}

func runBackfill(
	ctx context.Context,
	logger *slog.Logger,
	repo *postgres.UserRepository,
	ciph *crypto.AccountCipher,
	batchSize int,
) (int, error) {
	active := ciph.ActiveVersion()
	total := 0

	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		rows, err := repo.ScanKYCDocumentsForBackfill(ctx, batchSize)
		if err != nil {
			return total, fmt.Errorf("scan for backfill: %w", err)
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			idNumEnv, err := ciph.Encrypt(row.IDNumber)
			if err != nil {
				return total, fmt.Errorf("encrypt id_number for row %s: %w", row.ID, err)
			}
			frontKeyEnv, err := ciph.Encrypt(row.FrontObjectKey)
			if err != nil {
				return total, fmt.Errorf("encrypt front_object_key for row %s: %w", row.ID, err)
			}
			fingerprint := ciph.Fingerprint(row.IDNumber)

			var backKeyEnc []byte
			if row.BackObjectKey != nil {
				backKeyEnv, err := ciph.Encrypt(*row.BackObjectKey)
				if err != nil {
					return total, fmt.Errorf("encrypt back_object_key for row %s: %w", row.ID, err)
				}
				backKeyEnc = backKeyEnv.Ciphertext
			}

			if err := repo.UpdateKYCCipher(ctx, row.ID, idNumEnv.Ciphertext, frontKeyEnv.Ciphertext, backKeyEnc, active); err != nil {
				return total, fmt.Errorf("update row %s: %w", row.ID, err)
			}

			// Update fingerprint separately since UpdateKYCCipher doesn't set it
			if err := repo.UpdateKYCFingerprint(ctx, row.ID, fingerprint); err != nil {
				return total, fmt.Errorf("update fingerprint for row %s: %w", row.ID, err)
			}

			total++
		}

		logger.Info("backfill progress", "processed", total)
	}

	return total, nil
}
