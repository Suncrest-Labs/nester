package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64key(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// cipherEnv sets the minimum required config plus the caller's cipher-related
// overrides, then loads. It clears the cipher keys first so ambient CI env does
// not leak in.
func cipherEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	for _, k := range []string{
		"BANK_ACCOUNT_ENCRYPTION_KEY",
		"ACCOUNT_CIPHER_KEYS",
		"ACCOUNT_CIPHER_ACTIVE_KEY",
		"ACCOUNT_CIPHER_FINGERPRINT_KEY",
	} {
		t.Setenv(k, "")
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
}

func TestAccountCipher_FallbackToLegacyKey(t *testing.T) {
	cipherEnv(t, map[string]string{"BANK_ACCOUNT_ENCRYPTION_KEY": b64key(7)})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ac := cfg.AccountCipher()
	if !ac.Configured() {
		t.Fatal("expected cipher to be configured from legacy key")
	}
	if ac.ActiveVersion() != "v1" {
		t.Fatalf("active version = %q, want v1", ac.ActiveVersion())
	}
	if got := ac.Keys(); len(got) != 1 || got["v1"] != b64key(7) {
		t.Fatalf("keys = %v, want single v1 legacy key", got)
	}
}

func TestAccountCipher_MultiKey(t *testing.T) {
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       "v1:" + b64key(1) + ",v2:" + b64key(2),
		"ACCOUNT_CIPHER_ACTIVE_KEY": "v2",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ac := cfg.AccountCipher()
	if ac.ActiveVersion() != "v2" {
		t.Fatalf("active version = %q, want v2", ac.ActiveVersion())
	}
	keys := ac.Keys()
	if keys["v1"] != b64key(1) || keys["v2"] != b64key(2) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestAccountCipher_ActiveMissingFails(t *testing.T) {
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       "v1:" + b64key(1) + ",v2:" + b64key(2),
		"ACCOUNT_CIPHER_ACTIVE_KEY": "v3",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error when active version is not in the key set")
	}
}

func TestAccountCipher_RequiresActiveWhenKeysSet(t *testing.T) {
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS": "v1:" + b64key(1),
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error when ACCOUNT_CIPHER_ACTIVE_KEY is missing")
	}
}

func TestAccountCipher_MalformedPairFails(t *testing.T) {
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       "v1",
		"ACCOUNT_CIPHER_ACTIVE_KEY": "v1",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed ACCOUNT_CIPHER_KEYS entry")
	}
}

func TestAccountCipher_EmptyKeysetFails(t *testing.T) {
	// A non-empty setting that parses to zero entries must fail closed, not
	// silently disable encryption.
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       ",",
		"ACCOUNT_CIPHER_ACTIVE_KEY": "v1",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error when ACCOUNT_CIPHER_KEYS yields no usable entries")
	}
}

func TestAccountCipher_VersionTooLongFails(t *testing.T) {
	long := "v" + strings.Repeat("9", 40) // > 32 chars
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       long + ":" + b64key(1),
		"ACCOUNT_CIPHER_ACTIVE_KEY": long,
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error for key version exceeding 32 characters")
	}
}

func TestAccountCipher_NoV1RequiresFingerprintKey(t *testing.T) {
	// A key set without v1 and without an explicit pepper must be rejected.
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":       "v2:" + b64key(2) + ",v3:" + b64key(3),
		"ACCOUNT_CIPHER_ACTIVE_KEY": "v2",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected error when a v1-less key set omits ACCOUNT_CIPHER_FINGERPRINT_KEY")
	}
}

func TestAccountCipher_NoV1WithFingerprintKeyOK(t *testing.T) {
	cipherEnv(t, map[string]string{
		"ACCOUNT_CIPHER_KEYS":            "v2:" + b64key(2) + ",v3:" + b64key(3),
		"ACCOUNT_CIPHER_ACTIVE_KEY":      "v2",
		"ACCOUNT_CIPHER_FINGERPRINT_KEY": b64key(9),
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AccountCipher().FingerprintKey() != b64key(9) {
		t.Fatal("expected explicit fingerprint key to be carried through")
	}
}
