package config

import (
	"fmt"
	"strings"
)

// VarSpec describes one environment variable read by Load. Name, Type,
// Default and the unconditional Required flag are recorded from the loader
// calls themselves, so they cannot drift from the code. Secret, RequiredWhen
// and Mainnet are operator guidance the loader cannot express, kept in
// varNotes below and checked against the recorded names by the tests.
type VarSpec struct {
	Name         string
	Type         string
	Default      string
	Required     bool
	Secret       bool
	RequiredWhen string
	Mainnet      string
}

type varNote struct {
	secret       bool
	requiredWhen string
	mainnet      string
}

// varNotes holds the guidance that is not derivable from a loader call. Keep
// it in step with validate(): an entry here that names a variable Load no
// longer reads fails TestVarNotesReferenceRealVariables.
var varNotes = map[string]varNote{
	"APP_ENV": {mainnet: "Use `production` (or `staging` for a rehearsal). The default `development` enables the dev-only cipher key fallback and text logs."},

	"DATABASE_DSN": {secret: true, mainnet: "Carries the database password in userinfo. Use `sslmode=verify-full` against a managed instance."},

	"STELLAR_NETWORK_PASSPHRASE": {mainnet: "Must be `Public Global Stellar Network ; September 2015`. Must agree with STELLAR_RPC_URL and STELLAR_HORIZON_URL or startup fails."},
	"STELLAR_RPC_URL":            {mainnet: "Must not contain `testnet` or `futurenet` when the passphrase is mainnet."},
	"STELLAR_HORIZON_URL":        {mainnet: "Must not contain `testnet` or `futurenet` when the passphrase is mainnet."},
	"STELLAR_OPERATOR_SECRET":    {secret: true, mainnet: "Prefer SIGNER_SOCKET_PATH (isolated signer) so this process holds no operator key. If set, store it in the secrets manager only."},
	"STELLAR_OPERATOR_ADDRESS":   {mainnet: "Public address only; must correspond to the signer's key."},
	"SIGNER_SOCKET_PATH":         {mainnet: "Recommended on mainnet: delegates signing to the separate signer process."},
	"STELLAR_USDC_ISSUER": {
		mainnet: "Default is the Circle mainnet USDC issuer. It must be overridden on testnet and must not be changed on mainnet without review.",
	},
	"YIELD_REGISTRY_CONTRACT":             {requiredWhen: "mainnet passphrase", mainnet: "Startup fails if blank on mainnet."},
	"STELLAR_ALLOCATION_STRATEGY_ADDRESS": {requiredWhen: "mainnet passphrase", mainnet: "Startup fails if blank on mainnet."},
	"STELLAR_MAINNET_VAULT_TVL_CAP": {
		mainnet: "Default `0` means no per-vault cap. Set an explicit ceiling for the staged launch.",
	},
	"STELLAR_OPERATOR_FUNDED_DEPOSITS_ENABLED":  {mainnet: "Leave `false` unless STELLAR_OPERATOR_FUNDED_DEPOSIT_VAULTS and a max amount are set; this lets the API spend operator funds."},
	"STELLAR_OPERATOR_FUNDED_DEPOSIT_VAULTS":    {mainnet: "Allow-list of vaults eligible for operator-funded deposits."},
	"STELLAR_OPERATOR_FUNDED_DEPOSIT_MAX_AMOUNT": {mainnet: "Per-deposit ceiling. Default `0` is not a safe value if the feature is enabled."},
	"WITHDRAWAL_BREAKER_ENABLED":                {mainnet: "Default `false` is unsafe for mainnet; enable it."},
	"WITHDRAWAL_SLIPPAGE_BPS":                   {mainnet: "Must be between 1 and 300."},

	"AUTH_JWT_SECRET": {
		secret:  true,
		mainnet: "At least 32 characters with 8+ distinct bytes. The `.env.example` development value is rejected in production and staging.",
	},
	"NESTER_SERVICE_API_KEY": {
		secret:  true,
		mainnet: "Optional, but if set: 32+ characters, 8+ distinct bytes, and different from AUTH_JWT_SECRET. Empty disables service-to-service auth.",
	},
	"RATELIMIT_QUOTA_BYPASS_TOKEN": {secret: true, mainnet: "Leave empty on mainnet; a set value lets any holder skip quota accounting."},
	"RATELIMIT_TRUSTED_PROXY_COUNT": {
		mainnet: "Default `0` keys limits on the immediate peer. Behind a load balancer set this to the real hop count or every user shares one bucket.",
	},

	"ALLOWED_ORIGINS": {
		requiredWhen: "APP_ENV is production or staging",
		mainnet:      "Explicit `scheme://host[:port]` origins only. `*` and entries with a path, query or fragment are rejected.",
	},

	"BANK_ACCOUNT_ENCRYPTION_KEY": {
		secret:  true,
		mainnet: "Legacy single key (registered as version `v1`). The all-`A` development fallback only applies when APP_ENV=development. Prefer ACCOUNT_CIPHER_KEYS.",
	},
	"ACCOUNT_CIPHER_KEYS": {
		secret:  true,
		mainnet: "Comma-separated `version:base64key` pairs; versions are at most 32 characters. Takes precedence over BANK_ACCOUNT_ENCRYPTION_KEY.",
	},
	"ACCOUNT_CIPHER_ACTIVE_KEY": {
		requiredWhen: "ACCOUNT_CIPHER_KEYS is set",
		mainnet:      "Must name a version listed in ACCOUNT_CIPHER_KEYS.",
	},
	"ACCOUNT_CIPHER_FINGERPRINT_KEY": {
		secret:       true,
		requiredWhen: "ACCOUNT_CIPHER_KEYS has no `v1` key",
		mainnet:      "Rotation-independent pepper for the blind index; changing it duplicates accounts.",
	},

	"RUN_MIGRATIONS":             {mainnet: "`true` is refused on mainnet unless ALLOW_MAINNET_AUTO_MIGRATE=true. Run migrations as a reviewed pipeline step instead."},
	"ALLOW_MAINNET_AUTO_MIGRATE": {mainnet: "Keep `false`."},

	"LAUNCH_PER_USER_DEPOSIT_CAP": {mainnet: "Empty or `0` disables the cap. Set it for the staged launch."},
	"LAUNCH_GLOBAL_TVL_CAP":       {mainnet: "Empty or `0` disables the cap. Set it for the staged launch."},

	"TRACING_ENABLED":             {mainnet: "If enabled, OTEL_EXPORTER_OTLP_INSECURE must be `false` in production and staging."},
	"OTEL_EXPORTER_OTLP_INSECURE": {mainnet: "Default `true` is rejected when TRACING_ENABLED=true in production or staging."},

	"CANARY_ENABLED":  {mainnet: "Moves real funds on a schedule. Enable only with a dedicated CANARY_VAULT_ID and a small CANARY_AMOUNT."},
	"CANARY_VAULT_ID": {requiredWhen: "CANARY_ENABLED is true", mainnet: "Dedicated canary vault; never a customer vault."},

	"LOG_FORMAT":   {mainnet: "Defaults to `json` when APP_ENV is production or staging, otherwise `text`."},
	"METRICS_ADDR": {mainnet: "Loopback default is safe. Binding it to a public interface exposes route names and traffic volumes; it must not equal the public server address."},

	"RECONCILE_DRY_RUN": {mainnet: "Keep `false` so drift is acted on, not only logged."},
}

