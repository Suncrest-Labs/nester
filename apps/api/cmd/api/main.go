package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
	dbpkg "github.com/suncrestlabs/nester/apps/api/internal/db"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/server"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	stellarpkg "github.com/suncrestlabs/nester/apps/api/internal/stellar"
	"github.com/suncrestlabs/nester/apps/api/internal/ws"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	startedAt := time.Now()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	baseLogger, err := logpkg.New(cfg.Log(), version)
	if err != nil {
		return err
	}

	pgPool, err := repository.NewPostgresDB(cfg.Database())
	if err != nil {
		return err
	}
	defer pgPool.Pool.Close()

	db := stdlib.OpenDBFromPool(pgPool.Pool)
	defer db.Close()

	if cfg.Startup().EnableAutoMigrate() {
		baseLogger.Info("running database migrations", "dir", cfg.Startup().MigrationsDir())
		if err := dbpkg.MigrateUp(db, cfg.Startup().MigrationsDir()); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
		baseLogger.Info("database migrations complete")
	} else {
		baseLogger.Info("auto-migrate disabled; skipping migrations")
	}

	if err := pingStellarDependencies(baseLogger, cfg); err != nil {
		return err
	}

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultHandler := handler.NewVaultHandler(vaultService)

	transactionRepository := postgres.NewTransactionRepository(db)
	transactionService := service.NewTransactionService(transactionRepository, cfg.Stellar().HorizonURL())
	transactionHandler := handler.NewTransactionHandler(transactionService)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository)
	settlementHandler := handler.NewSettlementHandler(settlementService)

	userRepository := postgres.NewUserRepository(db)
	userService := service.NewUserService(userRepository)
	userHandler := handler.NewUserHandler(userService)

	adminRepository := postgres.NewAdminRepository(db)

	var chainInvoker service.VaultChainInvoker
	if secret := cfg.Stellar().OperatorSecret(); secret != "" {
		inv, err := service.NewSorobanVaultChainInvoker(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().HorizonURL(),
			cfg.Stellar().NetworkPassphrase(),
			secret,
		)
		if err != nil {
			return fmt.Errorf("init chain invoker: %w", err)
		}
		chainInvoker = inv
		vaultService.SetDepositInvoker(inv)
	}

	adminService := service.NewAdminService(
		adminRepository,
		chainInvoker,
		cfg.Stellar().HorizonURL(),
		cfg.SettlementProviderURL(),
	)
	adminHandler := handler.NewAdminHandler(adminService)
	adminHandler.SetEventSyncer(&mainEventSyncer{
		db:     db,
		rpcURL: cfg.Stellar().RPCURL(),
		logger: baseLogger,
	})

	var challengeStore service.ChallengeStore
	if addr := cfg.Redis().Addr(); addr != "" {
		redisClient := redis.NewClient(&redis.Options{Addr: addr})
		challengeStore = service.NewRedisChallengeStore(redisClient, cfg.Auth().ChallengeExpiry())
		baseLogger.Info("challenge store: redis", "addr", addr)
	} else {
		challengeStore = service.NewInMemoryChallengeStore(cfg.Auth().ChallengeExpiry())
		baseLogger.Info("challenge store: in-memory (single-instance only)")
	}
	authService := service.NewAuthService(challengeStore, userService, cfg.Auth())
	authHandler := handler.NewAuthHandler(authService)

	oracleService := oracle.NewRateService(cfg.Stellar().HorizonURL())
	rateHandler := handler.NewRateHandler(oracleService)
	
	wsHub := ws.NewHub(baseLogger.WithGroup("websocket"), func(token string) (string, error) {
		// Token validation should be performed here. 
		// Return userID if successful, error otherwise.
		if token == "" {
			return "", fmt.Errorf("missing token")
		}
		return "user-id-from-token", nil // Placeholder for actual JWT validation
	})
	
	wsCtx, wsCancel := context.WithCancel(context.Background())
	defer wsCancel()
	go wsHub.Run(wsCtx)

	performanceRepository := postgres.NewPerformanceRepository(db)
	performanceService := performancesvc.NewService(performanceRepository)
	performanceHandler := handler.NewPerformanceHandler(performanceService)

	tracker := performancesvc.NewTracker(
		performanceRepository,
		vaultRepository,
		nil, // BalanceProvider: wire to a Stellar adapter once the on-chain reader is exposed.
		cfg.Performance().SnapshotInterval(),
	)
	trackerCtx, cancelTracker := context.WithCancel(context.Background())
	defer cancelTracker()
	go func() {
		if err := tracker.Run(trackerCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("performance tracker stopped", "error", err.Error())
		}
	}()

	// ready tracks whether the server is accepting traffic. Flipped to false on
	// shutdown so load balancers stop routing before in-flight drain begins.
	var ready atomic.Bool
	ready.Store(true)

	depHTTPClient := &http.Client{Timeout: cfg.Startup().DependencyTimeout()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", livenessHandler(&ready))
	mux.HandleFunc("GET /healthz", livenessHandler(&ready))
	mux.HandleFunc("GET /readyz", readinessHandler(&ready, pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /health/detailed", detailedHealthHandler(detailedHealthDeps{
		ready:        &ready,
		pgPool:       pgPool,
		dbTimeout:    cfg.Database().ConnectionTimeout(),
		httpClient:   depHTTPClient,
		horizonURL:   cfg.Stellar().HorizonURL(),
		rpcURL:       cfg.Stellar().RPCURL(),
		startedAt:    startedAt,
		environment:  cfg.Environment(),
		buildVersion: version,
	}))
	vaultHandler.Register(mux)
	transactionHandler.Register(mux)
	settlementHandler.Register(mux)
	userHandler.Register(mux)
	adminHandler.Register(mux)
	authHandler.Register(mux)
	rateHandler.Register(mux)
	performanceHandler.Register(mux)

	mux.HandleFunc("GET /ws", wsHub.ServeWs)

	authRules := []middleware.RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/ws", Public: true},
		{PathPrefix: "/api/v1/auth/", Public: true},
		{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
		{PathPrefix: "/api/v1/", Public: false},
	}
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), authRules)
	// Global rate limit applies to all requests per IP.
	globalLimiter := middleware.IPRateLimiter(cfg.RateLimit().GlobalLimit(), cfg.RateLimit().GlobalWindow())
	// Write rate limit is stricter and applies only to mutating methods (POST/PUT/PATCH/DELETE).
	writeLimiter := middleware.WriteMethodRateLimiter(cfg.RateLimit().WriteLimit(), cfg.RateLimit().WriteWindow())
	// Wallet rate limit runs inside Authenticate so the caller's wallet address
	// is in context. Unauthenticated requests produce an empty key and pass
	// through — public routes are covered by the IP and write limiters above.
	walletLimiter := middleware.WalletRateLimiter(
		cfg.RateLimit().WalletLimit(),
		cfg.RateLimit().WalletWindow(),
		walletKeyFromContext,
	)
	// CORS sits inside rate limiting (preflights count against the bucket) and
	// outside auth (preflights don't carry credentials and must short-circuit
	// before Authenticate rejects them).
	cors := middleware.CORS(cfg.AllowedOrigins())

	server := &http.Server{
		Addr: cfg.Server().Address(),
		Handler: middleware.SecurityHeaders(cfg.Environment())(
			middleware.RecoverPanic(baseLogger)(
				globalLimiter(
					cors(
						writeLimiter(
							authenticator(
								walletLimiter(
									middleware.LimitRequestBody(1 * 1024 * 1024)(
										middleware.Logging(baseLogger)(mux),
									),
								),
							),
						),
					),
				),
			),
		),
		ReadTimeout:       cfg.Server().ReadTimeout(),
		ReadHeaderTimeout: cfg.Server().ReadHeaderTimeout(),
		WriteTimeout:      cfg.Server().WriteTimeout(),
		IdleTimeout:       cfg.Server().IdleTimeout(),
		MaxHeaderBytes:    cfg.Server().MaxHeaderBytes(),
	}

	baseLogger.Info("starting server",
		"addr", cfg.Server().Address(),
		"environment", cfg.Environment(),
		"version", version,
		"horizon_url", cfg.Stellar().HorizonURL(),
		"rpc_url", cfg.Stellar().RPCURL(),
		"network_passphrase", cfg.Stellar().NetworkPassphrase(),
		"auto_migrate", cfg.Startup().EnableAutoMigrate(),
	)

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startEventIndexer(shutdownCtx, baseLogger, db, cfg.Stellar().RPCURL())

	serverErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-shutdownCtx.Done():
		baseLogger.Info("shutdown signal received, draining")
	}

	stop()

	// Flip readiness immediately so load balancers stop routing new traffic
	// to this instance before we wait for in-flight requests to complete.
	ready.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server().GracefulShutdown())
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		baseLogger.Error("graceful shutdown timed out", "error", err.Error())
		return err
	}

	if err := <-serverErr; err != nil {
		return err
	}

	baseLogger.Info("server stopped",
		"uptime", time.Since(startedAt).String(),
	)
	return nil
}

