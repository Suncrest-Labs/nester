package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

func testB64Key(fill byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = fill
	}
	return base64.StdEncoding.EncodeToString(raw)
}

type memStore struct {
	rows []WebhookCipherRow
}

func (m *memStore) ListLegacyWebhooks(_ context.Context, activeVersion string, limit int) ([]WebhookCipherRow, error) {
	var out []WebhookCipherRow
	for _, r := range m.rows {
		if r.KeyVersion != activeVersion {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) UpdateWebhookSecret(_ context.Context, id string, expectedOldVersion string, newCiphertext []byte, newVersion string) error {
	for i := range m.rows {
		if m.rows[i].ID == id && m.rows[i].KeyVersion == expectedOldVersion {
			m.rows[i].Ciphertext = append([]byte(nil), newCiphertext...)
			m.rows[i].KeyVersion = newVersion
			return nil
		}
	}
	return nil
}

func (m *memStore) CountByVersion(_ context.Context) (map[string]int, error) {
	counts := make(map[string]int)
	for _, r := range m.rows {
		counts[r.KeyVersion]++
	}
	return counts, nil
}

func TestRotateWebhookSecrets_RotatesLegacyRowsWithoutLoggingSecrets(t *testing.T) {
	keyV1 := testB64Key(1)
	keyV2 := testB64Key(2)

	v1Cipher, err := crypto.NewAccountCipherWithKeys("v1", map[string]string{"v1": keyV1}, "")
	if err != nil {
		t.Fatalf("NewAccountCipherWithKeys(v1): %v", err)
	}

	env1, err := v1Cipher.Encrypt("whsec_super_secret_one")
	if err != nil {
		t.Fatalf("Encrypt(1): %v", err)
	}
	env2, err := v1Cipher.Encrypt("whsec_super_secret_two")
	if err != nil {
		t.Fatalf("Encrypt(2): %v", err)
	}

	store := &memStore{
		rows: []WebhookCipherRow{
			{ID: "wh-1", KeyVersion: env1.KeyVersion, Ciphertext: env1.Ciphertext},
			{ID: "wh-2", KeyVersion: env2.KeyVersion, Ciphertext: env2.Ciphertext},
		},
	}

	v2Cipher, err := buildCipher("v1:"+keyV1+",v2:"+keyV2, "v2", "")
	if err != nil {
		t.Fatalf("buildCipher: %v", err)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	summary, err := RotateWebhookSecrets(context.Background(), store, v2Cipher, 1, false, logger)
	if err != nil {
		t.Fatalf("RotateWebhookSecrets: %v", err)
	}

	if summary.RotatedCount != 2 {
		t.Fatalf("RotatedCount = %d, want 2", summary.RotatedCount)
	}
	if summary.LegacyRemains != 0 {
		t.Fatalf("LegacyRemains = %d, want 0", summary.LegacyRemains)
	}
	if summary.VersionCounts["v2"] != 2 {
		t.Fatalf("VersionCounts[v2] = %d, want 2", summary.VersionCounts["v2"])
	}

	// Verify rotated rows decrypt cleanly with a v2-only cipher (after v1 retirement).
	v2OnlyCipher, err := crypto.NewAccountCipherWithKeys("v2", map[string]string{"v2": keyV2}, keyV1)
	if err != nil {
		t.Fatalf("NewAccountCipherWithKeys(v2 only): %v", err)
	}
	got1, err := v2OnlyCipher.Decrypt(crypto.CipherEnvelope{
		KeyVersion: store.rows[0].KeyVersion,
		Ciphertext: store.rows[0].Ciphertext,
	})
	if err != nil || got1 != "whsec_super_secret_one" {
		t.Fatalf("Decrypt(wh-1) = %q, err = %v", got1, err)
	}

	// Ensure logs never leak plaintext or key material.
	logs := logBuf.String()
	for _, forbidden := range []string{"whsec_super_secret_one", "whsec_super_secret_two", keyV1, keyV2} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("log output leaked sensitive value %q", forbidden)
		}
	}
}

func TestRotateWebhookSecrets_DryRunDoesNotMutateStore(t *testing.T) {
	keyV1 := testB64Key(1)
	keyV2 := testB64Key(2)

	v1Cipher, _ := crypto.NewAccountCipherWithKeys("v1", map[string]string{"v1": keyV1}, "")
	env1, _ := v1Cipher.Encrypt("whsec_dry_run_secret")

	store := &memStore{
		rows: []WebhookCipherRow{
			{ID: "wh-dry", KeyVersion: env1.KeyVersion, Ciphertext: env1.Ciphertext},
		},
	}

	v2Cipher, _ := buildCipher("v1:"+keyV1+",v2:"+keyV2, "v2", "")
	summary, err := RotateWebhookSecrets(context.Background(), store, v2Cipher, 10, true, nil)
	if err != nil {
		t.Fatalf("RotateWebhookSecrets(dryRun): %v", err)
	}
	if summary.RotatedCount != 1 {
		t.Fatalf("RotatedCount = %d, want 1", summary.RotatedCount)
	}
	if summary.LegacyRemains != 1 {
		t.Fatalf("LegacyRemains = %d, want 1 in dry-run mode", summary.LegacyRemains)
	}
	if store.rows[0].KeyVersion != "v1" {
		t.Fatalf("expected dry-run to preserve v1, got %q", store.rows[0].KeyVersion)
	}
}
