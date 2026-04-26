package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	environment           string
	server                ServerConfig
	database              DatabaseConfig
	stellar               StellarConfig
	redis                 RedisConfig
	settlementProviderURL string
	auth                  AuthConfig
	rateLimit             RateLimitConfig
	log                   LogConfig
	allowedOrigins        []string
	performance           PerformanceConfig
}

type PerformanceConfig struct {
	snapshotInterval time.Duration
}

type ServerConfig struct {
	host             string
	port             int
	readTimeout      time.Duration
	writeTimeout     time.Duration
	gracefulShutdown time.Duration
}

type DatabaseConfig struct {
	dsn               string
	poolSize          int
	connectionTimeout time.Duration
}

type StellarConfig struct {
	networkPassphrase string
	rpcURL            string
	horizonURL        string
	operatorSecret    string
}

type AuthConfig struct {
	secret          string
	tokenExpiry     time.Duration
	challengeExpiry time.Duration
}

type RateLimitConfig struct {
	globalLimit  int
	globalWindow time.Duration
	writeLimit   int
	writeWindow  time.Duration
	walletLimit  int
	walletWindow time.Duration
}

type LogConfig struct {
	level  string
	format string
}

type RedisConfig struct {
	addr string
}

func Load() (*Config, error) {
	fileValues, err := loadDotEnvFile(".env")
	if err != nil {
		return nil, err
	}

	loader := envLoader{
		fileValues: fileValues,
		errors:     make([]string, 0),
	}

	environment := loader.stringDefault("APP_ENV", "development")
	if !isOneOf(environment, "development", "staging", "production", "test") {
		loader.addError("APP_ENV must be one of development, staging, production, test")
	}

	cfg := &Config{
		environment: environment,
		server: ServerConfig{
			host:             loader.stringDefault("SERVER_HOST", "0.0.0.0"),
			port:             loader.intDefault("SERVER_PORT", 8080),
			readTimeout:      loader.durationDefault("SERVER_READ_TIMEOUT", 15*time.Second),
			writeTimeout:     loader.durationDefault("SERVER_WRITE_TIMEOUT", 15*time.Second),
			gracefulShutdown: loader.durationDefault("SERVER_SHUTDOWN_TIMEOUT", 20*time.Second),
		},
		database: DatabaseConfig{
			dsn:               loader.requiredString("DATABASE_DSN"),
			poolSize:          loader.intDefault("DATABASE_POOL_SIZE", 25),
			connectionTimeout: loader.durationDefault("DATABASE_CONNECTION_TIMEOUT", 5*time.Second),
		},
		stellar: StellarConfig{
			networkPassphrase: loader.requiredString("STELLAR_NETWORK_PASSPHRASE"),
			rpcURL:            loader.requiredURL("STELLAR_RPC_URL"),
			horizonURL:        loader.requiredURL("STELLAR_HORIZON_URL"),
			operatorSecret:    loader.stringDefault("STELLAR_OPERATOR_SECRET", ""),
		},
		redis: RedisConfig{
			addr: loader.stringDefault("REDIS_ADDR", ""),
		},
		settlementProviderURL: loader.stringDefault("SETTLEMENT_PROVIDER_URL", ""),
		auth: AuthConfig{
			secret:          loader.requiredString("AUTH_JWT_SECRET"),
			tokenExpiry:     loader.durationDefault("AUTH_TOKEN_EXPIRY", 24*time.Hour),
			challengeExpiry: loader.durationDefault("AUTH_CHALLENGE_EXPIRY", 5*time.Minute),
		},
		rateLimit: RateLimitConfig{
			globalLimit:  loader.intDefault("RATELIMIT_GLOBAL_LIMIT", 100),
			globalWindow: loader.durationDefault("RATELIMIT_GLOBAL_WINDOW", 1*time.Minute),
			writeLimit:   loader.intDefault("RATELIMIT_WRITE_LIMIT", 20),
			writeWindow:  loader.durationDefault("RATELIMIT_WRITE_WINDOW", 1*time.Minute),
			walletLimit:  loader.intDefault("RATELIMIT_WALLET_LIMIT", 60),
			walletWindow: loader.durationDefault("RATELIMIT_WALLET_WINDOW", 1*time.Minute),
		},
		log: LogConfig{
			level:  strings.ToLower(loader.stringDefault("LOG_LEVEL", "info")),
			format: strings.ToLower(loader.stringDefault("LOG_FORMAT", defaultLogFormat(environment))),
		},
		allowedOrigins: loader.stringSliceDefault("ALLOWED_ORIGINS", nil),
		performance: PerformanceConfig{
			snapshotInterval: loader.durationDefault("PERFORMANCE_SNAPSHOT_INTERVAL", 1*time.Hour),
		},
	}

	cfg.validate(&loader)

	if len(loader.errors) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n - %s", strings.Join(loader.errors, "\n - "))
	}

	return cfg, nil
}

