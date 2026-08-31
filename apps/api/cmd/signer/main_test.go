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
