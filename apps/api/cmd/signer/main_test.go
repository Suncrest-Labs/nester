package main

import (
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
)

func TestSignerLogsNetworkLabelNotPassphrase(t *testing.T) {
	// The mapping exists so that startup logging carries a short label rather
	// than a value whose name reads as a credential. An unrecognised passphrase
	// must report "custom" rather than echoing it, so a misconfigured value
	// cannot inject arbitrary text into the log.
	cases := map[string]string{
		"Public Global Stellar Network ; September 2015": "pubnet",
		"Test SDF Network ; September 2015":              "testnet",
		"Test SDF Future Network ; October 2022":         "futurenet",
		"":                                               "unset",
		"something someone configured by hand":           "custom",
	}
	for passphrase, want := range cases {
		if got := signing.NetworkLabel(passphrase); got != want {
			t.Errorf("signing.NetworkLabel(%q) = %q, want %q", passphrase, got, want)
		}
	}
}

func TestSignerNetworkLabelNeverEchoesInput(t *testing.T) {
	unknown := "SECRET-LOOKING-VALUE-THAT-MUST-NOT-BE-LOGGED"
	if got := signing.NetworkLabel(unknown); got == unknown {
		t.Fatalf("networkName echoed its input: %q", got)
	}
}

func TestLoadConfig_KMSModeWithoutOperatorSecretInEnv(t *testing.T) {
	t.Setenv("STELLAR_RPC_URL", "https://mainnet.sorobanrpc.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.stellar.org")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Public Global Stellar Network ; September 2015")
	t.Setenv("STELLAR_OPERATOR_SECRET", "")
	t.Setenv("SIGNER_REQUIRE_KMS", "true")
	t.Setenv("SIGNER_KMS_KEY_ID", "gcpkms://projects/nester/locations/us/keyRings/mainnet/cryptoKeys/op/cryptoKeyVersions/1")
	t.Setenv("SIGNER_KMS_PUBLIC_ADDRESS", "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7")
	t.Setenv("SIGNER_KMS_ENDPOINT_URL", "http://127.0.0.1:8200/v1/kms/sign")
	t.Setenv("SIGNER_KILL_SWITCH_PATH", "/run/nester/killswitch")
	t.Setenv("SIGNER_SOCKET_PATH", "/run/nester/signer.sock")
	t.Setenv("SIGNER_ALLOWED_CONTRACTS", "CF6F8NESTERVAULTMAINNET0000000000000000000000000000000000")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed in KMS mode: %v", err)
	}
	if cfg.operatorSecret != "" {
		t.Fatalf("expected empty operatorSecret in KMS mode")
	}
	if cfg.kmsKeyID == "" {
		t.Fatalf("expected non-empty kmsKeyID")
	}
}

func TestLoadConfig_RequireKMSRejectsRawOperatorSecret(t *testing.T) {
	t.Setenv("STELLAR_RPC_URL", "https://mainnet.sorobanrpc.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.stellar.org")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Public Global Stellar Network ; September 2015")
	t.Setenv("STELLAR_OPERATOR_SECRET", "SB7Y3EXAMPLEOPERATORSECRET")
	t.Setenv("SIGNER_REQUIRE_KMS", "true")
	t.Setenv("SIGNER_KMS_KEY_ID", "gcpkms://key-1")
	t.Setenv("SIGNER_KMS_PUBLIC_ADDRESS", "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7")
	t.Setenv("SIGNER_KMS_ENDPOINT_URL", "http://127.0.0.1:8200/v1/kms/sign")
	t.Setenv("SIGNER_KILL_SWITCH_PATH", "/run/nester/killswitch")
	t.Setenv("SIGNER_SOCKET_PATH", "/run/nester/signer.sock")
	t.Setenv("SIGNER_ALLOWED_CONTRACTS", "CF6F8NESTERVAULTMAINNET0000000000000000000000000000000000")

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected loadConfig to fail when STELLAR_OPERATOR_SECRET is set alongside SIGNER_REQUIRE_KMS=true")
	}
}

