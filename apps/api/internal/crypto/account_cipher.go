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

var (
	ErrInvalidKey       = errors.New("encryption key must be 32 bytes (base64-encoded)")
	ErrCiphertextTooShort = errors.New("ciphertext is too short")
)

// AccountCipher encrypts and decrypts sensitive account numbers at rest.
type AccountCipher struct {
	aead cipher.AEAD
	mac  []byte
}

// NewAccountCipher builds a cipher from a base64-encoded 32-byte key.
func NewAccountCipher(keyB64 string) (*AccountCipher, error) {
	keyB64 = strings.TrimSpace(keyB64)
	if keyB64 == "" {
		return nil, ErrInvalidKey
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	return &AccountCipher{
		aead: aead,
		mac:  key,
	}, nil
}

// Encrypt returns nonce || ciphertext for storage.
func (c *AccountCipher) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt.
func (c *AccountCipher) Decrypt(blob []byte) (string, error) {
	if len(blob) < c.aead.NonceSize() {
		return "", ErrCiphertextTooShort
	}
	nonce := blob[:c.aead.NonceSize()]
	ciphertext := blob[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Fingerprint returns a stable HMAC for uniqueness checks without decryption.
func (c *AccountCipher) Fingerprint(normalizedAccount string) string {
	mac := hmac.New(sha256.New, c.mac)
	_, _ = mac.Write([]byte(normalizedAccount))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
