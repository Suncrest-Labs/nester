package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// randomKeyB64 generates a fresh base64 32-byte key.
//
// Keys are generated rather than hard-coded so that no test key can be mistaken
// for, or copied into, a real configuration.
func randomKeyB64(t *testing.T) string {
	t.Helper()
	raw := make([]byte, keyLen)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func newTestEnvelopeCipher(t *testing.T, active string, versions ...string) (*EnvelopeCipher, map[string]string) {
	t.Helper()
	keys := make(map[string]string, len(versions))
	for _, v := range versions {
		keys[v] = randomKeyB64(t)
	}
	c, err := NewEnvelopeCipher(active, keys)
	if err != nil {
		t.Fatalf("build envelope cipher: %v", err)
	}
	return c, keys
}

func TestEnvelopeSealOpenRoundTrip(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")

	const plaintext = "0123456789"
	aad := []byte("account:42")

	sealed, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed.KeyVersion != "v1" {
		t.Fatalf("expected key version v1, got %q", sealed.KeyVersion)
	}
	if len(sealed.WrappedDataKey) == 0 {
		t.Fatal("no wrapped data key was produced")
	}

	opened, err := c.Open(sealed, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != plaintext {
		t.Fatalf("round trip corrupted the value: got %q want %q", opened, plaintext)
	}
}

func TestEnvelopeCiphertextDoesNotContainPlaintext(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	const plaintext = "SENSITIVE-ACCOUNT-NUMBER"

	sealed, err := c.Seal(plaintext, nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed.Ciphertext, []byte(plaintext)) {
		t.Fatal("ciphertext contains the plaintext")
	}
	if bytes.Contains(sealed.WrappedDataKey, []byte(plaintext)) {
		t.Fatal("wrapped data key contains the plaintext")
	}
}

func TestEnvelopeUsesDistinctDataKeyPerRecord(t *testing.T) {
	// A shared data key would mean one compromised key exposed more than its
	// own record, and would reintroduce nonce-reuse risk.
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")

	a, err := c.Seal("same-value", nil)
	if err != nil {
		t.Fatalf("seal a: %v", err)
	}
	b, err := c.Seal("same-value", nil)
	if err != nil {
		t.Fatalf("seal b: %v", err)
	}

	if bytes.Equal(a.WrappedDataKey, b.WrappedDataKey) {
		t.Fatal("two records share a wrapped data key")
	}
	// Identical plaintext must not produce identical ciphertext.
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("identical plaintexts produced identical ciphertexts")
	}
}

func TestEnvelopeCorruptedCiphertextRejected(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Flip a bit in the authenticated ciphertext. GCM must refuse it rather
	// than returning corrupted plaintext.
	corrupted := sealed
	corrupted.Ciphertext = append([]byte(nil), sealed.Ciphertext...)
	corrupted.Ciphertext[len(corrupted.Ciphertext)-1] ^= 0x01

	if _, err := c.Open(corrupted, nil); err == nil {
		t.Fatal("corrupted ciphertext was accepted")
	}
}

func TestEnvelopeCorruptedWrappedKeyRejected(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	corrupted := sealed
	corrupted.WrappedDataKey = append([]byte(nil), sealed.WrappedDataKey...)
	corrupted.WrappedDataKey[len(corrupted.WrappedDataKey)-1] ^= 0x01

	if _, err := c.Open(corrupted, nil); err == nil {
		t.Fatal("a corrupted wrapped data key was accepted")
	}
}

func TestEnvelopeTamperedKeyVersionRejected(t *testing.T) {
	// The key version is authenticated as additional data when wrapping, so
	// altering the stored version must fail rather than being attempted
	// against the wrong master key.
	c, _ := newTestEnvelopeCipher(t, "v2", "v1", "v2")

	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed.KeyVersion != "v2" {
		t.Fatalf("expected active version v2, got %q", sealed.KeyVersion)
	}

	tampered := sealed
	tampered.KeyVersion = "v1" // a configured version, but not the one that wrapped
	if _, err := c.Open(tampered, nil); err == nil {
		t.Fatal("a record with an altered key version was decrypted")
	}
}

func TestEnvelopeWrongAADRejected(t *testing.T) {
	// AAD binds a ciphertext to its context. A ciphertext moved to a different
	// row must fail rather than decrypt into the wrong record.
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")

	sealed, err := c.Seal("value", []byte("account:1"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := c.Open(sealed, []byte("account:2")); err == nil {
		t.Fatal("a ciphertext opened under the wrong AAD")
	}
}

func TestEnvelopeUnknownKeyVersionRejected(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	sealed.KeyVersion = "v99"
	_, err = c.Open(sealed, nil)
	if !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("expected ErrUnknownKeyVersion, got %v", err)
	}
}

func TestEnvelopeMissingKeyVersionRejected(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	sealed.KeyVersion = ""
	if _, err := c.Open(sealed, nil); !errors.Is(err, ErrEmptyKeyVersion) {
		t.Fatalf("expected ErrEmptyKeyVersion, got %v", err)
	}
}

func TestEnvelopeTruncatedInputsRejected(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	t.Run("truncated wrapped key", func(t *testing.T) {
		short := sealed
		short.WrappedDataKey = sealed.WrappedDataKey[:4]
		if _, err := c.Open(short, nil); !errors.Is(err, ErrWrappedKeyTooShort) {
			t.Fatalf("expected ErrWrappedKeyTooShort, got %v", err)
		}
	})

	t.Run("truncated ciphertext", func(t *testing.T) {
		short := sealed
		short.Ciphertext = sealed.Ciphertext[:4]
		if _, err := c.Open(short, nil); err == nil {
			t.Fatal("a truncated ciphertext was accepted")
		}
	})
}

func TestEnvelopeMixedVersionReads(t *testing.T) {
	// The property rotation depends on: a dataset containing rows under both
	// the old and new master key versions must read correctly throughout.
	keys := map[string]string{"v1": randomKeyB64(t), "v2": randomKeyB64(t)}

	oldCipher, err := NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1 cipher: %v", err)
	}
	newCipher, err := NewEnvelopeCipher("v2", keys)
	if err != nil {
		t.Fatalf("build v2 cipher: %v", err)
	}

	underV1, err := oldCipher.Seal("written-before-rotation", nil)
	if err != nil {
		t.Fatalf("seal v1: %v", err)
	}
	underV2, err := newCipher.Seal("written-after-rotation", nil)
	if err != nil {
		t.Fatalf("seal v2: %v", err)
	}

	// The post-rotation cipher must read both.
	got1, err := newCipher.Open(underV1, nil)
	if err != nil {
		t.Fatalf("post-rotation cipher could not read a v1 record: %v", err)
	}
	if got1 != "written-before-rotation" {
		t.Fatalf("v1 record decrypted incorrectly: %q", got1)
	}

	got2, err := newCipher.Open(underV2, nil)
	if err != nil {
		t.Fatalf("post-rotation cipher could not read a v2 record: %v", err)
	}
	if got2 != "written-after-rotation" {
		t.Fatalf("v2 record decrypted incorrectly: %q", got2)
	}
}

func TestEnvelopeRewrapPreservesCiphertextAndPlaintext(t *testing.T) {
	// The defining property of rewrap rotation: the record ciphertext is
	// untouched, only the wrapped data key changes.
	keys := map[string]string{"v1": randomKeyB64(t), "v2": randomKeyB64(t)}

	oldCipher, err := NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1: %v", err)
	}
	newCipher, err := NewEnvelopeCipher("v2", keys)
	if err != nil {
		t.Fatalf("build v2: %v", err)
	}

	const plaintext = "rotate-me"
	sealed, err := oldCipher.Seal(plaintext, nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	rewrapped, err := newCipher.Rewrap(sealed)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}

	if rewrapped.KeyVersion != "v2" {
		t.Fatalf("rewrap did not move the record to v2: %q", rewrapped.KeyVersion)
	}
	if !bytes.Equal(rewrapped.Ciphertext, sealed.Ciphertext) {
		t.Fatal("rewrap modified the record ciphertext; it should only rewrap the data key")
	}
	if bytes.Equal(rewrapped.WrappedDataKey, sealed.WrappedDataKey) {
		t.Fatal("rewrap did not change the wrapped data key")
	}

	opened, err := newCipher.Open(rewrapped, nil)
	if err != nil {
		t.Fatalf("open after rewrap: %v", err)
	}
	if opened != plaintext {
		t.Fatalf("rewrap corrupted the value: %q", opened)
	}
}

