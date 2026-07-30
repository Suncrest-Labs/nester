package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// LegacyKeyVersion labels ciphertext written before key versioning existed.
	// The migration that introduces the key_version column defaults existing
	// rows to this value, and the single-key constructor registers its key under
	// this version so historical data keeps decrypting after the upgrade.
	LegacyKeyVersion = "v1"

	keyLen = 32
)

var (
	ErrInvalidKey         = errors.New("encryption key must be 32 bytes (base64-encoded)")
	ErrCiphertextTooShort = errors.New("ciphertext is too short")
	ErrNoKeys             = errors.New("no encryption keys configured")
	ErrActiveKeyMissing   = errors.New("active key version is not present in the key set")
	ErrUnknownKeyVersion  = errors.New("no key registered for the ciphertext key version")
	ErrEmptyKeyVersion    = errors.New("key version must not be empty")
	// ErrFingerprintKeyRequired is returned when no v1 key and no explicit
	// fingerprint pepper are provided. Defaulting the pepper to the active key
	// would let it change on every rotation and break blind-index uniqueness.
	ErrFingerprintKeyRequired = errors.New("a fingerprint key is required when no v1 key is configured")
)

// CipherEnvelope pairs stored ciphertext with the key version that produced it.
// Persisting the version alongside the bytes is what makes non-destructive key
// rotation possible: any historical version can still be resolved for
// decryption while new writes use the active key.
type CipherEnvelope struct {
	KeyVersion string
	Ciphertext []byte
}

// AccountCipher encrypts and decrypts sensitive account numbers at rest using a
// set of versioned AES-256-GCM keys. New data is sealed with the active key;
// any registered key version can decrypt. Uniqueness fingerprints use a stable
// key that is independent of the active encryption key, so blind-index lookups
// keep matching across a rotation.
type AccountCipher struct {
	keys           map[string]cipher.AEAD
	activeVersion  string
	fingerprintKey []byte
}

// NewAccountCipher builds a single-key cipher from a base64-encoded 32-byte key.
// The key is registered under LegacyKeyVersion and used for both encryption and
// fingerprints. Retained for backward compatibility with single-key
// deployments and callers that predate multi-key rotation.
func NewAccountCipher(keyB64 string) (*AccountCipher, error) {
	return NewAccountCipherWithKeys(LegacyKeyVersion, map[string]string{LegacyKeyVersion: keyB64}, "")
}

// NewAccountCipherWithKeys builds a multi-version cipher.
//
//   - keysB64 maps a version label (e.g. "v1", "v2") to a base64-encoded 32-byte key.
//   - activeVersion selects the key used for new encryptions; it must exist in keysB64.
//   - fingerprintKeyB64 is the stable pepper for uniqueness fingerprints. When empty
//     it defaults to the LegacyKeyVersion (v1) key, preserving fingerprints written
//     before a rotation. If neither a pepper nor a v1 key is present, construction
//     fails with ErrFingerprintKeyRequired rather than tracking the active key.
func NewAccountCipherWithKeys(activeVersion string, keysB64 map[string]string, fingerprintKeyB64 string) (*AccountCipher, error) {
	activeVersion = strings.TrimSpace(activeVersion)
	if len(keysB64) == 0 {
		return nil, ErrNoKeys
	}
	if activeVersion == "" {
		return nil, ErrEmptyKeyVersion
	}

	keys := make(map[string]cipher.AEAD, len(keysB64))
	rawByVersion := make(map[string][]byte, len(keysB64))
	for version, b64 := range keysB64 {
		version = strings.TrimSpace(version)
		if version == "" {
			return nil, ErrEmptyKeyVersion
		}
		raw, err := decodeKey(b64)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", version, err)
		}
		aead, err := newAEAD(raw)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", version, err)
		}
		keys[version] = aead
		rawByVersion[version] = raw
	}

	if _, ok := keys[activeVersion]; !ok {
		return nil, ErrActiveKeyMissing
	}

	fpKey, err := resolveFingerprintKey(fingerprintKeyB64, rawByVersion)
	if err != nil {
		return nil, err
	}

	return &AccountCipher{
		keys:           keys,
		activeVersion:  activeVersion,
		fingerprintKey: fpKey,
	}, nil
}

// ActiveVersion returns the key version used for new encryptions.
func (c *AccountCipher) ActiveVersion() string {
	return c.activeVersion
}

// Encrypt seals plaintext with the active key and tags the result with the
// active key version so it can be resolved for decryption later.
func (c *AccountCipher) Encrypt(plaintext string) (CipherEnvelope, error) {
	aead := c.keys[c.activeVersion]
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return CipherEnvelope{}, err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return CipherEnvelope{KeyVersion: c.activeVersion, Ciphertext: sealed}, nil
}

// Decrypt resolves the key for env.KeyVersion and reverses Encrypt. It returns
// ErrUnknownKeyVersion when no key is registered for the ciphertext's version,
// which is the signal that a required legacy key was dropped too early.
func (c *AccountCipher) Decrypt(env CipherEnvelope) (string, error) {
	version := strings.TrimSpace(env.KeyVersion)
	if version == "" {
		return "", ErrEmptyKeyVersion
	}
	aead, ok := c.keys[version]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownKeyVersion, version)
	}
	if len(env.Ciphertext) < aead.NonceSize() {
		return "", ErrCiphertextTooShort
	}
	nonce := env.Ciphertext[:aead.NonceSize()]
	body := env.Ciphertext[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Fingerprint returns a stable HMAC for uniqueness checks without decryption.
// It is keyed independently of the active encryption key so that rows created
// before and after a rotation continue to collide on the same account number.
func (c *AccountCipher) Fingerprint(normalizedAccount string) string {
	mac := hmac.New(sha256.New, c.fingerprintKey)
	_, _ = mac.Write([]byte(normalizedAccount))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func resolveFingerprintKey(b64 string, rawByVersion map[string][]byte) ([]byte, error) {
	if strings.TrimSpace(b64) != "" {
		raw, err := decodeKey(b64)
		if err != nil {
			return nil, fmt.Errorf("fingerprint key: %w", err)
		}
		return raw, nil
	}
	if raw, ok := rawByVersion[LegacyKeyVersion]; ok {
		return raw, nil
	}
	return nil, ErrFingerprintKeyRequired
}

func decodeKey(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, ErrInvalidKey
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(key) != keyLen {
		return nil, ErrInvalidKey
	}
	return key, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return aead, nil
}
