package config

import (
	"fmt"
	"log/slog"
)

// Issue #1106, "no secret is ever logged, including in startup diagnostics".
//
// The config structs hold secrets in unexported fields, which keeps them out
// of encoding/json but does nothing for fmt: `%v` and `%+v` reach unexported
// fields via reflection and print them verbatim. A single
// `logger.Info("config", "cfg", cfg)` or `fmt.Printf("%+v", cfg)` — in a
// startup diagnostic, a debug session, or a panic dump — is all it takes to
// put AUTH_JWT_SECRET or the Stellar operator seed into a log aggregator.
//
// Implementing String() makes every fmt verb route through it, so the secret
// is unreachable through fmt regardless of the verb used. Implementing
// slog.LogValuer does the same for structured logging. Both are declared on
// value receivers so they apply whether the value or a pointer is logged.
//
// redactedPlaceholder is what stands in for a set secret. Distinguishing set
// from empty matters when reading a startup diagnostic: "[REDACTED]" and ""
// answer different questions, and neither reveals the value.
const redactedPlaceholder = "[REDACTED]"

// redact returns the placeholder for a non-empty secret and the empty string
// for an unset one, so a diagnostic can still show whether a value is
// configured without disclosing it.
func redact(secret string) string {
	if secret == "" {
		return ""
	}
	return redactedPlaceholder
}

// ---------------------------------------------------------------------------
// AuthConfig
// ---------------------------------------------------------------------------

func (a AuthConfig) String() string {
	return fmt.Sprintf(
		"AuthConfig{secret:%q serviceAPIKey:%q accessTokenExpiry:%s refreshTokenExpiry:%s absoluteSessionLifetime:%s challengeExpiry:%s}",
		redact(a.secret), redact(a.serviceAPIKey),
		a.accessTokenExpiry, a.refreshTokenExpiry, a.absoluteSessionLifetime, a.challengeExpiry,
	)
}

func (a AuthConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("secret", redact(a.secret)),
		slog.String("service_api_key", redact(a.serviceAPIKey)),
		slog.Duration("access_token_expiry", a.accessTokenExpiry),
		slog.Duration("refresh_token_expiry", a.refreshTokenExpiry),
		slog.Duration("absolute_session_lifetime", a.absoluteSessionLifetime),
		slog.Duration("challenge_expiry", a.challengeExpiry),
	)
}

// ---------------------------------------------------------------------------
// DatabaseConfig
// ---------------------------------------------------------------------------

// The DSN carries the database password in userinfo, so it is redacted whole
// rather than parsed — a malformed DSN must not fall back to printing itself.

func (d DatabaseConfig) String() string {
	return fmt.Sprintf(
		"DatabaseConfig{dsn:%q poolSize:%d connectionTimeout:%s}",
		redact(d.dsn), d.poolSize, d.connectionTimeout,
	)
}

func (d DatabaseConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("dsn", redact(d.dsn)),
		slog.Int("pool_size", d.poolSize),
		slog.Duration("connection_timeout", d.connectionTimeout),
	)
}

// ---------------------------------------------------------------------------
// StellarConfig
// ---------------------------------------------------------------------------

// operatorSecret is a Stellar seed (S...) with signing authority over
// protocol funds — the highest-value secret in this config.

func (s StellarConfig) String() string {
	return fmt.Sprintf(
		"StellarConfig{operatorSecret:%q operatorAddress:%q horizonURL:%q rpcURL:%q networkPassphrase:%q}",
		redact(s.operatorSecret), s.operatorAddress, s.horizonURL, s.rpcURL, s.networkPassphrase,
	)
}

func (s StellarConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("operator_secret", redact(s.operatorSecret)),
		// operatorAddress is the PUBLIC key — safe to log, and useful for
		// confirming which operator account an instance is running as.
		slog.String("operator_address", s.operatorAddress),
		slog.String("horizon_url", s.horizonURL),
		slog.String("rpc_url", s.rpcURL),
		slog.String("network_passphrase", s.networkPassphrase),
	)
}

// ---------------------------------------------------------------------------
// BankConfig
// ---------------------------------------------------------------------------

func (b BankConfig) String() string {
	return fmt.Sprintf(
		"BankConfig{paystackKey:%q flutterwaveKey:%q}",
		redact(b.paystackKey), redact(b.flutterwaveKey),
	)
}

func (b BankConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("paystack_key", redact(b.paystackKey)),
		slog.String("flutterwave_key", redact(b.flutterwaveKey)),
	)
}

