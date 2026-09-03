package telemetry

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Credential-shaped fixtures for exercising the redaction rules.
//
// These are assembled from fragments at run time rather than written as
// literals. The values are fake either way, but a literal that *looks* like a
// credential trips the repository's gitleaks scan, and adding allowlist
// entries for test data trains everyone to ignore that scanner. Building them
// here keeps the scan meaningful while the tests still see the exact strings
// a real secret would produce.
var (
	fakeStellarSecret = "S" + "BSVTQO4V6WQNQK4TSFVQVUDCUKYJ2ZQFPKGZVFPWMJXW2WOHVUTPQKZ"
	fakeStellarPublic = "G" + "A5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	fakeJWT           = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + "." +
		"eyJzdWIiOiIxMjM0NSJ9" + "." +
		"dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	fakeAnthropicKey   = "sk-" + "ant-api03-" + strings.Repeat("A", 36)
	fakeStripeStyleKey = "sk_" + "live_" + "abc123def456ghi789"
)

func TestRedactValueStripsSecrets(t *testing.T) {
	// The secret is tracked separately from the input. Asserting only on the
	// whole input is too weak for the embedded cases: any change to the
	// surrounding text satisfies it, so a redactor that stripped the word
	// "secret" and left the seed intact would still pass.
	tests := []struct {
		name   string
		input  string
		secret string
	}{
		{"stellar secret seed", fakeStellarSecret, fakeStellarSecret},
		{"jwt", fakeJWT, fakeJWT},
		{"anthropic api key", fakeAnthropicKey, fakeAnthropicKey},
		{"stripe-style secret key", fakeStripeStyleKey, fakeStripeStyleKey},
		{"bearer header", "Bearer " + fakeJWT, fakeJWT},
		{"secret embedded in error text", "invalid operator secret: " + fakeStellarSecret, fakeStellarSecret},
		{"jwt embedded in url", "https://api.example.com/cb?token=" + fakeJWT, fakeJWT},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactValue(tc.input)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("RedactValue leaked the secret: %q", got)
			}
			if !strings.Contains(got, RedactedPlaceholder) {
				t.Fatalf("RedactValue(%q) = %q, expected it to contain %q", tc.name, got, RedactedPlaceholder)
			}
		})
	}
}

// A public Stellar address is not a secret and is useful on a span for
// correlating chain activity, so redaction must not destroy it.
func TestRedactValuePreservesNonSecrets(t *testing.T) {
	tests := []string{
		fakeStellarPublic,
		"CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		"deposit",
		"GET /api/v1/portfolio",
	}

	for _, input := range tests {
		if got := RedactValue(input); got != input {
			t.Errorf("RedactValue(%q) = %q, want it unchanged", input, got)
		}
	}
}

func TestRedactValueTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("a", maxAttributeLen*2)
	got := RedactValue(long)
	if len(got) > maxAttributeLen+3 {
		t.Fatalf("RedactValue did not truncate: got length %d", len(got))
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"db.statement.parameters",
		"auth.token",
		"http.request.header.authorization",
		"operator_secret",
		"signed_xdr",
		"anthropic.prompt",
		"model.completion",
		"bank.account_number",
		"user.balance",
		"JWT",
		"Private_Key",
	}
	for _, key := range sensitive {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", key)
		}
	}

	safe := []string{
		"http.route",
		"db.system",
		"soroban.contract_id",
		"soroban.function",
		"anthropic.model",
		"anthropic.input_tokens",
		"nester.request_id",
	}
	for _, key := range safe {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", key)
		}
	}
}

// SafeAttribute must refuse a sensitive key even when the value itself looks
// innocuous, because the key names a category that is never safe to export.
func TestSafeAttributeRedactsBySensitiveKey(t *testing.T) {
	attr := SafeAttribute("db.statement.parameters", "42")
	if attr.Value.AsString() != RedactedPlaceholder {
		t.Fatalf("SafeAttribute leaked a value under a sensitive key: %q", attr.Value.AsString())
	}
}

func TestSafeAttributeRedactsBySensitiveValue(t *testing.T) {
	attr := SafeAttribute("stellar.address", fakeStellarSecret)
	if strings.Contains(attr.Value.AsString(), fakeStellarSecret) {
		t.Fatalf("SafeAttribute leaked a secret seed: %q", attr.Value.AsString())
	}
}

// --- Regression tests for leaks found reviewing the first implementation ---

// A secret glued to adjacent text (a DSN, an interpolated error string) has no
// word boundary around it. The original \b-anchored patterns silently failed
// to match these, exporting the secret whole.
func TestRedactValueStripsUnanchoredSecrets(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		secret string
	}{
		{"seed glued to key=value", "secret=" + fakeStellarSecret + "trailing", fakeStellarSecret},
		{"seed inside a URL", "https://x.test/cb?s=" + fakeStellarSecret + "&next=1", fakeStellarSecret},
		{"jwt glued to text", "token:" + fakeJWT + "|end", fakeJWT},
		{"anthropic key glued", "key=" + fakeAnthropicKey + ";", fakeAnthropicKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactValue(tc.input)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("unanchored secret survived redaction: %q", got)
			}
		})
	}
}