func (c Config) Environment() string {
	return c.environment
}

func (c Config) Server() ServerConfig {
	return c.server
}

func (c Config) Database() DatabaseConfig {
	return c.database
}

func (c Config) Stellar() StellarConfig {
	return c.stellar
}

func (c Config) SettlementProviderURL() string {
	return c.settlementProviderURL
}

func (c Config) Auth() AuthConfig {
	return c.auth
}

func (c Config) RateLimit() RateLimitConfig {
	return c.rateLimit
}

func (c Config) Log() LogConfig {
	return c.log
}

func (c Config) Redis() RedisConfig {
	return c.redis
}

func (c Config) Performance() PerformanceConfig {
	return c.performance
}

func (p PerformanceConfig) SnapshotInterval() time.Duration {
	return p.snapshotInterval
}

// AllowedOrigins returns the list of origins permitted to make cross-origin
// requests to the API. An empty slice disables cross-origin access.
func (c Config) AllowedOrigins() []string {
	out := make([]string, len(c.allowedOrigins))
	copy(out, c.allowedOrigins)
	return out
}

func (r RedisConfig) Addr() string {
	return r.addr
}

func (c *Config) validate(loader *envLoader) {
	if strings.TrimSpace(c.server.host) == "" {
		loader.addError("SERVER_HOST is required")
	}

	if c.server.port <= 0 || c.server.port > 65535 {
		loader.addError("SERVER_PORT must be between 1 and 65535")
	}

	if c.server.readTimeout <= 0 {
		loader.addError("SERVER_READ_TIMEOUT must be greater than 0")
	}

	if c.server.writeTimeout <= 0 {
		loader.addError("SERVER_WRITE_TIMEOUT must be greater than 0")
	}

	if c.server.gracefulShutdown <= 0 {
		loader.addError("SERVER_SHUTDOWN_TIMEOUT must be greater than 0")
	}

	if c.database.poolSize <= 0 {
		loader.addError("DATABASE_POOL_SIZE must be greater than 0")
	}

	if c.database.connectionTimeout <= 0 {
		loader.addError("DATABASE_CONNECTION_TIMEOUT must be greater than 0")
	}

	if len(strings.TrimSpace(c.auth.secret)) < 32 {
		loader.addError("AUTH_JWT_SECRET must be at least 32 characters")
	}

	if c.auth.tokenExpiry <= 0 {
		loader.addError("AUTH_TOKEN_EXPIRY must be greater than 0")
	}

	if c.auth.challengeExpiry <= 0 {
		loader.addError("AUTH_CHALLENGE_EXPIRY must be greater than 0")
	}

	if c.rateLimit.globalLimit <= 0 {
		loader.addError("RATELIMIT_GLOBAL_LIMIT must be greater than 0")
	}

	if c.rateLimit.globalWindow <= 0 {
		loader.addError("RATELIMIT_GLOBAL_WINDOW must be greater than 0")
	}

	if c.rateLimit.writeLimit <= 0 {
		loader.addError("RATELIMIT_WRITE_LIMIT must be greater than 0")
	}

	if c.rateLimit.writeWindow <= 0 {
		loader.addError("RATELIMIT_WRITE_WINDOW must be greater than 0")
	}

	if c.rateLimit.walletLimit <= 0 {
		loader.addError("RATELIMIT_WALLET_LIMIT must be greater than 0")
	}

	if c.rateLimit.walletWindow <= 0 {
		loader.addError("RATELIMIT_WALLET_WINDOW must be greater than 0")
	}

	if !isOneOf(c.log.level, "debug", "info", "warn", "error") {
		loader.addError("LOG_LEVEL must be one of debug, info, warn, error")
	}

	if !isOneOf(c.log.format, "json", "text") {
		loader.addError("LOG_FORMAT must be one of json, text")
	}

	validateAllowedOrigins(c.environment, c.allowedOrigins, loader)

	if c.performance.snapshotInterval <= 0 {
		loader.addError("PERFORMANCE_SNAPSHOT_INTERVAL must be greater than 0")
	}
}

