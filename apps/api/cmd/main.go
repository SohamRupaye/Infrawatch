package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/api/handlers"
	"github.com/SohamRupaye/infrawatch/apps/api/middleware"
	"github.com/SohamRupaye/infrawatch/apps/api/store"
	apiwsocket "github.com/SohamRupaye/infrawatch/apps/api/websocket"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/gin-gonic/gin"

	// Import engine config only for loading — its types stay in the engine
	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()
	sugar := logger.Sugar()
	sugar.Info("Infrawatch API starting...")

	cfgPath := os.Getenv("INFRAWATCH_CONFIG")
	if cfgPath == "" {
		cfgPath = "infrawatch.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		sugar.Warnw("config file not found or invalid, using defaults", "path", cfgPath, "error", err)
		cfg = config.Default()
	}

	// Allow DATABASE_URL env var to override config DSN — docker-compose injects
	// this without requiring changes to the mounted yaml file.
	if envDSN := os.Getenv("DATABASE_URL"); envDSN != "" && cfg.Storage.DSN == "" {
		cfg.Storage.DSN = envDSN
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── TimescaleDB reader ─────────────────────────────────────────────────────
	// The API is read-only against the DB.  The engine owns all writes.
	// dbReader is nil when no DSN is configured; all handlers degrade gracefully.
	dbReader, err := store.NewDBReader(
		cfg.Storage.DSN,
		cfg.Storage.MaxOpenConns,
		cfg.Storage.MaxIdleConns,
		logger,
	)
	if err != nil {
		// Non-fatal: log and continue without persistence.
		sugar.Errorw("failed to connect to TimescaleDB — running without persistent storage",
			"error", err)
		dbReader = nil
	}
	if dbReader != nil {
		defer dbReader.Close()
		sugar.Info("TimescaleDB reader connected")
	}

	// ── Redis event bus ────────────────────────────────────────────────────────
	bus, err := enginepkg.NewBus(ctx, cfg.Redis.Addr, cfg.Redis.Password, logger)
	if err != nil {
		sugar.Fatalw("failed to connect to Redis", "error", err)
	}
	defer bus.Close()
	sugar.Info("Redis event bus connected")

	// ── WebSocket hub ──────────────────────────────────────────────────────────
	hub := apiwsocket.NewHub(logger)
	go hub.Run(ctx)

	// ── State store ────────────────────────────────────────────────────────────
	stateStore := store.NewStateStore(logger)

	// Seed the store from TimescaleDB before subscribing to Redis.
	// This ensures the dashboard shows correct state immediately after a restart
	// rather than waiting for the next Redis event.
	if dbReader != nil {
		initCtx, initCancel := context.WithTimeout(ctx, 15*time.Second)
		if err := stateStore.Initialize(initCtx, dbReader); err != nil {
			// Non-fatal: log and continue — the store will self-heal via Redis replay.
			sugar.Warnw("failed to seed state store from DB — operating in degraded mode (Redis replay from stream start); state may be incomplete until all services emit an event",
				"error", err)
		}
		initCancel()
	} else {
		sugar.Warn("TimescaleDB not available — state store running in degraded mode (Redis replay from stream start); dashboard state will build up as events arrive")
	}

	// Subscribe to Redis streams (starts from "$" if DB seeded, "0" otherwise).
	go stateStore.Run(ctx, bus)

	// ── Broadcaster ────────────────────────────────────────────────────────────
	broadcaster := apiwsocket.NewBroadcaster(bus, hub, logger)
	go broadcaster.Run(ctx)

	// ── Handler deps ───────────────────────────────────────────────────────────
	deps := handlers.Deps{
		Bus:   bus,
		Store: stateStore,
		Hub:   hub,
		DB:    dbReader, // nil-safe; handlers check before using
		Cfg: &handlers.APIConfig{
			DockerSocketPath: cfg.Docker.SocketPath,
			JWTSecret:        cfg.API.JWTSecret,
			ConfigPath:       cfgPath,
		},
		Logger: logger,
	}

	// ── HTTP router ────────────────────────────────────────────────────────────
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger(logger))
	r.Use(middleware.CORS(cfg.API.AllowOrigins))

	if cfg.API.JWTSecret == "" {
		sugar.Warn("api.jwt_secret is not set — read endpoints are open; config mutation endpoints (POST/PUT/DELETE) are disabled until a secret is configured")
	}

	// authed covers all read endpoints. If JWTSecret is set every request must
	// carry a valid Bearer JWT; if not set reads are open (dev/no-auth mode).
	authed := r.Group("/")
	if cfg.API.JWTSecret != "" {
		authed.Use(middleware.JWT(cfg.API.JWTSecret))
	}

	// mutationAuth always enforces JWT on config-mutation endpoints. When no
	// secret is configured these routes return 503 with a clear error rather
	// than silently accepting unauthenticated writes.
	mutationAuth := r.Group("/")
	if cfg.API.JWTSecret != "" {
		mutationAuth.Use(middleware.JWT(cfg.API.JWTSecret))
	} else {
		mutationAuth.Use(func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "config mutations are disabled: set api.jwt_secret in the configuration to enable authenticated writes",
			})
		})
	}
	// Rate-limit all mutation endpoints to 30 requests/min per IP.
	// Applied after auth so unauthenticated requests are rejected first.
	mutationAuth.Use(middleware.MutationRateLimit(30))

	v1 := authed.Group("/api/v1")
	mutV1 := mutationAuth.Group("/api/v1")
	{
		svcH := handlers.NewServiceHandler(deps)
		v1.GET("/services", svcH.List)
		v1.GET("/services/:name", svcH.Get)
		tagH := handlers.NewTagSummaryHandler(deps)
		v1.GET("/tags", tagH.List)

		cfgSvcH := handlers.NewConfigServiceHandler(deps)
		v1.GET("/config/services", cfgSvcH.List)
		mutV1.POST("/config/services", cfgSvcH.Create)
		mutV1.PUT("/config/services/:name", cfgSvcH.Update)
		mutV1.DELETE("/config/services/:name", cfgSvcH.Delete)

		metH := handlers.NewMetricsHandler(deps)
		v1.GET("/metrics/:service", metH.Get)
		v1.GET("/metrics/:service/baseline", metH.Baseline)
		v1.GET("/metrics/grouped/by-tag", metH.GroupByTag)

		cbH := handlers.NewCircuitHandler(deps)
		v1.GET("/circuit", cbH.List)
		v1.GET("/circuit/:service", cbH.Get)
		mutV1.POST("/circuit/:service/reset", cbH.Reset)

		healH := handlers.NewHealHandler(deps)
		v1.GET("/healing", healH.History)
		mutV1.POST("/services/:name/heal", healH.Trigger)

		incH := handlers.NewIncidentHandler(deps)
		v1.GET("/incidents", incH.List)
		v1.GET("/incidents/:id", incH.Get)
		v1.GET("/incidents/:id/export", incH.Export)
		v1.GET("/incidents/grouped/by-tag", incH.GroupByTag)

		alertH := handlers.NewAlertHandler(deps)
		v1.GET("/alerts/history", alertH.History)
		mutV1.POST("/alerts/:id/ack", alertH.Acknowledge)

		logH := handlers.NewLogHandler(deps)
		v1.GET("/logs/:container", logH.Tail)
	}

	r.GET("/ws", apiwsocket.ServeWS(hub, logger))
	r.GET("/ws/logs/:container", handlers.NewLogHandler(deps).WSLogs(hub, logger))

	statusH := handlers.NewStatusHandler(deps)
	r.GET("/status", statusH.Page)
	r.GET("/api/public/status", statusH.Data)

	r.GET("/healthz", func(c *gin.Context) {
		status := gin.H{
			"status":    "ok",
			"timestamp": time.Now().UTC(),
			"storage": gin.H{
				"timescaledb": dbReader != nil,
				// seeded=false means the state store is operating in degraded mode:
				// it is replaying the full Redis Stream history instead of starting
				// from a DB snapshot. State will be correct once replay completes.
				"state_store_seeded": stateStore.Seeded(),
			},
			"auth": gin.H{
				"enabled": cfg.API.JWTSecret != "",
			},
		}
		c.JSON(http.StatusOK, status)
	})

	// ── HTTP server ────────────────────────────────────────────────────────────
	addr := cfg.API.Addr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
	}

	go func() {
		sugar.Infow("API server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			sugar.Fatalw("HTTP server error", "error", err)
		}
	}()

	// ── Graceful shutdown ──────────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	sugar.Infow("received signal, shutting down", "signal", sig)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		sugar.Errorw("HTTP server shutdown error", "error", err)
	}
	sugar.Info("Infrawatch API stopped cleanly")
}
