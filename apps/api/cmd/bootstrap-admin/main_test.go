package main

import (
	"strings"
	"testing"
)

func TestValidateWalletAddress(t *testing.T) {
	tests := []struct {
		name    string
		wallet  string
		wantErr bool
		errSub  string
	}{
		{
			name:    "valid address format",
			wallet:  "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			wallet:  "SA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
			wantErr: true,
			errSub:  "must start with 'G'",
		},
		{
			name:    "invalid length - too short",
			wallet:  "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5R",
			wantErr: true,
			errSub:  "be 56 characters",
		},
		{
			name:    "invalid length - too long",
			wallet:  "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVNEXTRA",
			wantErr: true,
			errSub:  "be 56 characters",
		},
		{
			name:    "invalid checksum",
			wallet:  "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZAA",
			wantErr: true,
			errSub:  "invalid Stellar address format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWalletAddress(tt.wallet)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateWalletAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errSub != "" {
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("validateWalletAddress() error = %v, want error to contain %q", err, tt.errSub)
				}
			}
		})
	}
}
