package service_test

import (
	"encoding/base64"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

func testCipher(t *testing.T) *crypto.AccountCipher {
	t.Helper()
	c, err := crypto.NewAccountCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return c
}
