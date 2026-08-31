package telemetry

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzRedactValueNeverLeaksSeed asserts the core invariant against arbitrary
// surrounding text: whatever the padding, a Stellar secret seed embedded in a
// value must never survive redaction, and the output must stay valid UTF-8.
func FuzzRedactValueNeverLeaksSeed(f *testing.F) {
	f.Add("prefix", "suffix")
	f.Add("", "")
	f.Add("secret=", "trailing")
	f.Add(strings.Repeat("a", 250), strings.Repeat("b", 250))
	f.Add("é", "日本語")

	f.Fuzz(func(t *testing.T, prefix, suffix string) {
		got := RedactValue(prefix + fakeStellarSecret + suffix)

		if strings.Contains(got, fakeStellarSecret) {
			t.Fatalf("seed survived redaction with prefix=%q suffix=%q -> %q", prefix, suffix, got)
		}

		// Any long run of the seed is a disclosure, not just one anchored at
		// index 0. An earlier version of this test checked only the leading
		// 24 characters, which missed a real bypass: an adjacent base32 prefix
		// could absorb the start of the pattern match and leave seed
		// characters 1..55 exported. Every window is checked now.
		if window := longestSurvivingRun(got, fakeStellarSecret); window >= 16 {
			t.Fatalf("partial seed leaked (%d consecutive chars) with prefix=%q suffix=%q -> %q",
				window, prefix, suffix, got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 produced for prefix=%q suffix=%q", prefix, suffix)
		}
		if len(got) > maxAttributeLen+3 {
			t.Fatalf("output exceeded bound: %d bytes", len(got))
		}
	})
}

// FuzzIsSensitiveKeyNeverPanics also asserts that no key containing a
// disallowed fragment is ever reported safe.
func FuzzIsSensitiveKeyNeverPanics(f *testing.F) {
	f.Add("db.statement")
	f.Add("secret.input_tokens")
	f.Add("")
	f.Fuzz(func(t *testing.T, key string) {
		got := IsSensitiveKey(key)
		lower := strings.ToLower(key)

		isCanonical := false
		for _, allowed := range canonicalStatementKeys {
			if lower == allowed {
				isCanonical = true
			}
		}
		if isCanonical {
			return
		}
		for _, fragment := range sensitiveKeyFragments {
			if strings.Contains(lower, fragment) && !got {
				t.Fatalf("key %q contains sensitive fragment %q but was reported safe", key, fragment)
			}
		}
	})
}

// longestSurvivingRun returns the length of the longest contiguous substring
// of secret that appears anywhere in haystack. Checking every window rather
// than a fixed prefix is what makes the assertion match the actual invariant:
// no meaningful span of the secret may survive, wherever it sits.
func longestSurvivingRun(haystack, secret string) int {
	longest := 0
	for i := 0; i < len(secret); i++ {
		for j := len(secret); j > i+longest; j-- {
			if strings.Contains(haystack, secret[i:j]) {
				longest = j - i
				break
			}
		}
	}
	return longest
}
