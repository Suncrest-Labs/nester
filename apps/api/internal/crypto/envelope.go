package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Envelope encryption.
//
// The existing AccountCipher seals plaintext directly under a versioned master
// key. That is cryptographically sound, but it ties rotation cost to data
// volume: changing the master key means decrypting and re-encrypting every
// account number, with plaintext transiting the rotator for every row.
//
// Envelope encryption breaks that coupling:
//
//	per-record data key (DEK)  ── encrypts ──▶  the sensitive field
//	            │
//	    wrapped by master key (KEK)  ──▶  stored alongside, with its key version
//
// Rotating the master key then means unwrapping and rewrapping a 32-byte data
// key per row. The record ciphertext is never touched and the plaintext is
// never recovered, which is what makes both routine and emergency rotation
// cheap enough to actually perform.
//
// Both layers use AES-256-GCM from the standard library. No cryptography is
// invented here: the construction is the standard envelope pattern, and the
// primitives are the vetted stdlib ones already used elsewhere in this package.

const (
	// dataKeyLen is the size of a per-record data encryption key.
	dataKeyLen = 32

	// envelopeVersionPrefix tags the wire format so a future format change can
	// be distinguished from corruption rather than guessed at.
	envelopeVersionPrefix = "v1"
)

var (
	ErrInvalidDataKey     = errors.New("data key must be 32 bytes")
	ErrWrappedKeyTooShort = errors.New("wrapped data key is too short")
	ErrEnvelopeMalformed  = errors.New("envelope is malformed")
)

// SealedRecord is one envelope-encrypted value as it is stored.
//
// The three fields travel together and are meaningless apart: WrappedDataKey
// can only be unwrapped by the master key named in KeyVersion, and Ciphertext
// can only be opened by the data key that unwrapping yields.
type SealedRecord struct {
	// KeyVersion names the master key that wrapped the data key. This is the
	// existing key_version column: rotation changes this field and
	// WrappedDataKey, and leaves Ciphertext untouched.
	KeyVersion string

	// WrappedDataKey is the per-record data key sealed under the master key.
	WrappedDataKey []byte

	// Ciphertext is the record data sealed under the data key.
	Ciphertext []byte
}

// EnvelopeCipher performs envelope encryption over a set of versioned master
// keys.
//
// It reuses the same key material and version labels as AccountCipher, so a
// deployment configures one key set and both ciphers resolve versions from it.
// That is deliberate: introducing a second, independently-configured key set
// would double the number of things that must be rotated correctly.
type EnvelopeCipher struct {
	masterKeys    map[string]cipher.AEAD
	activeVersion string
}

// NewEnvelopeCipher builds an envelope cipher over base64-encoded master keys.
//
//   - keysB64 maps a version label to a base64-encoded 32-byte master key.
//   - activeVersion selects the key that wraps newly generated data keys.
//
// Every configured version can unwrap; only the active version wraps. That
// asymmetry is what allows mixed-version reads during a rotation.
func NewEnvelopeCipher(activeVersion string, keysB64 map[string]string) (*EnvelopeCipher, error) {
	activeVersion = strings.TrimSpace(activeVersion)
	if len(keysB64) == 0 {
		return nil, ErrNoKeys
	}
	if activeVersion == "" {
		return nil, ErrEmptyKeyVersion
	}

	keys := make(map[string]cipher.AEAD, len(keysB64))
	for version, b64 := range keysB64 {
		version = strings.TrimSpace(version)
		if version == "" {
			return nil, ErrEmptyKeyVersion
		}
		raw, err := decodeKey(b64)
		if err != nil {
			return nil, fmt.Errorf("master key %q: %w", version, err)
		}
		aead, err := newAEAD(raw)
		if err != nil {
			return nil, fmt.Errorf("master key %q: %w", version, err)
		}
		keys[version] = aead
	}

	if _, ok := keys[activeVersion]; !ok {
		return nil, ErrActiveKeyMissing
	}

	return &EnvelopeCipher{masterKeys: keys, activeVersion: activeVersion}, nil
}

// ActiveVersion returns the master key version used to wrap new data keys.
func (c *EnvelopeCipher) ActiveVersion() string {
	return c.activeVersion
}

// KnownVersions reports every master key version this cipher can unwrap.
//
// Rotation tooling uses it to confirm that a version still referenced by stored
// rows is configured, before an operator retires a key that rows still need.
func (c *EnvelopeCipher) KnownVersions() []string {
	out := make([]string, 0, len(c.masterKeys))
	for v := range c.masterKeys {
		out = append(out, v)
	}
	return out
}

// Seal encrypts plaintext under a freshly generated per-record data key and
// wraps that key under the active master key.
//
// A new data key is generated for every call. Reusing one across records would
// mean a single compromised data key exposed more than its own record, and
// would reintroduce the nonce-reuse hazard that per-record keys avoid.
func (c *EnvelopeCipher) Seal(plaintext string, aad []byte) (SealedRecord, error) {
	dataKey := make([]byte, dataKeyLen)
	if _, err := io.ReadFull(rand.Reader, dataKey); err != nil {
		return SealedRecord{}, fmt.Errorf("generate data key: %w", err)
	}
	// The data key is zeroed before returning regardless of outcome. Go's GC
	// gives no guarantee the memory is not later observable, but clearing the
	// only live reference is cheap and narrows the window.
	defer zero(dataKey)

	dataAEAD, err := newAEAD(dataKey)
	if err != nil {
		return SealedRecord{}, fmt.Errorf("data key cipher: %w", err)
	}

	// Seal the record under the data key. The AAD binds the ciphertext to its
	// context (for example the owning row's identity), so a ciphertext moved
	// to a different row fails authentication rather than decrypting into the
	// wrong record.
	recordNonce := make([]byte, dataAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, recordNonce); err != nil {
		return SealedRecord{}, fmt.Errorf("generate record nonce: %w", err)
	}
	ciphertext := dataAEAD.Seal(recordNonce, recordNonce, []byte(plaintext), aad)

	wrapped, err := c.wrapDataKey(dataKey, c.activeVersion)
	if err != nil {
		return SealedRecord{}, err
	}

	return SealedRecord{
		KeyVersion:     c.activeVersion,
		WrappedDataKey: wrapped,
		Ciphertext:     ciphertext,
	}, nil
}

