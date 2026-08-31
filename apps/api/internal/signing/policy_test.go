package signing

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The tests in this file are the negative-path proof for the signing boundary.
// Each one demonstrates a specific transaction shape an attacker might try to
// get signed, and asserts it is refused with the correct category. A signer
// that accepted any of these would be a signing oracle wearing a policy.

const (
	testNetwork  = "Test SDF Network ; September 2015"
	otherNetwork = "Public Global Stellar Network ; September 2015"
	// Valid-length strkeys. The signer's structural check verifies the prefix
	// and length; full strkey decoding happens when the transaction is built.
	testContract  = "CA" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	otherContract = "CB" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testAccount   = "GA" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func testPolicy() *Policy {
	return NewPolicy(
		testNetwork,
		[]string{testContract},
		KnownOperations(),
		1_000_000_000, // 100 XLM per transaction
		2*time.Minute,
		30*time.Second,
	)
}

func validDepositIntent(now time.Time) *Intent {
	return &Intent{
		ID:                "intent-1",
		Operation:         OpDeposit,
		Shape:             ShapeI128Pair,
		ContractAddress:   testContract,
		NetworkPassphrase: testNetwork,
		Arg0:              5_000_000,
		Arg1:              0,
		IssuedAt:          now,
	}
}

// assertRejected fails the test unless err is a policy rejection of the
// expected category.
func assertRejected(t *testing.T, err error, want Rejection) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rejection %q, got nil error (the intent would have been SIGNED)", want)
	}
	if !errors.Is(err, ErrPolicyRejected) {
		t.Fatalf("expected a policy rejection, got %v", err)
	}
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PolicyError, got %T", err)
	}
	if pe.Category != want {
		t.Fatalf("expected rejection category %q, got %q (reason: %s)", want, pe.Category, pe.Reason)
	}
}

func TestValidIntentIsAccepted(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)

	if err := intent.Validate(); err != nil {
		t.Fatalf("structural validation rejected a valid intent: %v", err)
	}
	if err := testPolicy().Evaluate(intent, now); err != nil {
		t.Fatalf("policy rejected a valid intent: %v", err)
	}
}

func TestEveryKnownOperationHasAShape(t *testing.T) {
	// The operation set is the signer's entire signing surface. A new operation
	// added without a shape would fall through the shape switch, so this guards
	// the invariant directly.
	ops := KnownOperations()
	if len(ops) == 0 {
		t.Fatal("signer knows no operations at all")
	}
	for _, op := range ops {
		if _, ok := ShapeFor(op); !ok {
			t.Errorf("operation %q has no declared shape", op)
		}
	}
}

func TestUnknownOperationRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	// The classic oracle probe: ask for a function the application never calls.
	intent.Operation = "transfer_all_to_attacker"

	assertRejected(t, intent.Validate(), RejectUnknownOperation)
}

func TestOperationNotPermittedByDeploymentRejected(t *testing.T) {
	now := time.Now().UTC()
	// A deployment that permits only deposits must refuse an emergency
	// withdrawal even though the signer knows how to build one.
	policy := NewPolicy(testNetwork, []string{testContract},
		[]Operation{OpDeposit}, 1_000_000_000, time.Minute, time.Second)

	intent := &Intent{
		ID:                "intent-emergency",
		Operation:         OpEmergencyWithdrawAll,
		Shape:             ShapeVoid,
		ContractAddress:   testContract,
		NetworkPassphrase: testNetwork,
		IssuedAt:          now,
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("intent should be structurally valid: %v", err)
	}
	assertRejected(t, policy.Evaluate(intent, now), RejectUnknownOperation)
}