func TestEnvelopeRewrapIsIdempotent(t *testing.T) {
	// Rotation must be safe to re-run after a partial failure.
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	once, err := c.Rewrap(sealed)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	twice, err := c.Rewrap(once)
	if err != nil {
		t.Fatalf("second rewrap: %v", err)
	}

	if !bytes.Equal(once.WrappedDataKey, twice.WrappedDataKey) {
		t.Fatal("rewrapping an already-current record changed it")
	}
	if once.KeyVersion != twice.KeyVersion {
		t.Fatal("rewrapping an already-current record changed its version")
	}
}

func TestEnvelopeRewrapFailsForRetiredKey(t *testing.T) {
	// Retiring a key that rows still reference is the one unrecoverable
	// mistake in this design. It must fail loudly, naming the version.
	keys := map[string]string{"v1": randomKeyB64(t), "v2": randomKeyB64(t)}
	oldCipher, err := NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1: %v", err)
	}
	sealed, err := oldCipher.Seal("orphaned", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// v1 has been dropped from the configuration while a row still uses it.
	withoutV1, err := NewEnvelopeCipher("v2", map[string]string{"v2": keys["v2"]})
	if err != nil {
		t.Fatalf("build v2-only: %v", err)
	}

	_, err = withoutV1.Rewrap(sealed)
	if !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("expected ErrUnknownKeyVersion, got %v", err)
	}
	if !strings.Contains(err.Error(), "v1") {
		t.Fatalf("error does not name the missing version: %v", err)
	}
}

