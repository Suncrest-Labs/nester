package stellar

import (
	"strings"
	"testing"
)

const validHorizonMultisigJSON = `{
  "id": "GADMIN7NESTERMAINNETMULTISIG3OF5ACCOUNTADDRESS7777777777",
  "account_id": "GADMIN7NESTERMAINNETMULTISIG3OF5ACCOUNTADDRESS7777777777",
  "thresholds": {
    "low_threshold": 3,
    "med_threshold": 3,
    "high_threshold": 3
  },
  "signers": [
    {
      "weight": 0,
      "key": "GADMIN7NESTERMAINNETMULTISIG3OF5ACCOUNTADDRESS7777777777",
      "type": "ed25519_public_key"
    },
    {
      "weight": 1,
      "key": "GSIGNER1COREMAINTAINERA777777777777777777777777777777777",
      "type": "ed25519_public_key"
    },
    {
      "weight": 1,
      "key": "GSIGNER2COREMAINTAINERB777777777777777777777777777777777",
      "type": "ed25519_public_key"
    },
    {
      "weight": 1,
      "key": "GSIGNER3SECURITYCOUNCIL777777777777777777777777777777777",
      "type": "ed25519_public_key"
    },
    {
      "weight": 1,
      "key": "GSIGNER4INDEPENDENTADVR777777777777777777777777777777777",
      "type": "ed25519_public_key"
    },
    {
      "weight": 1,
      "key": "GSIGNER5CUSTODYPROVIDER777777777777777777777777777777777",
      "type": "ed25519_public_key"
    }
  ]
}`

func expectedMainnetMultisigPolicy() MultisigPolicy {
	return MultisigPolicy{
		AdminAccount:      "GADMIN7NESTERMAINNETMULTISIG3OF5ACCOUNTADDRESS7777777777",
		RequiredM:         3,
		RequiredN:         5,
		ExpectedLowThresh: 3,
		ExpectedSigners: []string{
			"GSIGNER1COREMAINTAINERA777777777777777777777777777777777",
			"GSIGNER2COREMAINTAINERB777777777777777777777777777777777",
			"GSIGNER3SECURITYCOUNCIL777777777777777777777777777777777",
			"GSIGNER4INDEPENDENTADVR777777777777777777777777777777777",
			"GSIGNER5CUSTODYPROVIDER777777777777777777777777777777777",
		},
	}
}

func TestVerifyMultisigAccount_Valid3Of5Passes(t *testing.T) {
	if err := VerifyMultisigAccount([]byte(validHorizonMultisigJSON), expectedMainnetMultisigPolicy()); err != nil {
		t.Fatalf("expected valid 3-of-5 multisig to pass, got: %v", err)
	}
}

func TestVerifyMultisigAccount_NonZeroMasterWeightFails(t *testing.T) {
	bad := strings.Replace(validHorizonMultisigJSON, `"weight": 0`, `"weight": 1`, 1)
	err := VerifyMultisigAccount([]byte(bad), expectedMainnetMultisigPolicy())
	if err == nil || !strings.Contains(err.Error(), "master key") {
		t.Fatalf("expected master key weight error, got: %v", err)
	}
}

func TestVerifyMultisigAccount_LowMedOrHighThresholdFails(t *testing.T) {
	bad := strings.Replace(validHorizonMultisigJSON, `"med_threshold": 3`, `"med_threshold": 1`, 1)
	err := VerifyMultisigAccount([]byte(bad), expectedMainnetMultisigPolicy())
	if err == nil || !strings.Contains(err.Error(), "med_threshold mismatch") {
		t.Fatalf("expected med_threshold mismatch error, got: %v", err)
	}
}

func TestVerifyMultisigAccount_OverweightSignerFails(t *testing.T) {
	bad := strings.Replace(validHorizonMultisigJSON, `"weight": 1`, `"weight": 3`, 1)
	err := VerifyMultisigAccount([]byte(bad), expectedMainnetMultisigPolicy())
	if err == nil || !strings.Contains(err.Error(), "expected 1") {
		t.Fatalf("expected signer weight error, got: %v", err)
	}
}
