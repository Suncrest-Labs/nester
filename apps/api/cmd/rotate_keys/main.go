// Command rotate_keys re-encrypts stored bank account numbers under the active
// key version. It is safe to run repeatedly (idempotent) and safe to interrupt
// (resumable): rows already on the active key version are skipped, and each row
// is committed as it is rotated.
//
// Usage:
//
//	go run ./cmd/rotate_keys [-batch-size=500] [-timeout=0]
//
// Configuration is read from the same environment as the API (see
// ACCOUNT_CIPHER_KEYS / ACCOUNT_CIPHER_ACTIVE_KEY). It never logs plaintext,
// keys, or ciphertext — only counts and row IDs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	cryptopkg "github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/rotation"
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
	batchSize := flag.Int("batch-size", 500, "number of rows to re-encrypt per batch")
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

	acCfg := cfg.AccountCipher()
	if !acCfg.Configured() {
		return fmt.Errorf("account cipher is not configured; set ACCOUNT_CIPHER_KEYS (or BANK_ACCOUNT_ENCRYPTION_KEY)")
	}
	cipher, err := cryptopkg.NewAccountCipherWithKeys(acCfg.ActiveVersion(), acCfg.Keys(), acCfg.FingerprintKey())
	if err != nil {
		return fmt.Errorf("build account cipher: %w", err)
	}

	pgPool, err := repository.NewPostgresDB(cfg.Database())
	if err != nil {
		return err
	}
	defer pgPool.Pool.Close()

	db := stdlib.OpenDBFromPool(pgPool.Pool)
	defer db.Close()

	bankRepo := postgres.NewBankAccountRepository(db)
	userRepo := postgres.NewUserRepository(db)

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	start := time.Now()

	// Rotate bank account ciphertext
	bankStats, err := rotation.NewRotator(bankRepo, cipher).Run(ctx, rotation.Options{
		BatchSize: *batchSize,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("bank account rotation failed after %d rows rotated: %w", bankStats.Rotated, err)
	}

	logger.Info("bank account rotation done",
		"rotated", bankStats.Rotated,
		"pending_at_start", bankStats.Pending,
	)

	// Rotate KYC document ciphertext
	kycStore := &kycRotationStore{repo: userRepo}
	kycStats, err := rotation.NewRotator(kycStore, cipher).Run(ctx, rotation.Options{
		BatchSize: *batchSize,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("kyc document rotation failed after %d rows rotated: %w", kycStats.Rotated, err)
	}

	logger.Info("rotation finished",
		"active_version", acCfg.ActiveVersion(),
		"bank_accounts_rotated", bankStats.Rotated,
		"kyc_documents_rotated", kycStats.Rotated,
		"duration", time.Since(start).String(),
	)
	return nil
}

// kycRotationStore adapts *postgres.UserRepository to implement rotation.Store
// for KYC documents. It packs/unpacks the three encrypted fields (id_number,
// front_object_key, back_object_key) into a single ciphertext blob for the
// rotator interface.
type kycRotationStore struct {
	repo *postgres.UserRepository
}

func (s *kycRotationStore) CountPending(ctx context.Context, activeVersion string) (int, error) {
	return s.repo.CountPendingKYCEncryption(ctx, activeVersion)
}

func (s *kycRotationStore) ScanPending(ctx context.Context, activeVersion string, limit int) ([]rotation.EncryptedRow, error) {
	return s.repo.ScanPendingKYCEncryption(ctx, activeVersion, limit)
}

func (s *kycRotationStore) UpdateCipher(ctx context.Context, id uuid.UUID, ciphertext []byte, keyVersion string) error {
	idNum, frontKey, backKey := postgres.UnpackKYCCiphertexts(ciphertext)
	return s.repo.UpdateKYCCipher(ctx, id, idNum, frontKey, backKey, keyVersion)
}
