// Command rotate-keys re-encrypts database rows sealed under legacy
// AccountCipher key versions onto the currently active key version.
//
// Usage:
//
//	go run ./cmd/rotate-keys [--dsn=postgres://...] [--dry-run] [--batch-size=100]
//
// Key configuration is read from ACCOUNT_CIPHER_KEYS, ACCOUNT_CIPHER_ACTIVE_KEY,
// and ACCOUNT_CIPHER_FINGERPRINT_KEY (or flags). Plaintext, key material, and
// ciphertext are never logged.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

// RecordStore abstracts the database operations required to rotate encrypted
// webhook secrets so rotation logic can be unit-tested without a live database.
type RecordStore interface {
	ListLegacyWebhooks(ctx context.Context, activeVersion string, limit int) ([]WebhookCipherRow, error)
	UpdateWebhookSecret(ctx context.Context, id string, expectedOldVersion string, newCiphertext []byte, newVersion string) error
	CountByVersion(ctx context.Context) (map[string]int, error)
}

// WebhookCipherRow holds the non-plaintext metadata and sealed secret for a single row.
type WebhookCipherRow struct {
	ID         string
	KeyVersion string
	Ciphertext []byte
}

// RotationSummary reports the outcome of a rotation run without exposing secrets.
type RotationSummary struct {
	ActiveVersion  string         `json:"active_version"`
	RotatedCount   int            `json:"rotated_count"`
	DryRun         bool           `json:"dry_run"`
	VersionCounts  map[string]int `json:"version_counts"`
	LegacyRemains  int            `json:"legacy_remains"`
	ElapsedMillis  int64          `json:"elapsed_ms"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(context.Background(), os.Args[1:], logger); err != nil {
		logger.Error("rotate-keys failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("rotate-keys", flag.ContinueOnError)
	dsn := fs.String("dsn", "", "PostgreSQL DSN (defaults to DATABASE_DSN env var)")
	keysRaw := fs.String("keys", "", "Comma-separated version:base64key pairs (defaults to ACCOUNT_CIPHER_KEYS)")
	activeKey := fs.String("active-key", "", "Active key version (defaults to ACCOUNT_CIPHER_ACTIVE_KEY)")
	fingerprintKey := fs.String("fingerprint-key", "", "Fingerprint pepper key (defaults to ACCOUNT_CIPHER_FINGERPRINT_KEY)")
	dryRun := fs.Bool("dry-run", false, "Verify decryption of legacy rows without writing updates")
	batchSize := fs.Int("batch-size", 100, "Maximum rows to process per batch")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = godotenv.Load()

	if *dsn == "" {
		*dsn = strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	}
	if *keysRaw == "" {
		*keysRaw = strings.TrimSpace(os.Getenv("ACCOUNT_CIPHER_KEYS"))
	}
	if *activeKey == "" {
		*activeKey = strings.TrimSpace(os.Getenv("ACCOUNT_CIPHER_ACTIVE_KEY"))
	}
	if *fingerprintKey == "" {
		*fingerprintKey = strings.TrimSpace(os.Getenv("ACCOUNT_CIPHER_FINGERPRINT_KEY"))
	}

	if *dsn == "" {
		return errors.New("DATABASE_DSN environment variable or --dsn flag is required")
	}
	if *batchSize <= 0 {
		return errors.New("--batch-size must be positive")
	}

	cipher, err := buildCipher(*keysRaw, *activeKey, *fingerprintKey)
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	store := &sqlRecordStore{db: db}
	summary, err := RotateWebhookSecrets(ctx, store, cipher, *batchSize, *dryRun, logger)
	if err != nil {
		return err
	}

	logger.Info("key rotation finished",
		"active_version", summary.ActiveVersion,
		"rotated_count", summary.RotatedCount,
		"legacy_remains", summary.LegacyRemains,
		"dry_run", summary.DryRun,
		"elapsed_ms", summary.ElapsedMillis,
	)

	if !summary.DryRun && summary.LegacyRemains > 0 {
		return fmt.Errorf("%d row(s) still on legacy key versions after rotation", summary.LegacyRemains)
	}
	return nil
}

func buildCipher(keysRaw, activeVersion, fingerprintKey string) (*crypto.AccountCipher, error) {
	keysMap, err := parseKeyPairs(keysRaw)
	if err != nil {
		return nil, err
	}
	return crypto.NewAccountCipherWithKeys(activeVersion, keysMap, fingerprintKey)
}

func parseKeyPairs(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("ACCOUNT_CIPHER_KEYS is required")
	}
	out := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("malformed ACCOUNT_CIPHER_KEYS entry: expected version:base64key")
		}
		ver := strings.TrimSpace(parts[0])
		key := strings.TrimSpace(parts[1])
		if ver == "" || key == "" {
			return nil, errors.New("malformed ACCOUNT_CIPHER_KEYS entry: empty version or key")
		}
		out[ver] = key
	}
	if len(out) == 0 {
		return nil, errors.New("ACCOUNT_CIPHER_KEYS contained no valid key pairs")
	}
	return out, nil
}

// RotateWebhookSecrets decrypts every webhook secret not yet on cipher.ActiveVersion()
// and re-seals it under cipher.ActiveVersion().
func RotateWebhookSecrets(
	ctx context.Context,
	store RecordStore,
	cipher *crypto.AccountCipher,
	batchSize int,
	dryRun bool,
	logger *slog.Logger,
) (RotationSummary, error) {
	start := time.Now()
	active := cipher.ActiveVersion()
	rotated := 0

	for {
		rows, err := store.ListLegacyWebhooks(ctx, active, batchSize)
		if err != nil {
			return RotationSummary{}, fmt.Errorf("list legacy webhooks: %w", err)
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			plain, err := cipher.Decrypt(crypto.CipherEnvelope{
				KeyVersion: row.KeyVersion,
				Ciphertext: row.Ciphertext,
			})
			if err != nil {
				return RotationSummary{}, fmt.Errorf("decrypt webhook %s (version %q): %w", row.ID, row.KeyVersion, err)
			}

			sealed, err := cipher.Encrypt(plain)
			if err != nil {
				return RotationSummary{}, fmt.Errorf("re-encrypt webhook %s: %w", row.ID, err)
			}

			if !dryRun {
				if err := store.UpdateWebhookSecret(ctx, row.ID, row.KeyVersion, sealed.Ciphertext, sealed.KeyVersion); err != nil {
					return RotationSummary{}, fmt.Errorf("update webhook %s: %w", row.ID, err)
				}
			}

			rotated++
			if logger != nil {
				logger.Info("rotated webhook secret",
					"webhook_id", row.ID,
					"from_version", row.KeyVersion,
					"to_version", sealed.KeyVersion,
					"dry_run", dryRun,
				)
			}
		}

		if dryRun {
			break
		}
	}

	counts, err := store.CountByVersion(ctx)
	if err != nil {
		return RotationSummary{}, fmt.Errorf("verify key version counts: %w", err)
	}

	legacyRemains := 0
	for ver, count := range counts {
		if ver != active {
			legacyRemains += count
		}
	}

	return RotationSummary{
		ActiveVersion: active,
		RotatedCount:  rotated,
		DryRun:        dryRun,
		VersionCounts: counts,
		LegacyRemains: legacyRemains,
		ElapsedMillis: time.Since(start).Milliseconds(),
	}, nil
}

type sqlRecordStore struct {
	db *sql.DB
}

func (s *sqlRecordStore) ListLegacyWebhooks(ctx context.Context, activeVersion string, limit int) ([]WebhookCipherRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, secret_key_version, secret
		FROM webhooks
		WHERE secret IS NOT NULL
		  AND secret_key_version IS NOT NULL
		  AND secret_key_version <> $1
		ORDER BY id
		LIMIT $2
	`, activeVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WebhookCipherRow
	for rows.Next() {
		var r WebhookCipherRow
		if err := rows.Scan(&r.ID, &r.KeyVersion, &r.Ciphertext); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlRecordStore) UpdateWebhookSecret(ctx context.Context, id string, expectedOldVersion string, newCiphertext []byte, newVersion string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhooks
		SET secret = $1,
		    secret_key_version = $2
		WHERE id = $3
		  AND secret_key_version = $4
	`, newCiphertext, newVersion, id, expectedOldVersion)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("concurrent modification detected for webhook %s", id)
	}
	return nil
}

func (s *sqlRecordStore) CountByVersion(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT secret_key_version, COUNT(*)
		FROM webhooks
		WHERE secret_key_version IS NOT NULL
		GROUP BY secret_key_version
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var ver string
		var n int
		if err := rows.Scan(&ver, &n); err != nil {
			return nil, err
		}
		counts[ver] = n
	}
	return counts, rows.Err()
}
