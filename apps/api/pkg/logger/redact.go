package logger

import (
	"log/slog"
	"regexp"
	"strings"
)

// RedactedPlaceholder replaces any sensitive value or PII pattern in structured logs (#1566).
const RedactedPlaceholder = "[REDACTED]"

var (
	// stellarSeedPattern matches 56-character StrKey Stellar secret seeds (S...).
	stellarSeedPattern = regexp.MustCompile(`S[A-Z2-7]{55}`)

	// jwtPattern matches 3-segment JSON Web Tokens (header.payload.signature).
	jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)

	// bearerPattern matches Authorization header values (`Bearer ...` or `Basic ...`).
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[^\s"',;]+`)

	// wsTokenQueryPattern matches WebSocket/URL query parameters carrying tokens (`?token=...` or `&token=...`).
	wsTokenQueryPattern = regexp.MustCompile(`(?i)([?&](?:token|access_token|refresh_token|jwt|secret|api_key)=)[^&\s"',;]+`)

	// emailPattern matches standard email addresses.
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

	// ibanPattern matches international bank account numbers (IBAN).
	ibanPattern = regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`)

	// ssnPattern matches US Social Security Number formats (XXX-XX-XXXX).
	ssnPattern = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

	// bankAccountLabeledPattern matches labeled bank account or routing numbers in free-form strings.
	bankAccountLabeledPattern = regexp.MustCompile(`(?i)\b(account[_\s-]*number|routing[_\s-]*number|acct[_\s-]*no|iban)\s*[:=]\s*[A-Za-z0-9\-]{6,34}`)
)

// sensitiveAttrKeys lists structured log attribute key fragments that must always
// be redacted in production request logs (JWTs, secrets, KYC PII, bank details).
var sensitiveAttrKeys = []string{
	"password",
	"passwd",
	"secret",
	"authorization",
	"jwt",
	"access_token",
	"refresh_token",
	"ws_token",
	"service_api_key",
	"api_key",
	"private_key",
	"seed",
	"email",
	"phone",
	"ssn",
	"tax_id",
	"national_id",
	"passport",
	"date_of_birth",
	"dob",
	"full_name",
	"legal_name",
	"residential_address",
	"account_number",
	"routing_number",
	"iban",
	"swift_bic",
	"bank_account",
	"sort_code",
}

// IsSensitiveAttrKey reports whether a log attribute key refers to credentials,
// tokens, KYC PII, or bank account details that must be redacted.
func IsSensitiveAttrKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	if lower == "token" {
		return true
	}
	for _, frag := range sensitiveAttrKeys {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// SanitizeLogString scrubs JWTs, WebSocket token query parameters, Stellar secret
// seeds (S...), email addresses, SSNs, IBANs, and labeled bank account numbers
// from any string before it is written to a log sink.
func SanitizeLogString(s string) string {
	if s == "" {
		return ""
	}
	out := stellarSeedPattern.ReplaceAllString(s, RedactedPlaceholder)
	out = jwtPattern.ReplaceAllString(out, RedactedPlaceholder)
	out = bearerPattern.ReplaceAllString(out, "$1 "+RedactedPlaceholder)
	out = wsTokenQueryPattern.ReplaceAllString(out, "${1}"+RedactedPlaceholder)
	out = emailPattern.ReplaceAllString(out, RedactedPlaceholder)
	out = ibanPattern.ReplaceAllString(out, RedactedPlaceholder)
	out = ssnPattern.ReplaceAllString(out, RedactedPlaceholder)
	out = bankAccountLabeledPattern.ReplaceAllString(out, "$1="+RedactedPlaceholder)
	return out
}

// RedactReplaceAttr is a slog.HandlerOptions ReplaceAttr hook that redacts
// sensitive attribute keys and scrubs PII/secret patterns from string and error values.
func RedactReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key != slog.TimeKey && a.Key != slog.LevelKey && a.Key != slog.SourceKey && IsSensitiveAttrKey(a.Key) {
		return slog.String(a.Key, RedactedPlaceholder)
	}

	val := a.Value.Resolve()
	switch val.Kind() {
	case slog.KindString:
		return slog.String(a.Key, SanitizeLogString(val.String()))
	case slog.KindGroup:
		attrs := val.Group()
		redacted := make([] any, 0, len(attrs))
		for _, sub := range attrs {
			redacted = append(redacted, RedactReplaceAttr(append(groups, a.Key), sub))
		}
		return slog.Group(a.Key, redacted...)
	case slog.KindAny:
		if err, ok := val.Any().(error); ok && err != nil {
			return slog.String(a.Key, SanitizeLogString(err.Error()))
		}
	}
	return a
}