// walletKeyFromContext returns the authenticated caller's wallet address for
// per-wallet rate limiting. An empty string means no key is available (public
// route or claims without a wallet) — WalletRateLimiter passes those through.
func walletKeyFromContext(r *http.Request) string {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.WalletAddress
}

// livenessHandler returns 200 while the process is healthy and 503 once
// shutdown has begun. It performs no external calls so load balancers can
// poll it cheaply.
func livenessHandler(ready *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// readinessHandler returns 200 only when the database is reachable and the
// process is not draining. Used by orchestrators that need to know when this
// instance can serve traffic.
func readinessHandler(ready *atomic.Bool, db *repository.PostgresDB, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

type detailedHealthDeps struct {
	ready        *atomic.Bool
	pgPool       *repository.PostgresDB
	dbTimeout    time.Duration
	httpClient   *http.Client
	horizonURL   string
	rpcURL       string
	startedAt    time.Time
	environment  string
	buildVersion string
}

type dependencyStatus struct {
	OK            bool   `json:"ok"`
	Endpoint      string `json:"endpoint,omitempty"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	LatestLedger  uint64 `json:"latest_ledger,omitempty"`
}

type dbStatus struct {
	OK            bool   `json:"ok"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	MaxConns      int32  `json:"max_conns"`
	AcquiredConns int32  `json:"acquired_conns"`
	IdleConns     int32  `json:"idle_conns"`
	TotalConns    int32  `json:"total_conns"`
}

type detailedHealthResponse struct {
	Status       string           `json:"status"`
	Environment  string           `json:"environment"`
	Version      string           `json:"version"`
	UptimeSecs   int64            `json:"uptime_seconds"`
	Database     dbStatus         `json:"database"`
	Horizon      dependencyStatus `json:"horizon"`
	SorobanRPC   dependencyStatus `json:"soroban_rpc"`
	GeneratedAt  time.Time        `json:"generated_at"`
}

func detailedHealthHandler(deps detailedHealthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := detailedHealthResponse{
			Status:      "ok",
			Environment: deps.environment,
			Version:     deps.buildVersion,
			UptimeSecs:  int64(time.Since(deps.startedAt).Seconds()),
			GeneratedAt: time.Now().UTC(),
		}

		// Database
		dbCtx, dbCancel := context.WithTimeout(r.Context(), deps.dbTimeout)
		dbStart := time.Now()
		dbErr := deps.pgPool.Ping(dbCtx)
		dbCancel()
		stat := deps.pgPool.Pool.Stat()
		resp.Database = dbStatus{
			OK:            dbErr == nil,
			LatencyMillis: time.Since(dbStart).Milliseconds(),
			MaxConns:      stat.MaxConns(),
			AcquiredConns: stat.AcquiredConns(),
			IdleConns:     stat.IdleConns(),
			TotalConns:    stat.TotalConns(),
		}
		if dbErr != nil {
			resp.Database.Error = dbErr.Error()
		}

		// Horizon
		hStart := time.Now()
		hRes := stellarpkg.PingHorizon(r.Context(), deps.httpClient, deps.horizonURL)
		resp.Horizon = dependencyStatus{
			OK:            hRes.OK,
			Endpoint:      hRes.Endpoint,
			Error:         hRes.Error,
			LatencyMillis: time.Since(hStart).Milliseconds(),
			LatestLedger:  hRes.LatestLedger,
		}

		// Soroban RPC
		rStart := time.Now()
		rRes := stellarpkg.PingSorobanRPC(r.Context(), deps.httpClient, deps.rpcURL)
		resp.SorobanRPC = dependencyStatus{
			OK:            rRes.OK,
			Endpoint:      rRes.Endpoint,
			Error:         rRes.Error,
			LatencyMillis: time.Since(rStart).Milliseconds(),
			LatestLedger:  rRes.LatestLedger,
		}

		degraded := !resp.Database.OK || !resp.Horizon.OK || !resp.SorobanRPC.OK
		draining := !deps.ready.Load()
		switch {
		case draining:
			resp.Status = "draining"
		case degraded:
			resp.Status = "degraded"
		}

		status := http.StatusOK
		if draining || !resp.Database.OK {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// pingStellarDependencies verifies Horizon and Soroban RPC are reachable
// before the server starts accepting traffic. Failure here exits the process
// so orchestrators can restart against a healthy node.
func pingStellarDependencies(logger *slog.Logger, cfg *config.Config) error {
	timeout := cfg.Startup().DependencyTimeout()
	client := &http.Client{Timeout: timeout}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if res := stellarpkg.PingHorizon(ctx, client, cfg.Stellar().HorizonURL()); !res.OK {
		return fmt.Errorf("horizon unreachable at %s: %s", cfg.Stellar().HorizonURL(), res.Error)
	} else {
		logger.Info("horizon reachable", "url", cfg.Stellar().HorizonURL(), "latest_ledger", res.LatestLedger)
	}

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), timeout)
	defer rpcCancel()
	if res := stellarpkg.PingSorobanRPC(rpcCtx, client, cfg.Stellar().RPCURL()); !res.OK {
		return fmt.Errorf("soroban rpc unreachable at %s: %s", cfg.Stellar().RPCURL(), res.Error)
	} else {
		logger.Info("soroban rpc reachable", "url", cfg.Stellar().RPCURL(), "latest_ledger", res.LatestLedger)
	}

	return nil
}

func startEventIndexer(ctx context.Context, logger *slog.Logger, db *sql.DB, rpcURL string) {
	if strings.TrimSpace(rpcURL) == "" {
		logger.Warn("event indexer disabled: STELLAR_RPC_URL is empty")
		return
	}
	if err := ensureIndexerTables(ctx, db); err != nil {
		logger.Error("event indexer disabled: failed to initialize tables", "error", err)
		return
	}

	go func() {
		client := &http.Client{Timeout: 8 * time.Second}
		ticker := time.NewTicker(6 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				startLedger, err := getLastIndexedLedger(ctx, db)
				if err != nil {
					logger.Error("event indexer failed to load cursor", "error", err)
					continue
				}

				contractIDs, err := loadVaultContractIDs(ctx, db)
				if err != nil {
					logger.Error("event indexer failed to load vault contracts", "error", err)
					continue
				}
				if len(contractIDs) == 0 {
					continue
				}

				events, latestLedger, err := fetchSorobanEvents(ctx, client, rpcURL, contractIDs, startLedger)
				if err != nil {
					logger.Error("event indexer fetch failed", "error", err)
					continue
				}

				for _, event := range events {
					processed, err := applyIndexedEvent(ctx, db, event)
					if err != nil {
						logger.Error("event indexer failed to apply event", "event_id", event.ID, "contract_id", event.ContractID, "event_type", event.EventType, "error", err)
						continue
					}
					if !processed {
						logger.Debug("event indexer skipped duplicate event", "event_id", event.ID)
					}
				}

				if err := setLastIndexedLedger(ctx, db, latestLedger); err != nil {
					logger.Error("event indexer failed to persist cursor", "ledger", latestLedger, "error", err)
				}
			}
		}
	}()
}

func loadVaultContractIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT DISTINCT contract_address FROM vaults WHERE deleted_at IS NULL AND contract_address <> ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contractIDs := make([]string, 0)
	for rows.Next() {
		var contractID string
		if err := rows.Scan(&contractID); err != nil {
			return nil, err
		}
		contractIDs = append(contractIDs, contractID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return contractIDs, nil
}

type indexedEvent struct {
	ID         string
	ContractID string
	EventType  string
	Ledger     uint64
	Data       map[string]any
}

func applyIndexedEvent(ctx context.Context, db *sql.DB, event indexedEvent) (bool, error) {
	if strings.TrimSpace(event.ID) == "" {
		return false, fmt.Errorf("event id is required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	inserted, err := markEventProcessed(ctx, tx, event)
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, tx.Commit()
	}

	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "pause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'paused', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "unpause":
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults SET status = 'active', updated_at = NOW() WHERE contract_address = $1 AND deleted_at IS NULL`,
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "deposit":
		amount, ok := extractEventAmount(event)
		if !ok {
			return false, fmt.Errorf("deposit event missing parseable amount")
		}
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET total_deposited = total_deposited + $1::numeric,
			     current_balance = current_balance + $1::numeric,
			     updated_at = NOW()
			 WHERE contract_address = $2 AND deleted_at IS NULL`,
			amount.String(),
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	case "withdraw", "withdrawal":
		amount, ok := extractEventAmount(event)
		if !ok {
			return false, fmt.Errorf("withdraw event missing parseable amount")
		}
		_, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET current_balance = current_balance - $1::numeric,
			     updated_at = NOW()
			 WHERE contract_address = $2 AND deleted_at IS NULL`,
			amount.String(),
			event.ContractID,
		)
		if err != nil {
			return false, err
		}
	default:
		// Keep cursor continuity even for unsupported events.
	}

	return true, tx.Commit()
}

func extractEventAmount(event indexedEvent) (decimal.Decimal, bool) {
	if event.Data == nil {
		return decimal.Zero, false
	}

	for _, key := range []string{"amount", "value"} {
		raw, ok := event.Data[key]
		if !ok {
			continue
		}

		switch v := raw.(type) {
		case string:
			value, err := decimal.NewFromString(strings.TrimSpace(v))
			if err != nil {
				return decimal.Zero, false
			}
			return value, true
		case json.Number:
			value, err := decimal.NewFromString(v.String())
			if err != nil {
				return decimal.Zero, false
			}
			return value, true
		case int:
			return decimal.NewFromInt(int64(v)), true
		case int64:
			return decimal.NewFromInt(v), true
		case float64:
			// Avoid binary-float conversion: parse the decimal text form instead.
			value, err := decimal.NewFromString(fmt.Sprintf("%v", v))
			if err != nil {
				return decimal.Zero, false
			}
			return value, true
		}
	}

	return decimal.Zero, false
}

func fetchSorobanEvents(
	ctx context.Context,
	client *http.Client,
	rpcURL string,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "nester-indexer",
		"method":  "getEvents",
		"params": map[string]any{
			"startLedger": startLedger,
			"filters": []map[string]any{
				{
					"type":      "contract",
					"contractIds": contractIDs,
				},
			},
			"pagination": map[string]any{"limit": 200},
		},
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("rpc returned %d: %s", resp.StatusCode, string(payload))
	}

	var rpcResp struct {
		Result struct {
			LatestLedger uint64 `json:"latestLedger"`
			Events       []struct {
				ID         string         `json:"id"`
				ContractID string         `json:"contractId"`
				Ledger     uint64         `json:"ledger"`
				Topic      []interface{}  `json:"topic"`
				Value      map[string]any `json:"value"`
			} `json:"events"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&rpcResp); err != nil {
		return nil, 0, err
	}
	if rpcResp.Error != nil {
		return nil, 0, fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}

	events := make([]indexedEvent, 0, len(rpcResp.Result.Events))
	for _, raw := range rpcResp.Result.Events {
		eventType := ""
		if len(raw.Topic) > 0 {
			if topic, ok := raw.Topic[0].(string); ok {
				eventType = topic
			}
		}
		if eventType == "" {
			continue
		}
		events = append(events, indexedEvent{
			ID:         raw.ID,
			ContractID: raw.ContractID,
			EventType:  eventType,
			Ledger:     raw.Ledger,
			Data:       raw.Value,
		})
	}

	return events, rpcResp.Result.LatestLedger, nil
}

func ensureIndexerTables(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS event_indexer_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_indexed_ledger BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS processed_chain_events (
    event_id TEXT PRIMARY KEY,
    contract_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    ledger BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return err
	}

	_, err := db.ExecContext(ctx, `
INSERT INTO event_indexer_state (id, last_indexed_ledger)
VALUES (1, 0)
ON CONFLICT (id) DO NOTHING`)
	return err
}

func getLastIndexedLedger(ctx context.Context, db *sql.DB) (uint64, error) {
	var ledger uint64
	err := db.QueryRowContext(ctx, `SELECT last_indexed_ledger FROM event_indexer_state WHERE id = 1`).Scan(&ledger)
	if err != nil {
		return 0, err
	}
	return ledger, nil
}

func setLastIndexedLedger(ctx context.Context, db *sql.DB, ledger uint64) error {
	_, err := db.ExecContext(ctx, `
UPDATE event_indexer_state
SET last_indexed_ledger = GREATEST(last_indexed_ledger, $1::bigint),
    updated_at = NOW()
WHERE id = 1`, ledger)
	return err
}

func markEventProcessed(ctx context.Context, tx *sql.Tx, event indexedEvent) (bool, error) {
	result, err := tx.ExecContext(ctx, `
INSERT INTO processed_chain_events (event_id, contract_id, event_type, ledger)
VALUES ($1, $2, $3, $4)
ON CONFLICT (event_id) DO NOTHING`,
		event.ID,
		event.ContractID,
		event.EventType,
		event.Ledger,
	)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected == 1, nil
}

// mainEventSyncer implements handler.EventSyncer using the same indexer
// helpers defined in this package, allowing the admin handler to trigger
// a manual one-shot sync for recovery purposes.
type mainEventSyncer struct {
	db     *sql.DB
	rpcURL string
	logger *slog.Logger
}

func (s *mainEventSyncer) SyncEvents(ctx context.Context) (int, error) {
	if err := ensureIndexerTables(ctx, s.db); err != nil {
		return 0, fmt.Errorf("indexer tables: %w", err)
	}

	startLedger, err := getLastIndexedLedger(ctx, s.db)
	if err != nil {
		return 0, fmt.Errorf("load cursor: %w", err)
	}

	contractIDs, err := loadVaultContractIDs(ctx, s.db)
	if err != nil {
		return 0, fmt.Errorf("load contracts: %w", err)
	}
	if len(contractIDs) == 0 {
		return 0, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	events, latestLedger, err := fetchSorobanEvents(ctx, client, s.rpcURL, contractIDs, startLedger)
	if err != nil {
		return 0, fmt.Errorf("fetch events: %w", err)
	}

	processed := 0
	for _, event := range events {
		ok, err := applyIndexedEvent(ctx, s.db, event)
		if err != nil {
			s.logger.Error("admin sync: failed to apply event", "event_id", event.ID, "error", err)
			continue
		}
		if ok {
			processed++
		}
	}

	if err := setLastIndexedLedger(ctx, s.db, latestLedger); err != nil {
		s.logger.Error("admin sync: failed to persist cursor", "ledger", latestLedger, "error", err)
	}

	return processed, nil
}