// Open reverses Seal.
//
// It resolves the master key named by rec.KeyVersion — which may be any
// configured version, not only the active one. That is what makes reads work
// against a dataset mid-rotation, where rows exist under both the old and new
// master key versions simultaneously.
func (c *EnvelopeCipher) Open(rec SealedRecord, aad []byte) (string, error) {
	dataKey, err := c.unwrapDataKey(rec.WrappedDataKey, rec.KeyVersion)
	if err != nil {
		return "", err
	}
	defer zero(dataKey)

	dataAEAD, err := newAEAD(dataKey)
	if err != nil {
		return "", fmt.Errorf("data key cipher: %w", err)
	}
	if len(rec.Ciphertext) < dataAEAD.NonceSize() {
		return "", ErrCiphertextTooShort
	}
	nonce := rec.Ciphertext[:dataAEAD.NonceSize()]
	body := rec.Ciphertext[dataAEAD.NonceSize():]

	plain, err := dataAEAD.Open(nil, nonce, body, aad)
	if err != nil {
		// The GCM authentication tag failed. This is returned as-is rather
		// than falling back to any other key or to plaintext: a failed tag
		// means the ciphertext, the AAD, or the data key is wrong, and every
		// one of those is a reason to refuse rather than to guess.
		return "", fmt.Errorf("open record: %w", err)
	}
	return string(plain), nil
}

// Rewrap moves a record's data key from its current master key version to the
// active one, without touching the record ciphertext.
//
// This is the operation that makes rotation cheap. It unwraps the data key
// under the old master key and rewraps it under the active one; the record
// ciphertext and its nonce are unchanged, and the plaintext is never recovered.
//
// Rewrapping a record already on the active version is a no-op that returns the
// record unchanged, which is what makes a rotation run idempotent and safe to
// retry after a partial failure.
func (c *EnvelopeCipher) Rewrap(rec SealedRecord) (SealedRecord, error) {
	if strings.TrimSpace(rec.KeyVersion) == c.activeVersion {
		return rec, nil
	}

	dataKey, err := c.unwrapDataKey(rec.WrappedDataKey, rec.KeyVersion)
	if err != nil {
		return SealedRecord{}, err
	}
	defer zero(dataKey)

	wrapped, err := c.wrapDataKey(dataKey, c.activeVersion)
	if err != nil {
		return SealedRecord{}, err
	}

	return SealedRecord{
		KeyVersion:     c.activeVersion,
		WrappedDataKey: wrapped,
		Ciphertext:     rec.Ciphertext,
	}, nil
}

// wrapDataKey seals a data key under the named master key.
func (c *EnvelopeCipher) wrapDataKey(dataKey []byte, version string) ([]byte, error) {
	if len(dataKey) != dataKeyLen {
		return nil, ErrInvalidDataKey
	}
	aead, ok := c.masterKeys[version]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyVersion, version)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate wrap nonce: %w", err)
	}
	// The key version is authenticated as additional data. A wrapped key whose
	// stored version has been altered therefore fails to unwrap rather than
	// being attempted against the wrong master key.
	return aead.Seal(nonce, nonce, dataKey, []byte(envelopeVersionPrefix+":"+version)), nil
}

// unwrapDataKey recovers a data key sealed under the named master key.
func (c *EnvelopeCipher) unwrapDataKey(wrapped []byte, version string) ([]byte, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, ErrEmptyKeyVersion
	}
	aead, ok := c.masterKeys[version]
	if !ok {
		// This is the signal that a master key was retired while rows still
		// depended on it. The error names the version so the operator knows
		// exactly which key to restore.
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyVersion, version)
	}
	if len(wrapped) < aead.NonceSize() {
		return nil, ErrWrappedKeyTooShort
	}
	nonce := wrapped[:aead.NonceSize()]
	body := wrapped[aead.NonceSize():]

	dataKey, err := aead.Open(nil, nonce, body, []byte(envelopeVersionPrefix+":"+version))
	if err != nil {
		return nil, fmt.Errorf("unwrap data key: %w", err)
	}
	if len(dataKey) != dataKeyLen {
		return nil, ErrInvalidDataKey
	}
	return dataKey, nil
}

// EncodeWrappedKey renders a wrapped data key for storage in a text column.
func EncodeWrappedKey(wrapped []byte) string {
	return base64.StdEncoding.EncodeToString(wrapped)
}

// DecodeWrappedKey parses a wrapped data key from storage.
func DecodeWrappedKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, ErrEnvelopeMalformed
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvelopeMalformed, err)
	}
	return raw, nil
}

// zero overwrites key material in place.
//
// This is a best-effort measure. Go's garbage collector may already have copied
// the slice, and the runtime offers no guarantee of erasure, so this narrows
// the exposure window rather than eliminating it. It is not a substitute for
// keeping key material out of the process in the first place, which is what the
// signing boundary does for the operator key.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
