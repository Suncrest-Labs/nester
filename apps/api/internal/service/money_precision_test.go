package service

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/analytics"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// maxI128Stroops is 2^127 - 1, the largest Soroban token amount expressible as
// an i128 stroop value. Any layer on the money path has to carry this exactly.
const maxI128Stroops = "170141183460469231731687303715884105727"

// requiredIntegerDigits is the number of integer digits needed to store
// maxI128Stroops. Columns with fewer will raise "numeric field overflow".
const requiredIntegerDigits = 39

// TestAmountRoundTripPreservesExactValue covers the round-trip requirement in
// issue #1121: a contract-scale amount must survive domain -> JSON -> domain
// without losing a single stroop. The JSON hop is the step that actually
// regressed historically, because encoding through float64 silently rounds.
func TestAmountRoundTripPreservesExactValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"max i128 stroops", maxI128Stroops},
		{"large deposit beyond float64 precision", "12345678901234567890.12345678"},
		{"value that float64 cannot represent", "9007199254740993"},
		{"smallest non-zero stroop", "0.00000001"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original, err := decimal.NewFromString(tc.value)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.value, err)
			}

			// Domain -> JSON, the API boundary.
			in := vault.Vault{
				CurrentBalance: original,
				TotalDeposited: original,
				YieldEarned:    original,
				FeesPaid:       original,
			}
			encoded, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			// JSON -> domain, what a client parses back.
			var out vault.Vault
			if err := json.Unmarshal(encoded, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			for field, got := range map[string]decimal.Decimal{
				"CurrentBalance": out.CurrentBalance,
				"TotalDeposited": out.TotalDeposited,
				"YieldEarned":    out.YieldEarned,
				"FeesPaid":       out.FeesPaid,
			} {
				if !got.Equal(original) {
					t.Errorf("%s round-tripped to %s, want %s", field, got.String(), original.String())
				}
			}

			// The encoded form must carry the digits, not a float approximation.
			if !strings.Contains(string(encoded), original.String()) {
				t.Errorf("encoded payload lost exact value; got %s", encoded)
			}
		})
	}
}

// TestAnalyticsAmountsRoundTripExactly guards the reporting layer, which is
// where the float64 fields found by the #1121 audit actually lived.
func TestAnalyticsAmountsRoundTripExactly(t *testing.T) {
	original, err := decimal.NewFromString(maxI128Stroops)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	in := analytics.PerformanceMetrics{
		TotalYieldEarned: original,
		TotalDeposited:   original,
		TotalWithdrawn:   original,
		NetPosition:      original,
	}

	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out analytics.PerformanceMetrics
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for field, got := range map[string]decimal.Decimal{
		"TotalYieldEarned": out.TotalYieldEarned,
		"TotalDeposited":   out.TotalDeposited,
		"TotalWithdrawn":   out.TotalWithdrawn,
		"NetPosition":      out.NetPosition,
	} {
		if !got.Equal(original) {
			t.Errorf("%s round-tripped to %s, want %s", field, got.String(), original.String())
		}
	}
}

// numericColumn matches a money column declaration or a widening ALTER.
var numericColumn = regexp.MustCompile(`(?i)(\w+)\s+(?:TYPE\s+)?NUMERIC\s*\(\s*(\d+)\s*,\s*(\d+)\s*\)`)

// moneyColumn identifies a column name that carries an amount.
var moneyColumn = regexp.MustCompile(`(?i)amount|balance|deposited|withdrawn|yield_earned|fees_paid|fee_charged|estimated_fee|principal|stroop`)

// TestMoneyColumnsCoverI128Range proves the precision requirement from #1121:
// every money column must end up wide enough for the full i128 stroop range
// once the full migration chain has been applied.
func TestMoneyColumnsCoverI128Range(t *testing.T) {
	dir := filepath.Join("..", "..", "migrations")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no up migrations found")
	}
	// Lexical order matches the zero-padded numeric prefix ordering used here.
	sortStrings(names)

	// Track the final precision each column lands on after the whole chain.
	type spec struct {
		precision int
		scale     int
		source    string
	}
	final := map[string]spec{}

	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		content, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		for _, m := range numericColumn.FindAllStringSubmatch(string(content), -1) {
			col := m[1]
			if !moneyColumn.MatchString(col) {
				continue
			}
			p, _ := strconv.Atoi(m[2])
			s, _ := strconv.Atoi(m[3])
			final[strings.ToLower(col)] = spec{precision: p, scale: s, source: name}
		}
	}

	if len(final) == 0 {
		t.Fatal("no money columns discovered; the audit regex is wrong")
	}

	for col, sp := range final {
		if got := sp.precision - sp.scale; got < requiredIntegerDigits {
			t.Errorf(
				"column %s ends at NUMERIC(%d,%d) in %s: %d integer digits, need >= %d for the i128 stroop range",
				col, sp.precision, sp.scale, sp.source, got, requiredIntegerDigits,
			)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
