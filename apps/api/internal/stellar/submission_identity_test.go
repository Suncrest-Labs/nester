package stellar

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/txnbuild"
)

// The whole design rests on knowing the chain's identity for a transaction
// before submitting it: the reconciler asks getTransaction about this exact
// hash. If IdentifyTransaction returned a digest of our own making — as the
// pre-existing chain_submissions scaffold did, hashing the base64 string with
// SHA-256 — the reconciler would ask about a transaction the chain has never
// heard of and every submission would reconcile to NOT_FOUND forever.
//
// These build a real signed envelope through the same txnbuild path the
// invoker uses, so they also guard the production precondition that every
// submitted transaction carries a maxTime.

func buildSignedEnvelope(t *testing.T, timeout int64) (envelope string, kp *keypair.Full) {
	t.Helper()

	kp = keypair.MustRandom()
	source := txnbuild.NewSimpleAccount(kp.Address(), 1)

	params := txnbuild.TransactionParams{
		SourceAccount:        &source,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.BumpSequence{BumpTo: 2},
		},
		BaseFee: txnbuild.MinBaseFee,
	}
	if timeout > 0 {
		params.Preconditions = txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(timeout)}
	} else {
		params.Preconditions = txnbuild.Preconditions{TimeBounds: txnbuild.NewInfiniteTimeout()}
	}

	tx, err := txnbuild.NewTransaction(params)
	if err != nil {
		t.Fatalf("build transaction: %v", err)
	}

	signed, err := tx.Sign(network.TestNetworkPassphrase, kp)
	if err != nil {
		t.Fatalf("sign transaction: %v", err)
	}

	envelope, err = signed.Base64()
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return envelope, kp
}

// The hash must be the network's canonical transaction hash — the value a
// block explorer and getTransaction both use — not a digest of the encoded
// string.
func TestIdentifyTransactionProducesTheCanonicalChainHash(t *testing.T) {
	envelope, _ := buildSignedEnvelope(t, 300)

	identity, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction: %v", err)
	}

	// Independently recompute via the SDK from the same envelope.
	generic, err := txnbuild.TransactionFromXDR(envelope)
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	inner, _ := generic.Transaction()
	want, err := inner.HashHex(network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("HashHex: %v", err)
	}

	if identity.Hash != want {
		t.Fatalf("hash = %s, want the canonical %s", identity.Hash, want)
	}
	if len(identity.Hash) != 64 {
		t.Fatalf("hash %q is not a 32-byte hex digest", identity.Hash)
	}
	// Not a hash of the string we happened to encode.
	if strings.EqualFold(identity.Hash, sha256Hex(envelope)) {
		t.Fatal("hash is a digest of the envelope string, not the chain's transaction hash")
	}
}

// The hash is network-scoped: the same envelope on a different network is a
// different transaction, and asking the wrong network's hash would never
// resolve.
func TestTransactionHashIsNetworkScoped(t *testing.T) {
	envelope, _ := buildSignedEnvelope(t, 300)

	testnet, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction(testnet): %v", err)
	}
	pubnet, err := IdentifyTransaction(envelope, network.PublicNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction(pubnet): %v", err)
	}

	if testnet.Hash == pubnet.Hash {
		t.Fatal("the same envelope hashed identically on two networks")
	}
}

// The same envelope always yields the same identity, which is what lets the
// transaction hash serve as a stable idempotency reference when no
// caller-supplied one exists.
func TestTransactionIdentityIsDeterministic(t *testing.T) {
	envelope, _ := buildSignedEnvelope(t, 300)

	first, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
		if err != nil {
			t.Fatalf("IdentifyTransaction: %v", err)
		}
		if again.Hash != first.Hash || !again.ValidUntil.Equal(first.ValidUntil) {
			t.Fatal("identity is not deterministic for the same envelope")
		}
	}
}

// The maxTime must survive the encode/sign round trip, because it is what
// turns a NOT_FOUND into proof that the transaction can never land. Every
// invoker build path sets a five-minute timeout; this is the guard that the
// value actually reaches the submission record.
func TestIdentifyTransactionExtractsTheSignedMaxTime(t *testing.T) {
	before := time.Now().UTC()
	envelope, _ := buildSignedEnvelope(t, 300)
	after := time.Now().UTC()

	identity, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction: %v", err)
	}

	if identity.ValidUntil.IsZero() {
		t.Fatal("maxTime was lost; the submission could never be proven un-landable")
	}

	// Within the window the SDK would have produced for a 300s timeout,
	// allowing a second of slack for the clock ticking during the test.
	earliest := before.Add(300 * time.Second).Add(-2 * time.Second)
	latest := after.Add(300 * time.Second).Add(2 * time.Second)
	if identity.ValidUntil.Before(earliest) || identity.ValidUntil.After(latest) {
		t.Fatalf("maxTime = %s, want within [%s, %s]", identity.ValidUntil, earliest, latest)
	}
}

// A transaction built with no upper time bound reports a zero ValidUntil, so
// send refuses it rather than creating a submission that could never be
// reconciled.
func TestIdentifyTransactionReportsAMissingTimeBound(t *testing.T) {
	envelope, _ := buildSignedEnvelope(t, 0)

	identity, err := IdentifyTransaction(envelope, network.TestNetworkPassphrase)
	if err != nil {
		t.Fatalf("IdentifyTransaction: %v", err)
	}
	if !identity.ValidUntil.IsZero() {
		t.Fatalf("ValidUntil = %s, want zero for an unbounded transaction", identity.ValidUntil)
	}

	// And such an intent is never provable, however long we wait.
	intent := SubmissionIntent{ValidUntil: identity.ValidUntil, CreatedAt: submittedAt}
	outcome := DetermineOutcome(TxStatusNotFound, chainAt(submittedAt.Add(365*24*time.Hour)), intent)
	if outcome.PermitsNewAttempt() {
		t.Fatal("an unbounded transaction was treated as expired")
	}
}

// sha256Hex reproduces the pre-existing scaffold's (incorrect) hashing — a
// SHA-256 of the encoded envelope string — so the test above can assert we
// are not doing that.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
