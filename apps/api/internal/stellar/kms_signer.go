package stellar

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

// KMSKeyProvider abstracts a non-exportable Ed25519 key stored in a Cloud KMS
// (GCP Cloud KMS, AWS KMS ECC_NIST_EDWARDS25519, HashiCorp Vault Transit, or
// PKCS#11 HSM). The private key never enters process memory; only the 32-byte
// Stellar transaction hash is sent to the KMS/HSM for signing.
type KMSKeyProvider interface {
	// KeyID returns the KMS/HSM key resource ARN or URI for audit logging.
	KeyID() string
	// PublicAddress returns the Stellar G... public key corresponding to the
	// non-exportable KMS/HSM key.
	PublicAddress() string
	// SignDigest signs a 32-byte Stellar transaction hash using PureEd25519 and
	// returns the 64-byte raw signature.
	SignDigest(ctx context.Context, digest []byte) ([]byte, error)
}

// KMSTransactionSigner implements TransactionSigner backed by a KMSKeyProvider
// so that no Stellar secret seed (S...) ever exists in process environment or RAM.
type KMSTransactionSigner struct {
	provider          KMSKeyProvider
	pubKP             *keypair.FromAddress
	networkPassphrase string
}

// NewKMSTransactionSigner constructs a TransactionSigner that delegates all
// Ed25519 signature generation to the provided KMS/HSM key provider.
func NewKMSTransactionSigner(provider KMSKeyProvider, networkPassphrase string) (*KMSTransactionSigner, error) {
	if provider == nil {
		return nil, errors.New("kms key provider is required")
	}
	if strings.TrimSpace(networkPassphrase) == "" {
		return nil, errors.New("network passphrase is required")
	}
	pubAddr := strings.TrimSpace(provider.PublicAddress())
	kp, err := keypair.ParseAddress(pubAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid kms public stellar address %q: %w", pubAddr, err)
	}
	return &KMSTransactionSigner{
		provider:          provider,
		pubKP:             kp,
		networkPassphrase: networkPassphrase,
	}, nil
}

// OperatorAddress returns the Stellar G... public address of the KMS/HSM key.
func (s *KMSTransactionSigner) OperatorAddress() string {
	return s.pubKP.Address()
}

// KeyID returns the KMS/HSM key identifier for audit correlation.
func (s *KMSTransactionSigner) KeyID() string {
	return s.provider.KeyID()
}

// SignEnvelope hashes the transaction for the configured Stellar network,
// requests a 64-byte Ed25519 signature from the KMS/HSM over the 32-byte
// transaction hash, cryptographically verifies the returned signature against
// the public key, and attaches the decorated signature to the XDR envelope.
func (s *KMSTransactionSigner) SignEnvelope(ctx context.Context, req SignRequest) (string, error) {
	generic, err := txnbuild.TransactionFromXDR(req.EnvelopeXDR)
	if err != nil {
		return "", fmt.Errorf("parse envelope: %w", err)
	}
	inner, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}

	txXDR := inner.ToXDR()
	txHash, err := network.HashTransactionInEnvelope(txXDR, s.networkPassphrase)
	if err != nil {
		return "", fmt.Errorf("hash transaction: %w", err)
	}

	sigBytes, err := s.provider.SignDigest(ctx, txHash[:])
	if err != nil {
		return "", fmt.Errorf("kms sign digest: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return "", fmt.Errorf("kms returned signature of length %d, want %d", len(sigBytes), ed25519.SignatureSize)
	}

	// Verify the KMS signature against the expected public address before
	// emitting the signed envelope. This catches key-ID/public-address
	// mismatches or corrupted responses immediately.
	if err := s.pubKP.Verify(txHash[:], sigBytes); err != nil {
		return "", fmt.Errorf("kms signature verification failed against %s: %w", s.pubKP.Address(), err)
	}

	hint := s.pubKP.Hint()
	var xdrHint xdr.SignatureHint
	copy(xdrHint[:], hint[:])

	decorated := xdr.DecoratedSignature{
		Hint:      xdrHint,
		Signature: xdr.Signature(sigBytes),
	}
	txXDR.V1.Signatures = append(txXDR.V1.Signatures, decorated)

	signedB64, err := xdr.MarshalBase64(txXDR)
	if err != nil {
		return "", fmt.Errorf("marshal signed envelope: %w", err)
	}
	return signedB64, nil
}

// HTTPKMSProvider implements KMSKeyProvider over an authenticated KMS/HSM
// signing endpoint (compatible with Vault Transit / cloud KMS proxy sidecar).
type HTTPKMSProvider struct {
	keyID         string
	publicAddress string
	endpointURL   string
	authToken     string
	httpClient    *http.Client
}

// HTTPKMSConfig configures an HTTPKMSProvider.
type HTTPKMSConfig struct {
	KeyID         string
	PublicAddress string
	EndpointURL   string
	AuthToken     string
	HTTPClient    *http.Client
}

// NewHTTPKMSProvider constructs an HTTP-backed KMS/HSM key provider.
func NewHTTPKMSProvider(cfg HTTPKMSConfig) (*HTTPKMSProvider, error) {
	if strings.TrimSpace(cfg.KeyID) == "" {
		return nil, errors.New("SIGNER_KMS_KEY_ID is required")
	}
	if strings.TrimSpace(cfg.PublicAddress) == "" {
		return nil, errors.New("SIGNER_KMS_PUBLIC_ADDRESS is required")
	}
	if _, err := keypair.ParseAddress(strings.TrimSpace(cfg.PublicAddress)); err != nil {
		return nil, fmt.Errorf("invalid SIGNER_KMS_PUBLIC_ADDRESS: %w", err)
	}
	if strings.TrimSpace(cfg.EndpointURL) == "" {
		return nil, errors.New("SIGNER_KMS_ENDPOINT_URL is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPKMSProvider{
		keyID:         strings.TrimSpace(cfg.KeyID),
		publicAddress: strings.TrimSpace(cfg.PublicAddress),
		endpointURL:   strings.TrimSpace(cfg.EndpointURL),
		authToken:     strings.TrimSpace(cfg.AuthToken),
		httpClient:    client,
	}, nil
}

func (p *HTTPKMSProvider) KeyID() string         { return p.keyID }
func (p *HTTPKMSProvider) PublicAddress() string { return p.publicAddress }

type kmsSignRequest struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	DigestB64 string `json:"digest_b64"`
}

type kmsSignResponse struct {
	SignatureB64 string `json:"signature_b64"`
}

// SignDigest sends the 32-byte transaction digest to the KMS/HSM endpoint and
// decodes the 64-byte Ed25519 signature.
func (p *HTTPKMSProvider) SignDigest(ctx context.Context, digest []byte) ([]byte, error) {
	if len(digest) != 32 {
		return nil, fmt.Errorf("expected 32-byte transaction hash, got %d bytes", len(digest))
	}
	bodyBytes, err := json.Marshal(kmsSignRequest{
		KeyID:     p.keyID,
		Algorithm: "ED25519",
		DigestB64: base64.StdEncoding.EncodeToString(digest),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal kms sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build kms http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("invoke kms endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("kms endpoint returned HTTP %d", resp.StatusCode)
	}

	var parsed kmsSignResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode kms sign response: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(parsed.SignatureB64)
	if err != nil {
		return nil, fmt.Errorf("decode base64 kms signature: %w", err)
	}
	return sig, nil
}