func TestEnvelopeConstructionValidation(t *testing.T) {
	valid := randomKeyB64(t)

	t.Run("no keys", func(t *testing.T) {
		if _, err := NewEnvelopeCipher("v1", nil); !errors.Is(err, ErrNoKeys) {
			t.Fatalf("expected ErrNoKeys, got %v", err)
		}
	})

	t.Run("empty active version", func(t *testing.T) {
		if _, err := NewEnvelopeCipher("", map[string]string{"v1": valid}); !errors.Is(err, ErrEmptyKeyVersion) {
			t.Fatalf("expected ErrEmptyKeyVersion, got %v", err)
		}
	})

	t.Run("active version not present", func(t *testing.T) {
		if _, err := NewEnvelopeCipher("v9", map[string]string{"v1": valid}); !errors.Is(err, ErrActiveKeyMissing) {
			t.Fatalf("expected ErrActiveKeyMissing, got %v", err)
		}
	})

	t.Run("malformed key", func(t *testing.T) {
		if _, err := NewEnvelopeCipher("v1", map[string]string{"v1": "not-base64!!"}); err == nil {
			t.Fatal("a malformed key was accepted")
		}
	})

	t.Run("wrong length key", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("too-short"))
		if _, err := NewEnvelopeCipher("v1", map[string]string{"v1": short}); err == nil {
			t.Fatal("an undersized key was accepted")
		}
	})
}

func TestEnvelopeKnownVersions(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v2", "v1", "v2", "v3")
	versions := c.KnownVersions()
	if len(versions) != 3 {
		t.Fatalf("expected 3 known versions, got %d", len(versions))
	}
}

func TestWrappedKeyEncoding(t *testing.T) {
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")
	sealed, err := c.Seal("value", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	encoded := EncodeWrappedKey(sealed.WrappedDataKey)
	decoded, err := DecodeWrappedKey(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(decoded, sealed.WrappedDataKey) {
		t.Fatal("wrapped key did not survive the encoding round trip")
	}

	if _, err := DecodeWrappedKey(""); !errors.Is(err, ErrEnvelopeMalformed) {
		t.Fatalf("expected ErrEnvelopeMalformed for empty input, got %v", err)
	}
	if _, err := DecodeWrappedKey("not base64 !!"); !errors.Is(err, ErrEnvelopeMalformed) {
		t.Fatalf("expected ErrEnvelopeMalformed for malformed input, got %v", err)
	}
}

func TestEnvelopeNeverFallsBackToPlaintext(t *testing.T) {
	// A decryption failure must never degrade into returning the raw bytes.
	c, _ := newTestEnvelopeCipher(t, "v1", "v1")

	bogus := SealedRecord{
		KeyVersion:     "v1",
		WrappedDataKey: []byte("this is not a wrapped key at all, just plain bytes"),
		Ciphertext:     []byte("PLAINTEXT-LOOKING-VALUE"),
	}
	got, err := c.Open(bogus, nil)
	if err == nil {
		t.Fatal("a bogus record was accepted")
	}
	if got != "" {
		t.Fatalf("a failed open returned data: %q", got)
	}
}