func TestShapeMismatchRejected(t *testing.T) {
	now := time.Now().UTC()

	t.Run("declared shape does not match operation", func(t *testing.T) {
		intent := validDepositIntent(now)
		intent.Shape = ShapeVoid // deposit requires i128_pair
		assertRejected(t, intent.Validate(), RejectShapeMismatch)
	})

	t.Run("void operation carrying smuggled arguments", func(t *testing.T) {
		// The attack this blocks: name a permitted no-argument function, then
		// attach arguments hoping the builder uses them.
		intent := &Intent{
			ID:                "intent-smuggle",
			Operation:         OpPause,
			Shape:             ShapeVoid,
			ContractAddress:   testContract,
			NetworkPassphrase: testNetwork,
			Arg0:              999_999_999,
			IssuedAt:          now,
		}
		assertRejected(t, intent.Validate(), RejectShapeMismatch)
	})

	t.Run("i128 operation carrying an address argument", func(t *testing.T) {
		intent := validDepositIntent(now)
		intent.Address = testAccount
		assertRejected(t, intent.Validate(), RejectShapeMismatch)
	})

	t.Run("weights operation carrying integer arguments", func(t *testing.T) {
		intent := &Intent{
			ID:                "intent-weights",
			Operation:         OpSetWeights,
			Shape:             ShapeWeights,
			ContractAddress:   testContract,
			NetworkPassphrase: testNetwork,
			Arg0:              1,
			Weights:           []WeightEntry{{Protocol: "a", WeightBps: 10_000}},
			IssuedAt:          now,
		}
		assertRejected(t, intent.Validate(), RejectShapeMismatch)
	})
}

func TestWrongContractRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	intent.ContractAddress = otherContract

	if err := intent.Validate(); err != nil {
		t.Fatalf("a well-formed address should pass structural validation: %v", err)
	}
	assertRejected(t, testPolicy().Evaluate(intent, now), RejectContractNotAllowed)
}

func TestEmptyContractAllowlistPermitsNothing(t *testing.T) {
	// Fail-closed: a misconfigured signer must sign nothing, not everything.
	now := time.Now().UTC()
	policy := NewPolicy(testNetwork, nil, KnownOperations(), 1_000, time.Minute, time.Second)
	assertRejected(t, policy.Evaluate(validDepositIntent(now), now), RejectContractNotAllowed)
}

func TestEmptyOperationAllowlistPermitsNothing(t *testing.T) {
	now := time.Now().UTC()
	policy := NewPolicy(testNetwork, []string{testContract}, nil, 1_000, time.Minute, time.Second)
	assertRejected(t, policy.Evaluate(validDepositIntent(now), now), RejectUnknownOperation)
}

func TestWrongNetworkRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	// A testnet signer must never produce a mainnet-valid signature.
	intent.NetworkPassphrase = otherNetwork

	assertRejected(t, testPolicy().Evaluate(intent, now), RejectNetworkMismatch)
}

func TestInvalidAddressRejected(t *testing.T) {
	now := time.Now().UTC()

	cases := map[string]string{
		"empty":                "",
		"wrong prefix":         strings.Repeat("X", 56),
		"too short":            "GABC",
		"contract not account": testContract,
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			intent := &Intent{
				ID:                "intent-harvest",
				Operation:         OpHarvest,
				Shape:             ShapeAddressBool,
				ContractAddress:   testContract,
				NetworkPassphrase: testNetwork,
				Address:           addr,
				IssuedAt:          now,
			}
			err := intent.Validate()
			if err == nil {
				t.Fatalf("address %q was accepted", addr)
			}
			var pe *PolicyError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *PolicyError, got %T", err)
			}
			if pe.Category != RejectInvalidAddress && pe.Category != RejectMalformed {
				t.Fatalf("unexpected category %q for address %q", pe.Category, addr)
			}
		})
	}
}

func TestInvalidContractAddressRejected(t *testing.T) {
	now := time.Now().UTC()
	for name, addr := range map[string]string{
		"empty":                "",
		"account not contract": testAccount,
		"too short":            "CABC",
	} {
		t.Run(name, func(t *testing.T) {
			intent := validDepositIntent(now)
			intent.ContractAddress = addr
			err := intent.Validate()
			if err == nil {
				t.Fatalf("contract address %q was accepted", addr)
			}
		})
	}
}

func TestExcessiveAmountRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	// Within policy the operation is legitimate; the amount is what makes it
	// dangerous. This is the limit that bounds damage from a compromised API
	// making otherwise entirely valid requests.
	intent.Arg0 = 1_000_000_001

	assertRejected(t, testPolicy().Evaluate(intent, now), RejectAmountOutOfPolicy)
}

