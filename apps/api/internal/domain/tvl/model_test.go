package tvl

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPrecisionHandling(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantUSD    string // 2-decimal truncated
		wantUSDC   string // 6-decimal fixed
	}{
		{
			name:     "full 6 decimal precision",
			input:    "1234.567890",
			wantUSD:  "1234.56",
			wantUSDC: "1234.567890",
		},
		{
			name:     "rounds up case is truncated instead",
			input:    "9999.999",
			wantUSD:  "9999.99",
			wantUSDC: "9999.999000",
		},
		{
			name:     "exact two decimals unchanged",
			input:    "100.50",
			wantUSD:  "100.50",
			wantUSDC: "100.500000",
		},
		{
			name:     "zero",
			input:    "0",
			wantUSD:  "0.00",
			wantUSDC: "0.000000",
		},
		{
			name:     "large value",
			input:    "10000000.123456789",
			wantUSD:  "10000000.12",
			wantUSDC: "10000000.123456",
		},
		{
			name:     "negative value truncates toward zero",
			input:    "-1234.567",
			wantUSD:  "-1234.56",
			wantUSDC: "-1234.567000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decimal.NewFromString(tc.input)
			if err != nil {
				t.Fatalf("invalid decimal %q: %v", tc.input, err)
			}

			if got := FormatUSD(d); got != tc.wantUSD {
				t.Errorf("2-decimal format = %s, want %s", got, tc.wantUSD)
			}
			if got := FormatUSDC(d); got != tc.wantUSDC {
				t.Errorf("6-decimal format = %s, want %s", got, tc.wantUSDC)
			}
		})
	}
}