func validateAllowedOrigins(environment string, origins []string, loader *envLoader) {
	if (environment == "production" || environment == "staging") && len(origins) == 0 {
		loader.addError("ALLOWED_ORIGINS must list at least one origin in production or staging")
	}

	for _, origin := range origins {
		if origin == "*" {
			loader.addError("ALLOWED_ORIGINS must not contain wildcard \"*\"; list explicit origins instead")
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			loader.addError(fmt.Sprintf("ALLOWED_ORIGINS entry %q is not a valid origin (expected scheme://host[:port])", origin))
			continue
		}
		if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			loader.addError(fmt.Sprintf("ALLOWED_ORIGINS entry %q must not contain a path, query, or fragment", origin))
		}
	}
}

func (s ServerConfig) Host() string {
	return s.host
}

func (s ServerConfig) Port() int {
	return s.port
}

func (s ServerConfig) ReadTimeout() time.Duration {
	return s.readTimeout
}

func (s ServerConfig) WriteTimeout() time.Duration {
	return s.writeTimeout
}

func (s ServerConfig) GracefulShutdown() time.Duration {
	return s.gracefulShutdown
}

func (s ServerConfig) Address() string {
	return net.JoinHostPort(s.host, strconv.Itoa(s.port))
}

func (d DatabaseConfig) DSN() string {
	return d.dsn
}

func (d DatabaseConfig) PoolSize() int {
	return d.poolSize
}

func (d DatabaseConfig) ConnectionTimeout() time.Duration {
	return d.connectionTimeout
}

func (s StellarConfig) NetworkPassphrase() string {
	return s.networkPassphrase
}

func (s StellarConfig) RPCURL() string {
	return s.rpcURL
}

func (s StellarConfig) HorizonURL() string {
	return s.horizonURL
}

func (s StellarConfig) OperatorSecret() string {
	return s.operatorSecret
}

func (l LogConfig) Level() string {
	return l.level
}

func (l LogConfig) Format() string {
	return l.format
}

func (a AuthConfig) Secret() string {
	return a.secret
}

func (a AuthConfig) TokenExpiry() time.Duration {
	return a.tokenExpiry
}

func (a AuthConfig) ChallengeExpiry() time.Duration {
	return a.challengeExpiry
}

func (r RateLimitConfig) GlobalLimit() int {
	return r.globalLimit
}

func (r RateLimitConfig) GlobalWindow() time.Duration {
	return r.globalWindow
}

func (r RateLimitConfig) WriteLimit() int {
	return r.writeLimit
}

func (r RateLimitConfig) WriteWindow() time.Duration {
	return r.writeWindow
}

func (r RateLimitConfig) WalletLimit() int {
	return r.walletLimit
}

func (r RateLimitConfig) WalletWindow() time.Duration {
	return r.walletWindow
}

type envLoader struct {
	fileValues map[string]string
	errors     []string
}

func (l *envLoader) requiredString(key string) string {
	value, ok := l.lookup(key)
	if !ok {
		l.addError(key + " is required")
		return ""
	}
	return value
}

func (l *envLoader) requiredURL(key string) string {
	value := l.requiredString(key)
	if value == "" {
		return ""
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		l.addError(fmt.Sprintf("%s must be a valid absolute URL", key))
		return ""
	}
	return value
}

func (l *envLoader) stringDefault(key, fallback string) string {
	if value, ok := l.lookup(key); ok {
		return value
	}
	return fallback
}

func (l *envLoader) intDefault(key string, fallback int) int {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be an integer, got %q", key, raw))
		return fallback
	}
	return value
}

func (l *envLoader) stringSliceDefault(key string, fallback []string) []string {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (l *envLoader) durationDefault(key string, fallback time.Duration) time.Duration {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be a valid duration, got %q", key, raw))
		return fallback
	}
	return value
}

func (l *envLoader) lookup(key string) (string, bool) {
	if value, ok := os.LookupEnv(key); ok {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, true
		}
	}

	value, ok := l.fileValues[key]
	if !ok {
		return "", false
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func (l *envLoader) addError(message string) {
	l.errors = append(l.errors, message)
}

func loadDotEnvFile(path string) (map[string]string, error) {
	values, err := godotenv.Read(path)
	if err == nil {
		return values, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	return nil, fmt.Errorf("load .env: %w", err)
}

func defaultLogFormat(environment string) string {
	if environment == "production" || environment == "staging" {
		return "json"
	}
	return "text"
}

func isOneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
