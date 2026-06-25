package savingsgoal

import "testing"

func TestIsSupportedCurrency(t *testing.T) {
	cases := []struct {
		currency string
		want     bool
	}{
		{"USDC", true},
		{"usdc", true},
		{"XLM", true},
		{" xlm ", true},
		{"NGN", false},
		{"BTC", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsSupportedCurrency(tc.currency); got != tc.want {
			t.Errorf("IsSupportedCurrency(%q) = %v, want %v", tc.currency, got, tc.want)
		}
	}
}

func TestNormalizeCurrency(t *testing.T) {
	if got := NormalizeCurrency(" xlm "); got != "XLM" {
		t.Fatalf("NormalizeCurrency() = %q, want XLM", got)
	}
}
