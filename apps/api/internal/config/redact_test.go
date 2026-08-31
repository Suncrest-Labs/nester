package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Issue #1106, "no secret is ever logged, including in startup diagnostics".
//
// These tests are about a leak that is invisible in review: config secrets sit
// in unexported fields, which feels safe, but fmt's reflection reaches them and
// prints them for %v and %+v alike. The redaction in redact.go closes that; the
// tests below pin it shut, including for secret fields added later.

// sentinel values are distinctive enough that a substring search cannot
// plausibly match them by accident.
//
// They are deliberately written as readable words rather than as
// random-looking hex. A high-entropy literal in a file named *_test.go still
// looks exactly like a leaked credential to a secret scanner, and the honest
// fix is a value that is not key-shaped rather than an allowlist entry
// teaching the scanner to ignore this file. These only need to be unique
// enough that strings.Contains cannot match them by chance, which
// "not-a-real-secret" satisfies as well as a hex blob does.
const (
	sentinelJWTSecret     = "SENTINEL-jwt-secret-not-a-real-secret"
	sentinelDSN           = "postgres://user:SENTINEL-db-password-not-real@host:5432/db"
	sentinelOperatorSeed  = "SENTINEL-stellar-operator-seed-not-real"
	sentinelPaystack      = "SENTINEL-paystack-key-not-real"
	sentinelFlutterwave   = "SENTINEL-flutterwave-key-not-real"
	sentinelServiceAPIKey = "SENTINEL-service-api-key-not-real"
	sentinelIntelKey      = "SENTINEL-intelligence-key-not-real"
	sentinelCipherKey     = "SENTINEL-bank-cipher-key-not-real"
	sentinelFingerprint   = "SENTINEL-fingerprint-key-not-real"
	sentinelAccountKey    = "SENTINEL-account-cipher-key-not-real"
)

// allSentinels is every secret planted below, so a test can assert none of
// them survive into rendered output.
var allSentinels = []string{
	sentinelJWTSecret, sentinelDSN, sentinelOperatorSeed,
	sentinelPaystack, sentinelFlutterwave, sentinelServiceAPIKey,
	sentinelIntelKey, sentinelCipherKey, sentinelFingerprint,
	sentinelAccountKey,
}

// configWithSecrets builds a Config with every credential field populated by a
// sentinel, so any leak shows up as a recognizable substring.
func configWithSecrets() Config {
	return Config{
		environment:          "production",
		bankAccountCipherKey: sentinelCipherKey,
		auth: AuthConfig{
			secret:                  sentinelJWTSecret,
			serviceAPIKey:           sentinelServiceAPIKey,
			accessTokenExpiry:       5 * time.Minute,
			refreshTokenExpiry:      7 * 24 * time.Hour,
			absoluteSessionLifetime: 30 * 24 * time.Hour,
			challengeExpiry:         5 * time.Minute,
		},
		database: DatabaseConfig{
			dsn:               sentinelDSN,
			poolSize:          10,
			connectionTimeout: 5 * time.Second,
		},
		stellar: StellarConfig{
			operatorSecret:    sentinelOperatorSeed,
			operatorAddress:   "GPUBLICADDRESSNOTSECRET",
			horizonURL:        "https://horizon-testnet.stellar.org",
			rpcURL:            "https://soroban-testnet.stellar.org",
			networkPassphrase: "Test SDF Network ; September 2015",
		},
		bank: BankConfig{
			paystackKey:    sentinelPaystack,
			flutterwaveKey: sentinelFlutterwave,
		},
		intelligence: IntelligenceConfig{
			baseURL:       "http://intelligence:8000",
			serviceAPIKey: sentinelIntelKey,
			timeout:       10 * time.Second,
		},
		accountCipher: AccountCipherConfig{
			activeVersion:  "v1",
			keys:           map[string]string{"v1": sentinelAccountKey},
			fingerprintKey: sentinelFingerprint,
		},
	}
}

// assertNoSentinels fails when any planted secret appears in rendered.
func assertNoSentinels(t *testing.T, label, rendered string) {
	t.Helper()

	for _, secret := range allSentinels {
		if strings.Contains(rendered, secret) {
			t.Errorf("%s leaked a secret (%s):\n%s", label, secret, rendered)
		}
	}
}

// TestConfigFormattingNeverLeaksSecrets covers the fmt verbs that reach
// unexported fields by reflection. %+v and %#v are the ones a developer
// reaches for when dumping a struct, and both must go through String().
func TestConfigFormattingNeverLeaksSecrets(t *testing.T) {
	cfg := configWithSecrets()

	for _, verb := range []string{"%v", "%+v", "%s", "%q"} {
		t.Run(verb, func(t *testing.T) {
			assertNoSentinels(t, "fmt.Sprintf("+verb+", cfg)", fmt.Sprintf(verb, cfg))
			assertNoSentinels(t, "fmt.Sprintf("+verb+", &cfg)", fmt.Sprintf(verb, &cfg))
		})
	}
}