func TestNonPositiveAmountRejected(t *testing.T) {
	now := time.Now().UTC()
	for name, amount := range map[string]int64{"zero": 0, "negative": -5} {
		t.Run(name, func(t *testing.T) {
			intent := validDepositIntent(now)
			intent.Arg0 = amount
			assertRejected(t, intent.Validate(), RejectAmountOutOfPolicy)
		})
	}
}

func TestNegativeSlippageGuardRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	intent.Arg1 = -1
	assertRejected(t, intent.Validate(), RejectAmountOutOfPolicy)
}

func TestExpiredIntentRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now.Add(-5 * time.Minute))

	assertRejected(t, testPolicy().Evaluate(intent, now), RejectIntentExpired)
}

func TestFutureDatedIntentBeyondSkewRejected(t *testing.T) {
	// A forged future timestamp would otherwise extend the window during which
	// a captured intent stays replayable.
	now := time.Now().UTC()
	intent := validDepositIntent(now.Add(10 * time.Minute))

	assertRejected(t, testPolicy().Evaluate(intent, now), RejectIntentExpired)
}

func TestSmallClockSkewTolerated(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now.Add(5 * time.Second))

	if err := testPolicy().Evaluate(intent, now); err != nil {
		t.Fatalf("a 5s clock skew should be tolerated, got: %v", err)
	}
}

func TestWeightsValidation(t *testing.T) {
	now := time.Now().UTC()
	base := func(weights []WeightEntry) *Intent {
		return &Intent{
			ID:                "intent-weights",
			Operation:         OpSetWeights,
			Shape:             ShapeWeights,
			ContractAddress:   testContract,
			NetworkPassphrase: testNetwork,
			Weights:           weights,
			IssuedAt:          now,
		}
	}

	t.Run("valid weights summing to 100 percent", func(t *testing.T) {
		intent := base([]WeightEntry{
			{Protocol: "blend", WeightBps: 6_000},
			{Protocol: "aqua", WeightBps: 4_000},
		})
		if err := intent.Validate(); err != nil {
			t.Fatalf("valid weights rejected: %v", err)
		}
	})

	t.Run("weights not summing to 100 percent", func(t *testing.T) {
		intent := base([]WeightEntry{{Protocol: "blend", WeightBps: 5_000}})
		assertRejected(t, intent.Validate(), RejectWeightsInvalid)
	})

	t.Run("empty weight vector", func(t *testing.T) {
		// A set_weights intent with no weights is caught by the weight
		// validator rather than the shape check: the shape is correct, the
		// vector is not.
		assertRejected(t, base(nil).Validate(), RejectWeightsInvalid)
	})

	t.Run("duplicate protocol", func(t *testing.T) {
		// Duplicates would let a caller express a total that looks valid
		// per-entry while the contract sees something different.
		intent := base([]WeightEntry{
			{Protocol: "blend", WeightBps: 5_000},
			{Protocol: "blend", WeightBps: 5_000},
		})
		assertRejected(t, intent.Validate(), RejectWeightsInvalid)
	})

	t.Run("negative weight", func(t *testing.T) {
		intent := base([]WeightEntry{
			{Protocol: "blend", WeightBps: -1},
			{Protocol: "aqua", WeightBps: 10_001},
		})
		assertRejected(t, intent.Validate(), RejectWeightsInvalid)
	})

	t.Run("oversized weight vector", func(t *testing.T) {
		many := make([]WeightEntry, 0, maxWeightEntries+1)
		for i := 0; i <= maxWeightEntries; i++ {
			many = append(many, WeightEntry{Protocol: string(rune('a'+i%26)) + itoa(int64(i)), WeightBps: 1})
		}
		assertRejected(t, base(many).Validate(), RejectWeightsInvalid)
	})
}

func TestMissingIntentIDRejected(t *testing.T) {
	now := time.Now().UTC()
	intent := validDepositIntent(now)
	intent.ID = ""
	assertRejected(t, intent.Validate(), RejectMalformed)
}

