package stellar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/txnbuild"
)

func buildUnsignedEnvelopeB64(t *testing.T, sourceAddress string) string {
	t.Helper()
	acct := txnbuild.NewSimpleAccount(sourceAddress, 100)
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &acct,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.BumpSequence{BumpTo: 101},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewInfiniteTimeout()},
	})
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	b64, err := tx.Base64()
	if err != nil {
		t.Fatalf("tx.Base64: %v", err)
	}
	return b64
}

func TestKMSTransactionSigner_MatchesLocalSignerWithoutKeyInProcess(t *testing.T) {
	// Generate a simulated HSM/KMS keypair that lives inside the mock KMS server,
	// never inside KMSTransactionSigner.
	hsmKey, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair.Random: %v", err)
	}

	kmsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kms-test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req kmsSignRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		digest, err := base64.StdEncoding.DecodeString(req.DigestB64)
		if err != nil || len(digest) != 32 {
			http.Error(w, "invalid digest", http.StatusBadRequest)
			return
		}
		sig, err := hsmKey.Sign(digest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsSignResponse{
			SignatureB64: base64.StdEncoding.EncodeToString(sig),
		})
	}))
	defer kmsServer.Close()

	provider, err := NewHTTPKMSProvider(HTTPKMSConfig{
		KeyID:         "gcpkms://projects/nester-prod/locations/us-east1/keyRings/mainnet/cryptoKeys/operator/cryptoKeyVersions/1",
		PublicAddress: hsmKey.Address(),
		EndpointURL:   kmsServer.URL,
		AuthToken:     "kms-test-token",
		HTTPClient:    kmsServer.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPKMSProvider: %v", err)
	}

	kmsSigner, err := NewKMSTransactionSigner(provider, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("NewKMSTransactionSigner: %v", err)
	}

	localSigner, err := NewLocalSigner(hsmKey.Seed(), network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("NewLocalSigner: %v", err)
	}

	unsignedXDR := buildUnsignedEnvelopeB64(t, hsmKey.Address())
	kmsSignedXDR, err := kmsSigner.SignEnvelope(context.Background(), SignRequest{EnvelopeXDR: unsignedXDR})
	if err != nil {
		t.Fatalf("kmsSigner.SignEnvelope: %v", err)
	}
	localSignedXDR, err := localSigner.SignEnvelope(context.Background(), SignRequest{EnvelopeXDR: unsignedXDR})
	if err != nil {
		t.Fatalf("localSigner.SignEnvelope: %v", err)
	}

	if kmsSignedXDR != localSignedXDR {
		t.Fatalf("KMS signed XDR does not match LocalSigner signed XDR:\n got: %s\nwant: %s", kmsSignedXDR, localSignedXDR)
	}
}

func TestKMSTransactionSigner_RejectsCorruptedSignatureFromKMS(t *testing.T) {
	hsmKey, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair.Random: %v", err)
	}
	wrongKey, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair.Random: %v", err)
	}

	kmsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req kmsSignRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		digest, _ := base64.StdEncoding.DecodeString(req.DigestB64)
		// Sign with the wrong key to simulate a KMS key-ID/public-address mismatch.
		sig, _ := wrongKey.Sign(digest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kmsSignResponse{
			SignatureB64: base64.StdEncoding.EncodeToString(sig),
		})
	}))
	defer kmsServer.Close()

	provider, err := NewHTTPKMSProvider(HTTPKMSConfig{
		KeyID:         "kms-key-mismatch",
		PublicAddress: hsmKey.Address(),
		EndpointURL:   kmsServer.URL,
		HTTPClient:    kmsServer.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPKMSProvider: %v", err)
	}
	kmsSigner, err := NewKMSTransactionSigner(provider, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("NewKMSTransactionSigner: %v", err)
	}

	unsignedXDR := buildUnsignedEnvelopeB64(t, hsmKey.Address())
	if _, err := kmsSigner.SignEnvelope(context.Background(), SignRequest{EnvelopeXDR: unsignedXDR}); err == nil {
		t.Fatal("expected KMSTransactionSigner to reject signature that fails public key verification")
	}
}