// TestNestedConfigFormattingNeverLeaksSecrets checks each sub-struct on its
// own, since they are passed around and logged independently of Config.
func TestNestedConfigFormattingNeverLeaksSecrets(t *testing.T) {
	cfg := configWithSecrets()

	cases := map[string]any{
		"AuthConfig":          cfg.auth,
		"DatabaseConfig":      cfg.database,
		"StellarConfig":       cfg.stellar,
		"BankConfig":          cfg.bank,
		"IntelligenceConfig":  cfg.intelligence,
		"AccountCipherConfig": cfg.accountCipher,
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%s"} {
				assertNoSentinels(t, name+" "+verb, fmt.Sprintf(verb, value))
			}
		})
	}
}

// TestConfigSlogNeverLeaksSecrets covers the structured-logging path, which is
// how this service actually logs. slog resolves LogValuer, so a config logged
// as an attribute value must come out redacted.
func TestConfigSlogNeverLeaksSecrets(t *testing.T) {
	cfg := configWithSecrets()

	for _, handlerName := range []string{"json", "text"} {
		t.Run(handlerName, func(t *testing.T) {
			var buf bytes.Buffer
			var h slog.Handler
			if handlerName == "json" {
				h = slog.NewJSONHandler(&buf, nil)
			} else {
				h = slog.NewTextHandler(&buf, nil)
			}

			logger := slog.New(h)
			logger.Info("startup configuration", "config", cfg)
			logger.Info("auth configuration", "auth", cfg.auth)
			logger.Info("pointer form", "config", &cfg)

			assertNoSentinels(t, "slog "+handlerName, buf.String())
		})
	}
}

// TestRedactedConfigStillReportsWhetherSecretsAreSet asserts redaction did not
// go so far as to make startup diagnostics useless: an operator must still be
// able to see that a secret is configured, and that an unset one is not.
func TestRedactedConfigStillReportsWhetherSecretsAreSet(t *testing.T) {
	set := fmt.Sprintf("%v", configWithSecrets().auth)
	if !strings.Contains(set, redactedPlaceholder) {
		t.Errorf("a configured secret should render as %s, got:\n%s", redactedPlaceholder, set)
	}

	empty := fmt.Sprintf("%v", AuthConfig{})
	if strings.Contains(empty, redactedPlaceholder) {
		t.Errorf("an unset secret should render as empty, not %s, got:\n%s", redactedPlaceholder, empty)
	}
}

// TestRedactedConfigKeepsNonSecretFieldsVisible guards the opposite failure:
// redaction that hides the operational values a diagnostic exists to show.
func TestRedactedConfigKeepsNonSecretFieldsVisible(t *testing.T) {
	cfg := configWithSecrets()
	rendered := fmt.Sprintf("%v", cfg)

	for _, want := range []string{
		"production",                  // environment
		"horizon-testnet.stellar.org", // Horizon URL
		"GPUBLICADDRESSNOTSECRET",     // operator PUBLIC address
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("redacted config dropped non-secret value %q:\n%s", want, rendered)
		}
	}
}

// secretFieldPattern names the field-name substrings that indicate a
// credential. Matching is case-insensitive.
var secretFieldPattern = []string{"secret", "password", "dsn", "apikey", "cipherkey", "fingerprintkey", "privatekey", "token"}

// looksSecret reports whether a field name suggests it holds a credential.
// operatorAddress and similar public values are excluded by name.
func looksSecret(fieldName string) bool {
	lower := strings.ToLower(fieldName)

	// Explicit non-secrets whose names would otherwise match.
	switch lower {
	case "accesstokenexpiry", "refreshtokenexpiry", "challengeexpiry", "absolutesessionlifetime":
		return false
	}

	for _, pattern := range secretFieldPattern {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// TestEverySecretBearingStructRedacts is the tripwire for fields added later.
//
// It walks Config by reflection, finds every struct holding a field whose name
// looks like a credential, and asserts that struct implements both String()
// and slog.LogValuer. A new `webhookSecret string` on a config struct with no
// redaction fails here, naming the struct — rather than silently shipping a
// value that %+v will print.
func TestEverySecretBearingStructRedacts(t *testing.T) {
	stringerType := reflect.TypeOf((*fmt.Stringer)(nil)).Elem()
	logValuerType := reflect.TypeOf((*slog.LogValuer)(nil)).Elem()

	seen := make(map[reflect.Type]bool)

	var walk func(t *testing.T, typ reflect.Type, path string)
	walk = func(t *testing.T, typ reflect.Type, path string) {
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true

		var secretFields []string
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Type.Kind() == reflect.String && looksSecret(field.Name) {
				secretFields = append(secretFields, field.Name)
			}
			// Recurse into nested config structs.
			if field.Type.Kind() == reflect.Struct {
				walk(t, field.Type, path+"."+field.Name)
			}
		}

		if len(secretFields) == 0 {
			return
		}

		if !typ.Implements(stringerType) {
			t.Errorf(
				"%s (at %s) holds secret field(s) %v but does not implement String(); "+
					"fmt %%v/%%+v would print them. Add a String() method in redact.go.",
				typ.Name(), path, secretFields,
			)
		}
		if !typ.Implements(logValuerType) {
			t.Errorf(
				"%s (at %s) holds secret field(s) %v but does not implement slog.LogValue(); "+
					"structured logging would print them. Add a LogValue() method in redact.go.",
				typ.Name(), path, secretFields,
			)
		}
	}

	walk(t, reflect.TypeOf(Config{}), "Config")

	if len(seen) < 5 {
		t.Fatalf("reflection walk visited only %d struct types; traversal is broken", len(seen))
	}
}
