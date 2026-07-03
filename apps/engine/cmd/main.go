package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/alerts"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/anomaly"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/circuit"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/events"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/healing"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/health"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/storage"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/SohamRupaye/infrawatch/apps/engine/streams"
	"go.uber.org/zap"
)

func main() {
	// Bootstrap logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	sugar := logger.Sugar()
	sugar.Info("Infrawatch engine starting...")

	// Load config
	cfgPath := os.Getenv("INFRAWATCH_CONFIG")
	if cfgPath == "" {
		cfgPath = "infrawatch.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		sugar.Warnw("config file not found or invalid, using defaults", "path", cfgPath, "error", err)
		cfg = config.Default()
	}

	if err := config.Validate(cfg); err != nil {
		sugar.Fatalw("invalid config", "error", err)
	}

	// Allow DATABASE_URL env var to override config DSN — useful in Docker where
	// the env var is injected by docker-compose without touching the yaml file.
	if envDSN := os.Getenv("DATABASE_URL"); envDSN != "" && cfg.Storage.DSN == "" {
		cfg.Storage.DSN = envDSN
	}

	// Build context with cancellation for clean shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── TimescaleDB ────────────────────────────────────────────────────────────
	// Persistence is optional — the engine degrades gracefully if the DB is
	// unavailable.  A nil *storage.DB is safe to pass to StateManager (all writes
	// become no-ops).
	var db *storage.DB
	if cfg.Storage.DSN != "" {
		sugar.Infow("connecting to TimescaleDB", "dsn_len", len(cfg.Storage.DSN))

		db, err = storage.New(
			cfg.Storage.DSN,
			cfg.Storage.MaxOpenConns,
			cfg.Storage.MaxIdleConns,
			logger,
		)
		if err != nil {
			// Fatal — if a DSN is configured we must be able to reach it.
			// Remove the DSN from config to run without persistence instead.
			sugar.Fatalw("failed to connect to TimescaleDB", "error", err)
		}
		defer db.Close()
		sugar.Info("TimescaleDB connected")

		// Run schema migrations — idempotent, safe on every restart.
		schemaCtx, schemaCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := db.SetupSchema(schemaCtx); err != nil {
			schemaCancel()
			sugar.Fatalw("TimescaleDB schema setup failed", "error", err)
		}
		schemaCancel()
		sugar.Info("TimescaleDB schema ready")
	} else {
		sugar.Warn("storage.dsn not set — running without TimescaleDB persistence (metrics and incidents will not be persisted)")
	}

	// ── Redis event bus ────────────────────────────────────────────────────────
	bus, err := events.NewBus(ctx, cfg.Redis.Addr, cfg.Redis.Password, logger)
	if err != nil {
		sugar.Fatalw("failed to connect to Redis", "error", err)
	}
	defer bus.Close()
	sugar.Info("Redis event bus connected")

	// ── Supporting subsystems ──────────────────────────────────────────────────
	cbRegistry := circuit.NewRegistry(cfg.Circuit, logger)
	anomalyDetector := anomaly.NewDetector(cfg.Anomaly, logger)

	alertDispatcher, err := alerts.NewDispatcher(cfg.Alerts, db, logger)
	if err != nil {
		sugar.Fatalw("failed to create alert dispatcher", "error", err)
	}

	healer, err := healing.NewHealer(cfg.Healing, cfg.Docker.SocketPath, logger)
	if err != nil {
		sugar.Fatalw("failed to create healer", "error", err)
	}

	// ── State machine ──────────────────────────────────────────────────────────
	// db is passed through — nil is fine, all DB calls become no-ops.
	stateMgr := health.NewStateManager(bus, alertDispatcher, healer, anomalyDetector, cbRegistry, db, logger)

	// ── Health poller ──────────────────────────────────────────────────────────
	poller := health.NewPoller(cfg, stateMgr, cbRegistry, anomalyDetector, bus, logger)

	sugar.Infow("starting engine", "services", len(cfg.Services))
	poller.Start(ctx)

	// ── External alert signal listener ────────────────────────────────────────
	// Passive-mode services are driven by external signals (e.g. Alertmanager)
	// instead of the poller's own HTTP checks.
	alertSignalHandler := health.NewAlertSignalHandler(poller, stateMgr, logger)
	go bus.Subscribe(ctx, streams.ExternalAlerts, "$", func(_ string, payload []byte) {
		var sig enginepkg.ExternalAlertSignal
		if err := json.Unmarshal(payload, &sig); err != nil {
			sugar.Warnw("invalid external alert signal payload", "error", err)
			return
		}
		alertSignalHandler.Handle(ctx, health.AlertSignal{
			ServiceName: sig.ServiceName,
			Status:      sig.Status,
			Reason:      sig.Reason,
		})
	})

	// ── Anomaly event forwarding ───────────────────────────────────────────────
	// Drain detected anomalies and publish them to Redis so the API/dashboard
	// actually see them — detection alone only logs locally.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case a := <-anomalyDetector.Anomalies():
				bus.PublishAnomaly(ctx, events.AnomalyEvent{
					ServiceName: a.ServiceName,
					Type:        string(a.Type),
					Message:     a.Message,
					Value:       a.Value,
					Baseline:    a.Baseline,
					Timestamp:   a.Timestamp,
				})
			}
		}
	}()

	// ── API command listeners ──────────────────────────────────────────────────
	// Subscribe to API-initiated circuit reset commands
	go bus.Subscribe(ctx, streams.CircuitResets, "$", func(_ string, payload []byte) {
		var cmd enginepkg.CircuitResetCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			sugar.Warnw("invalid circuit reset command payload", "error", err)
			return
		}
		if !cbRegistry.Reset(cmd.ServiceName) {
			sugar.Warnw("circuit reset requested for unknown service", "service", cmd.ServiceName)
			return
		}
		sugar.Infow("circuit breaker reset via API command", "service", cmd.ServiceName)
		bus.PublishCircuitState(ctx, events.CircuitStateEvent{
			ServiceName: cmd.ServiceName,
			Snapshot:    cbRegistry.Get(cmd.ServiceName).Snapshot(),
			Timestamp:   time.Now(),
		})
	})

	// Subscribe to API-initiated manual heal commands
	go bus.Subscribe(ctx, streams.HealCommands, "$", func(_ string, payload []byte) {
		var cmd enginepkg.HealCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			sugar.Warnw("invalid heal command payload", "error", err)
			return
		}
		svc, ok := poller.Lookup(cmd.ServiceName)
		if !ok {
			sugar.Warnw("manual heal command for unknown service", "service", cmd.ServiceName)
			return
		}
		sugar.Infow("manual heal command received from API", "service", cmd.ServiceName)
		go func() {
			result := healer.Heal(ctx, svc)
			sugar.Infow("manual healing attempt completed",
				"service", svc.Name,
				"action", result.Action,
				"success", result.Success,
				"error", result.Error,
			)
			bus.PublishHealingEvent(ctx, events.HealingEvent{
				ServiceName: svc.Name,
				Action:      result.Action,
				Success:     result.Success,
				Error:       result.ErrorString(),
				Timestamp:   time.Now(),
			})
		}()
	})

	// Subscribe to API-initiated service config mutations.
	go bus.Subscribe(ctx, streams.Config, "$", func(_ string, payload []byte) {
		var cmd enginepkg.ServiceConfigCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			sugar.Warnw("invalid service config command payload", "error", err)
			return
		}

		switch cmd.Action {
		case "upsert":
			poller.UpsertService(ctx, cmd.Service)
			sugar.Infow("service config applied", "action", cmd.Action, "service", cmd.Service.Name)
		case "delete":
			name := cmd.ServiceName
			if name == "" {
				name = cmd.Service.Name
			}
			if name == "" {
				sugar.Warnw("service config delete ignored: missing service name")
				return
			}
			poller.RemoveService(name)
			sugar.Infow("service config applied", "action", cmd.Action, "service", name)
		default:
			sugar.Warnw("unknown service config command action", "action", cmd.Action)
		}
	})

	// ── Graceful shutdown ──────────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	sugar.Infow("received signal, shutting down", "signal", sig)

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	poller.Stop(shutdownCtx)

	sugar.Info("Infrawatch engine stopped cleanly")
}