// ---------------------------------------------------------------------------
// AccountCipherConfig
// ---------------------------------------------------------------------------

// keys maps a version label to an account-encryption key. Only the count and
// the active version label are reported — never a key, and never the map,
// whose default formatting would print every value.

func (a AccountCipherConfig) String() string {
	return fmt.Sprintf(
		"AccountCipherConfig{activeVersion:%q keys:%d configured fingerprintKey:%q}",
		a.activeVersion, len(a.keys), redact(a.fingerprintKey),
	)
}

func (a AccountCipherConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("active_version", a.activeVersion),
		slog.Int("key_count", len(a.keys)),
		slog.String("fingerprint_key", redact(a.fingerprintKey)),
	)
}

// ---------------------------------------------------------------------------
// IntelligenceConfig
// ---------------------------------------------------------------------------

func (i IntelligenceConfig) String() string {
	return fmt.Sprintf(
		"IntelligenceConfig{serviceAPIKey:%q timeout:%s}",
		redact(i.serviceAPIKey), i.timeout,
	)
}

func (i IntelligenceConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("service_api_key", redact(i.serviceAPIKey)),
		slog.Duration("timeout", i.timeout),
	)
}

// ---------------------------------------------------------------------------
// RateLimitConfig
// ---------------------------------------------------------------------------

// RateLimitConfig holds quotaBypassToken, which lets its bearer skip cost
// accounting entirely. The limits and windows around it are ordinary
// operational settings and stay readable — a redacted config that hides them
// makes a misconfigured limiter harder to diagnose without making the token
// any safer.

func (r RateLimitConfig) String() string {
	return fmt.Sprintf(
		"RateLimitConfig{globalLimit:%d globalWindow:%s writeLimit:%d writeWindow:%s "+
			"walletLimit:%d walletWindow:%s rebalanceLimit:%d rebalanceWindow:%s "+
			"authLimit:%d authWindow:%s settlementLimit:%d settlementWindow:%s "+
			"trustedProxyCount:%d quotaEnabled:%t quotaLimit:%d quotaWindow:%s "+
			"quotaBypassToken:%q}",
		r.globalLimit, r.globalWindow, r.writeLimit, r.writeWindow,
		r.walletLimit, r.walletWindow, r.rebalanceLimit, r.rebalanceWindow,
		r.authLimit, r.authWindow, r.settlementLimit, r.settlementWindow,
		r.trustedProxyCount, r.quotaEnabled, r.quotaLimit, r.quotaWindow,
		redact(r.quotaBypassToken),
	)
}

func (r RateLimitConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("global_limit", r.globalLimit),
		slog.Duration("global_window", r.globalWindow),
		slog.Int("write_limit", r.writeLimit),
		slog.Duration("write_window", r.writeWindow),
		slog.Int("wallet_limit", r.walletLimit),
		slog.Duration("wallet_window", r.walletWindow),
		slog.Int("rebalance_limit", r.rebalanceLimit),
		slog.Duration("rebalance_window", r.rebalanceWindow),
		slog.Int("auth_limit", r.authLimit),
		slog.Duration("auth_window", r.authWindow),
		slog.Int("settlement_limit", r.settlementLimit),
		slog.Duration("settlement_window", r.settlementWindow),
		slog.Int("trusted_proxy_count", r.trustedProxyCount),
		slog.Bool("quota_enabled", r.quotaEnabled),
		slog.Int("quota_limit", r.quotaLimit),
		slog.Duration("quota_window", r.quotaWindow),
		slog.String("quota_bypass_token", redact(r.quotaBypassToken)),
	)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config is the struct most likely to be logged wholesale, and it both holds
// a secret directly (bankAccountCipherKey) and embeds every struct above.
// Without String()/LogValue() here, `%+v` on a Config would walk into those
// nested structs by reflection and never call their String() methods.

func (c Config) String() string {
	return fmt.Sprintf(
		"Config{environment:%q bankAccountCipherKey:%q auth:%s database:%s stellar:%s bank:%s intelligence:%s accountCipher:%s}",
		c.environment, redact(c.bankAccountCipherKey),
		c.auth, c.database, c.stellar, c.bank, c.intelligence, c.accountCipher,
	)
}

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("environment", c.environment),
		slog.String("bank_account_cipher_key", redact(c.bankAccountCipherKey)),
		slog.Any("auth", c.auth),
		slog.Any("database", c.database),
		slog.Any("stellar", c.stellar),
		slog.Any("bank", c.bank),
		slog.Any("intelligence", c.intelligence),
		slog.Any("account_cipher", c.accountCipher),
	)
}
