package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func key(b byte) string {
	raw := make([]byte, keyLen)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestNewAccountCipher_RejectsBadKey(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not base64":   "not-base64!!",
		"wrong length": base64.StdEncoding.EncodeToString(make([]byte, 16)),
	}
	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewAccountCipher(k); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("want ErrInvalidKey, got %v", err)
			}
		})
	}
}

func TestEncrypt_UsesActiveVersion(t *testing.T) {
	c, err := NewAccountCipherWithKeys("v2", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	env, err := c.Encrypt("0123456789")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if env.KeyVersion != "v2" {
		t.Fatalf("want active version v2, got %q", env.KeyVersion)
	}
	if c.ActiveVersion() != "v2" {
		t.Fatalf("ActiveVersion = %q", c.ActiveVersion())
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	c, err := NewAccountCipher(key(0))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	env, err := c.Encrypt("0123456789")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if env.KeyVersion != LegacyKeyVersion {
		t.Fatalf("single key should use %q, got %q", LegacyKeyVersion, env.KeyVersion)
	}
	got, err := c.Decrypt(env)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "0123456789" {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestDecrypt_CrossVersion(t *testing.T) {
	// A row sealed by the v1 single-key cipher must still decrypt after a v2 key
	// is added and made active.
	v1, err := NewAccountCipher(key(1))
	if err != nil {
		t.Fatalf("v1 cipher: %v", err)
	}
	legacyEnv, err := v1.Encrypt("9876543210")
	if err != nil {
		t.Fatalf("v1 encrypt: %v", err)
	}

	multi, err := NewAccountCipherWithKeys("v2", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if err != nil {
		t.Fatalf("multi cipher: %v", err)
	}
	got, err := multi.Decrypt(legacyEnv)
	if err != nil {
		t.Fatalf("cross-version decrypt: %v", err)
	}
	if got != "9876543210" {
		t.Fatalf("cross-version mismatch: %q", got)
	}
}

func TestDecrypt_UnknownVersionFails(t *testing.T) {
	// v2-only key set has no v1, so an explicit fingerprint pepper is required.
	c, err := NewAccountCipherWithKeys("v2", map[string]string{"v2": key(2)}, key(9))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	_, err = c.Decrypt(CipherEnvelope{KeyVersion: "v1", Ciphertext: []byte("whatever-bytes-here-not-real")})
	if !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("want ErrUnknownKeyVersion, got %v", err)
	}
}

func TestNewAccountCipherWithKeys_RequiresPepperWithoutV1(t *testing.T) {
	// No v1 and no explicit pepper must fail closed rather than defaulting the
	// fingerprint key to the (rotatable) active key.
	if _, err := NewAccountCipherWithKeys("v2", map[string]string{"v2": key(2), "v3": key(3)}, ""); !errors.Is(err, ErrFingerprintKeyRequired) {
		t.Fatalf("want ErrFingerprintKeyRequired, got %v", err)
	}
}

func TestFingerprint_StableAcrossActiveKey_NoV1(t *testing.T) {
	// With an explicit pepper and no v1, rotating the active key from v2 to v3
	// must not change the fingerprint.
	keys := map[string]string{"v2": key(2), "v3": key(3)}
	fpV2, err := NewAccountCipherWithKeys("v2", keys, key(9))
	if err != nil {
		t.Fatalf("cipher v2 active: %v", err)
	}
	fpV3, err := NewAccountCipherWithKeys("v3", keys, key(9))
	if err != nil {
		t.Fatalf("cipher v3 active: %v", err)
	}
	const acct = "0123456789|058"
	if fpV2.Fingerprint(acct) != fpV3.Fingerprint(acct) {
		t.Fatal("fingerprint changed across active-key rotation without v1")
	}
}

func TestDecrypt_EmptyVersionFails(t *testing.T) {
	c, err := NewAccountCipher(key(0))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	if _, err := c.Decrypt(CipherEnvelope{Ciphertext: []byte("x")}); !errors.Is(err, ErrEmptyKeyVersion) {
		t.Fatalf("want ErrEmptyKeyVersion, got %v", err)
	}
}

func TestNewAccountCipherWithKeys_ActiveMustExist(t *testing.T) {
	_, err := NewAccountCipherWithKeys("v3", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if !errors.Is(err, ErrActiveKeyMissing) {
		t.Fatalf("want ErrActiveKeyMissing, got %v", err)
	}
}

func TestFingerprint_StableAcrossActiveKey(t *testing.T) {
	// The fingerprint defaults to the v1 key. Changing the active key from v1 to
	// v2 must not change fingerprints, or the uniqueness index would break.
	fpV1, err := NewAccountCipherWithKeys("v1", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if err != nil {
		t.Fatalf("cipher v1 active: %v", err)
	}
	fpV2, err := NewAccountCipherWithKeys("v2", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if err != nil {
		t.Fatalf("cipher v2 active: %v", err)
	}
	single, err := NewAccountCipher(key(1))
	if err != nil {
		t.Fatalf("single cipher: %v", err)
	}

	const acct = "0123456789|058"
	a, b, s := fpV1.Fingerprint(acct), fpV2.Fingerprint(acct), single.Fingerprint(acct)
	if a != b || a != s {
		t.Fatalf("fingerprints diverged: v1active=%q v2active=%q single=%q", a, b, s)
	}
	if strings.TrimSpace(a) == "" {
		t.Fatal("fingerprint is empty")
	}
}

func TestFingerprint_ExplicitPepperOverrides(t *testing.T) {
	def, err := NewAccountCipherWithKeys("v1", map[string]string{"v1": key(1)}, "")
	if err != nil {
		t.Fatalf("default cipher: %v", err)
	}
	pep, err := NewAccountCipherWithKeys("v1", map[string]string{"v1": key(1)}, key(9))
	if err != nil {
		t.Fatalf("peppered cipher: %v", err)
	}
	if def.Fingerprint("x") == pep.Fingerprint("x") {
		t.Fatal("explicit fingerprint key should change the fingerprint")
	}
}