func TestMissingIssuedAtRejected(t *testing.T) {
	intent := validDepositIntent(time.Now().UTC())
	intent.IssuedAt = time.Time{}
	assertRejected(t, intent.Validate(), RejectMalformed)
}

func TestReplayGuardRejectsRepeatedIntent(t *testing.T) {
	now := time.Now().UTC()
	guard := NewReplayGuard(5 * time.Minute)

	if err := guard.Observe("intent-1", now); err != nil {
		t.Fatalf("first observation should succeed: %v", err)
	}
	// The replay: the same intent presented a second time.
	assertRejected(t, guard.Observe("intent-1", now), RejectIntentReplayed)
}

func TestReplayGuardAllowsDistinctIntents(t *testing.T) {
	now := time.Now().UTC()
	guard := NewReplayGuard(5 * time.Minute)

	for _, id := range []string{"a", "b", "c"} {
		if err := guard.Observe(id, now); err != nil {
			t.Fatalf("distinct intent %q rejected: %v", id, err)
		}
	}
	if guard.Size() != 3 {
		t.Fatalf("expected 3 retained ids, got %d", guard.Size())
	}
}

func TestReplayGuardExpiresOldEntries(t *testing.T) {
	start := time.Now().UTC()
	guard := NewReplayGuard(time.Minute)

	if err := guard.Observe("old", start); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Well past both the retention window and the sweep interval.
	later := start.Add(10 * time.Minute)
	if err := guard.Observe("new", later); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guard.Size() != 1 {
		t.Fatalf("expected the expired id to be swept, retained %d", guard.Size())
	}
}

func TestHashIntentDistinguishesDifferentIntents(t *testing.T) {
	now := time.Now().UTC()
	base := validDepositIntent(now)

	variants := map[string]func(*Intent){
		"amount":    func(i *Intent) { i.Arg0 = base.Arg0 + 1 },
		"contract":  func(i *Intent) { i.ContractAddress = otherContract },
		"operation": func(i *Intent) { i.Operation = OpWithdraw },
		"network":   func(i *Intent) { i.NetworkPassphrase = otherNetwork },
		"id":        func(i *Intent) { i.ID = "different" },
	}

	baseHash := HashIntent(base)
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			altered := validDepositIntent(now)
			mutate(altered)
			if HashIntent(altered) == baseHash {
				t.Fatalf("changing %s did not change the intent hash; the audit record could not distinguish them", name)
			}
		})
	}
}

func TestHashIntentIsStable(t *testing.T) {
	now := time.Now().UTC()
	a := validDepositIntent(now)
	b := validDepositIntent(now)
	if HashIntent(a) != HashIntent(b) {
		t.Fatal("identical intents produced different hashes")
	}
}

func TestHashIntentFieldSeparation(t *testing.T) {
	// Guards the field-tagged encoding: without separators, moving characters
	// between adjacent fields would produce the same hash.
	now := time.Now().UTC()
	a := validDepositIntent(now)
	a.Address = ""
	a.ID = "ab"

	b := validDepositIntent(now)
	b.Address = ""
	b.ID = "a"
	b.RequestID = "b"

	if HashIntent(a) == HashIntent(b) {
		t.Fatal("intents differing in field boundaries produced the same hash")
	}
}

func TestNetworkLabelDistinguishesNetworksAndNeverEchoes(t *testing.T) {
	// The intent commitment binds to this label, so two networks must not
	// collide -- otherwise an audit record could not tell a testnet intent from
	// a mainnet one.
	if NetworkLabel(testNetwork) == NetworkLabel(otherNetwork) {
		t.Fatal("two different networks produced the same label")
	}
	if NetworkLabel(testNetwork) != "testnet" {
		t.Fatalf("expected testnet, got %q", NetworkLabel(testNetwork))
	}
	if NetworkLabel(otherNetwork) != "pubnet" {
		t.Fatalf("expected pubnet, got %q", NetworkLabel(otherNetwork))
	}
	// An unrecognised value must not reach the audit record verbatim.
	unknown := "SOME-MISCONFIGURED-VALUE"
	if got := NetworkLabel(unknown); got == unknown {
		t.Fatalf("NetworkLabel echoed its input: %q", got)
	}
}