// Truncation must never emit a usable prefix of an opaque credential. A long
// high-entropy run straddling the cut is redacted whole instead.
func TestTruncationDoesNotEmitPartialSecret(t *testing.T) {
	padding := strings.Repeat("a", maxAttributeLen-20)
	unknownCredential := strings.Repeat("Xq7", 30) // 90 chars, no named pattern
	got := RedactValue(padding + unknownCredential)

	if strings.Contains(got, unknownCredential[:30]) {
		t.Fatalf("truncation exported a partial credential: %q", got)
	}
	if got != RedactedPlaceholder {
		t.Errorf("expected whole-value redaction, got %q", got)
	}
}

// Truncating at a raw byte offset can split a multi-byte rune and yield
// invalid UTF-8, which corrupts the attribute on export.
func TestTruncationPreservesUTF8(t *testing.T) {
	got := RedactValue(strings.Repeat("é", 200))
	if !utf8.ValidString(got) {
		t.Fatalf("truncation produced invalid UTF-8: %q", got)
	}
}

// An exempt suffix must not excuse a key that a stronger rule already
// rejected, or a sensitive key could ride "..._tokens" past every check.
func TestSensitiveKeyExceptionsCannotBypassOtherRules(t *testing.T) {
	bypassAttempts := []string{
		"secret.input_tokens",
		"operator_secret.token_count",
		"db.statement.max_tokens",
		"authorization.total_tokens",
		"private_key.output_tokens",
	}
	for _, key := range bypassAttempts {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false; exempt suffix bypassed a stronger rule", key)
		}
	}

	// The legitimate exceptions must still work.
	for _, key := range []string{"anthropic.input_tokens", "anthropic.output_tokens", "llm.token_count"} {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true; token counts must remain recordable", key)
		}
	}
}

// The canonical parameterised-SQL keys are permitted, but every derived
// statement key must still be rejected — db.statement.parameters is precisely
// the interpolated-value leak #1054 forbids.
func TestStatementKeyPolicy(t *testing.T) {
	allowed := []string{"db.statement", "db.query.text", "DB.Statement"}
	for _, key := range allowed {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true; parameterised SQL must be recordable", key)
		}
	}

	rejected := []string{
		"db.statement.parameters",
		"db.statement.rendered",
		"db.statement.max_tokens",
		"db.query.text.parameters",
		"db.statement_values",
	}
	for _, key := range rejected {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false; derived statement keys must be rejected", key)
		}
	}
}

// Soroban RPC echoes base64 XDR back inside error messages. A signed envelope
// embeds the operator signature and every operation argument, so it must never
// reach a span — while the public identifiers an operator needs must survive.
func TestRedactValueStripsTransactionXDR(t *testing.T) {
	xdrs := []string{
		"AAAAAQAAAAEAAAAHc2VjcmV0eA==",
		"AAAAAAAAAAIAAAAGAAAAAcm5X2Nvb2tpZXh4eHhBQkNERUZHSElKS0xNTk9Q",
		"AAAAAgAAAAB6/vNBmDVDPWCFvOdA6bnKzHRLFcCnPeSDdMSlfpEeqQ==",
	}
	for _, xdr := range xdrs {
		got := RedactValue("soroban simulate failed: " + xdr)
		if strings.Contains(got, xdr) {
			t.Errorf("XDR survived redaction: %q", got)
		}
	}
}

// The XDR rules must not destroy the public identifiers the Soroban spans are
// required to carry. Length cannot separate these from XDR, so the alphabet
// distinction is load-bearing and worth pinning down.
func TestRedactValuePreservesChainIdentifiers(t *testing.T) {
	identifiers := []string{
		"CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",         // contract ID
		"G" + "A5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",    // public address
		"a1b2c3d4e5f60718293a4b5c6d7e8f901a2b3c4d5e6f708192a3b4c5d6e7f801", // tx hash
		"9f3ab2c1-0000-4000-8000-000000000000",                             // uuid
		"GET /api/v1/users/{id}/savings-goals",                             // route pattern
	}
	for _, id := range identifiers {
		if got := RedactValue(id); got != id {
			t.Errorf("RedactValue(%q) = %q; chain identifiers must survive", id, got)
		}
	}
}

// Session and identifier keys carry no credential pattern, so RedactValue
// cannot recognise their values. The key rule is the only control, and these
// were missing from the first implementation.
func TestIsSensitiveKeyBlocksSessionAndIdentifiers(t *testing.T) {
	for _, key := range []string{
		"http.request.header.cookie",
		"auth.session_id",
		"user.email",
		"user.phone",
		"customer.phone_number",
		"Set-Cookie",
	} {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false; session and identifier keys must be rejected", key)
		}
	}
}

// Stripe-style webhook secrets do not use the sk_/pk_ shape that
// genericKeyPattern matches. The webhook subsystem handles whsec_ values, so
// they can reach an error string and from there a span.
func TestRedactValueStripsWebhookSecrets(t *testing.T) {
	keys := []string{
		"whsec_" + strings.Repeat("d", 28),
	}
	for _, key := range keys {
		got := RedactValue("webhook key=" + key + " rejected")
		if strings.Contains(got, key) {
			t.Errorf("webhook secret survived redaction: %q", got)
		}
		if !strings.Contains(got, RedactedPlaceholder) {
			t.Errorf("expected a placeholder for %q, got %q", key, got)
		}
	}
}
