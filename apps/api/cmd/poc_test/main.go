package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/txnbuild"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

const (
	AnchorURL = "https://testanchor.stellar.org"
)

func main() {
	ctx := context.Background()

	log.Println("1. Generating testnet keypair...")
	kp, err := keypair.Random()
	if err != nil {
		log.Fatalf("Failed to generate keypair: %v", err)
	}
	log.Printf("Account ID: %s", kp.Address())

	// Funding the account is actually not strictly required just to get a SEP-10 token
	// for some anchors, but we will do it anyway if needed. For testanchor.stellar.org,
	// we can usually just do SEP-10 without funding first.

	log.Println("2. Requesting SEP-10 challenge...")
	challengeURL := fmt.Sprintf("%s/auth?account=%s", AnchorURL, kp.Address())
	resp, err := http.DefaultClient.Get(challengeURL)
	if err != nil {
		log.Fatalf("Failed to get challenge: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("Failed challenge request, status: %d, body: %s", resp.StatusCode, string(b))
	}

	var challengeResp struct {
		Transaction string `json:"transaction"`
		NetworkPassphrase string `json:"network_passphrase"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&challengeResp); err != nil {
		log.Fatalf("Failed to decode challenge: %v", err)
	}

	log.Println("3. Signing SEP-10 challenge...")
	parsedTx, err := txnbuild.TransactionFromXDR(challengeResp.Transaction)
	if err != nil {
		log.Fatalf("Failed to parse challenge XDR: %v", err)
	}
	
	tx, ok := parsedTx.Transaction()
	if !ok {
		log.Fatalf("Parsed XDR is not a transaction")
	}

	tx, err = tx.Sign(network.TestNetworkPassphrase, kp)
	if err != nil {
		log.Fatalf("Failed to sign challenge: %v", err)
	}

	signedXDR, err := tx.Base64()
	if err != nil {
		log.Fatalf("Failed to encode signed challenge: %v", err)
	}

	log.Println("4. Submitting signed challenge for JWT...")
	authData := url.Values{}
	authData.Set("transaction", signedXDR)
	
	authReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/auth", AnchorURL), strings.NewReader(authData.Encode()))
	if err != nil {
		log.Fatalf("Failed to create auth request: %v", err)
	}
	authReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		log.Fatalf("Failed to submit auth: %v", err)
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(authResp.Body)
		log.Fatalf("Auth failed, status: %d, body: %s", authResp.StatusCode, string(b))
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&tokenResp); err != nil {
		log.Fatalf("Failed to decode token: %v", err)
	}

	log.Println("JWT acquired successfully!")

	log.Println("5. Testing sep24_resolver.go flow...")
	resolver := service.NewSep24Resolver(AnchorURL+"/sep24", tokenResp.Token)

	// testanchor.stellar.org testnet asset is usually SRTC
	amount := decimal.NewFromInt(10)
	txID, redirectURL, err := resolver.InitiateInteractiveWithdrawal(ctx, "SRT", amount)
	if err != nil {
		log.Fatalf("InitiateInteractiveWithdrawal failed: %v", err)
	}

	log.Printf("Successfully initiated withdrawal! Transaction ID: %s", txID)
	log.Printf("Interactive URL: %s", redirectURL)

	log.Println("6. Polling transaction status...")
	status, err := resolver.PollTransactionStatus(ctx, txID)
	if err != nil {
		log.Fatalf("PollTransactionStatus failed: %v", err)
	}
	log.Printf("Current Transaction Status: %s", status)
	
	log.Println("SEP-24 Testnet Flow completely verified!")
}
