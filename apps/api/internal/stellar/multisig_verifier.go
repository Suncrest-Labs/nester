package stellar

import (
	"encoding/json"
	"fmt"
	"sort"
)

// HorizonAccountResponse represents the subset of Horizon's GET /accounts/{id}
// payload required to verify on-chain multisig thresholds and signer weights (#1538).
type HorizonAccountResponse struct {
	ID         string             `json:"id"`
	AccountID  string             `json:"account_id"`
	Thresholds HorizonThresholds  `json:"thresholds"`
	Signers    []HorizonSignerRow `json:"signers"`
}

type HorizonThresholds struct {
	LowThreshold  int `json:"low_threshold"`
	MedThreshold  int `json:"med_threshold"`
	HighThreshold int `json:"high_threshold"`
}

type HorizonSignerRow struct {
	Key    string `json:"key"`
	Weight int    `json:"weight"`
	Type   string `json:"type"`
}

// MultisigPolicy expectations from docs/security/multisig-signers.md.
type MultisigPolicy struct {
	AdminAccount      string
	RequiredM         int
	RequiredN         int
	ExpectedSigners   []string
	ExpectedLowThresh int
}

// VerifyMultisigAccount parses a Horizon /accounts/{admin} JSON payload and
// enforces that:
//  1. The master key weight is 0 (disabled).
//  2. med_threshold == RequiredM and high_threshold == RequiredM (and RequiredM >= 2).
//  3. Every active signer (weight > 0) has weight == 1, so no single signer can
//     meet med_threshold or high_threshold alone.
//  4. Active signers exactly match ExpectedSigners (count == RequiredN).
func VerifyMultisigAccount(rawJSON []byte, policy MultisigPolicy) error {
	var acct HorizonAccountResponse
	if err := json.Unmarshal(rawJSON, &acct); err != nil {
		return fmt.Errorf("parse horizon account json: %w", err)
	}

	accountID := acct.AccountID
	if accountID == "" {
		accountID = acct.ID
	}
	if policy.AdminAccount != "" && accountID != policy.AdminAccount {
		return fmt.Errorf("account_id mismatch: got %q, want %q", accountID, policy.AdminAccount)
	}

	if policy.RequiredM < 2 {
		return fmt.Errorf("invalid policy RequiredM=%d: multisig threshold must be at least 2", policy.RequiredM)
	}

	if acct.Thresholds.MedThreshold != policy.RequiredM {
		return fmt.Errorf("med_threshold mismatch: got %d, want %d", acct.Thresholds.MedThreshold, policy.RequiredM)
	}
	if acct.Thresholds.HighThreshold != policy.RequiredM {
		return fmt.Errorf("high_threshold mismatch: got %d, want %d", acct.Thresholds.HighThreshold, policy.RequiredM)
	}
	if policy.ExpectedLowThresh > 0 && acct.Thresholds.LowThreshold != policy.ExpectedLowThresh {
		return fmt.Errorf("low_threshold mismatch: got %d, want %d", acct.Thresholds.LowThreshold, policy.ExpectedLowThresh)
	}

	masterFound := false
	var activeSigners []string

	for _, s := range acct.Signers {
		if s.Key == accountID {
			masterFound = true
			if s.Weight != 0 {
				return fmt.Errorf("master key %s has weight %d, expected 0", s.Key, s.Weight)
			}
			continue
		}
		if s.Weight <= 0 {
			continue
		}
		if s.Weight != 1 {
			return fmt.Errorf("signer %s has weight %d, expected 1", s.Key, s.Weight)
		}
		if s.Weight >= acct.Thresholds.MedThreshold || s.Weight >= acct.Thresholds.HighThreshold {
			return fmt.Errorf("single signer %s weight %d meets threshold", s.Key, s.Weight)
		}
		activeSigners = append(activeSigners, s.Key)
	}

	if !masterFound {
		return fmt.Errorf("master key entry %s missing from Horizon signers array", accountID)
	}

	if len(activeSigners) != policy.RequiredN {
		return fmt.Errorf("active signer count mismatch: got %d, want %d", len(activeSigners), policy.RequiredN)
	}

	if len(policy.ExpectedSigners) > 0 {
		got := append([]string(nil), activeSigners...)
		want := append([]string(nil), policy.ExpectedSigners...)
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			return fmt.Errorf("expected signer list length mismatch: got %d, want %d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				return fmt.Errorf("active signer set mismatch at index %d: got %s, want %s", i, got[i], want[i])
			}
		}
	}

	return nil
}