// Describe returns every environment variable Load reads, in the order Load
// reads them, de-duplicated by name. It runs the real loader in a mode that
// ignores the process environment, so the recorded defaults are the code's.
func Describe() []VarSpec {
	loader := envLoader{
		errors:   make([]string, 0),
		describe: true,
	}
	_, _ = loadFrom(&loader)

	seen := make(map[string]int, len(loader.vars))
	out := make([]VarSpec, 0, len(loader.vars))
	for _, v := range loader.vars {
		if i, ok := seen[v.Name]; ok {
			// A later read can only add information (e.g. requiredURL records
			// a required string first, then retypes it).
			out[i].Required = out[i].Required || v.Required
			continue
		}
		if n, ok := varNotes[v.Name]; ok {
			v.Secret = n.secret
			v.RequiredWhen = n.requiredWhen
			v.Mainnet = n.mainnet
		}
		seen[v.Name] = len(out)
		out = append(out, v)
	}
	return out
}

// retype corrects the recorded type of a variable already recorded under a
// more generic one (requiredURL reads through requiredString).
func (l *envLoader) retype(key, typ string) {
	if !l.describe {
		return
	}
	for i := len(l.vars) - 1; i >= 0; i-- {
		if l.vars[i].Name == key {
			l.vars[i].Type = typ
			return
		}
	}
}

// RenderMarkdown renders the reference committed at docs/api-config.md.
// Output is deterministic; CI regenerates it and fails on any difference.
func RenderMarkdown(vars []VarSpec) string {
	var b strings.Builder
	b.WriteString("<!-- Code generated by `go run ./cmd/config-doc` from apps/api/internal/config. DO NOT EDIT. -->\n\n")
	b.WriteString("# API configuration reference\n\n")
	b.WriteString("Every environment variable read by `apps/api/internal/config` at startup. ")
	b.WriteString("Regenerate with `cd apps/api && go run ./cmd/config-doc > ../../docs/api-config.md`; ")
	b.WriteString("CI fails when this file is stale.\n\n")
	b.WriteString("- **Required**: `yes` means startup fails when unset. `if …` means required only under that condition.\n")
	b.WriteString("- **Secret**: `yes` means the value belongs in the secrets manager, never in a committed `.env` or a plain config map.\n")
	b.WriteString("- **Mainnet**: constraints enforced at startup, and defaults that are unsafe or must be set deliberately on mainnet.\n")
	b.WriteString("- Values are read from the process environment first, then a local `.env`. Empty values count as unset.\n\n")

	req, sec := 0, 0
	for _, v := range vars {
		if v.Required {
			req++
		}
		if v.Secret {
			sec++
		}
	}
	fmt.Fprintf(&b, "%d variables: %d always required, %d secret.\n\n", len(vars), req, sec)

	b.WriteString("| Name | Type | Default | Required | Secret | Mainnet |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, v := range vars {
		required := "no"
		switch {
		case v.Required:
			required = "yes"
		case v.RequiredWhen != "":
			required = "if " + v.RequiredWhen
		}
		secret := "no"
		if v.Secret {
			secret = "**yes**"
		}
		def := "_none_"
		if v.Default != "" {
			def = "`" + escapeCell(v.Default) + "`"
		}
		mainnet := "—"
		if v.Mainnet != "" {
			mainnet = escapeCell(v.Mainnet)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n", v.Name, v.Type, def, required, secret, mainnet)
	}
	return b.String()
}

func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
