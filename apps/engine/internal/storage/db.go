// Package storage provides the TimescaleDB write layer for the Infrawatch engine.
// The engine is the source of truth — every poll result and every state transition
// is written here. The API reads from the same DB for historical queries and restart
// recovery, but never writes through this package.
//
// Design decisions:
//   - WriteMetric is fully async / fire-and-forget so DB latency never blocks polling.
//   - WriteStateChange + HandleIncident run in the same goroutine to keep the
//     state_changes and incidents tables causally consistent.
//   - All schema operations are idempotent (IF NOT EXISTS / if_not_exists => TRUE)
//     so the engine can safely call SetupSchema on every startup.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/internal/alerts"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

// ─── row types ────────────────────────────────────────────────────────────────

// MetricRow represents one poll result that will be inserted into the metrics
// hypertable.
type MetricRow struct {
	Time       time.Time
	Service    string
	StatusCode int
	LatencyMs  float64
	IsHealthy  bool
	Error      string
}

// StateChangeRow represents one FSM transition that will be inserted into the
// state_changes table.
type StateChangeRow struct {
	Service   string
	FromState string
	ToState   string
	Reason    string
}

// ─── DB ───────────────────────────────────────────────────────────────────────

// DB wraps a *sql.DB connection pool and exposes the narrow write API that the
// engine needs.  All methods are safe for concurrent use.
type DB struct {
	pool   *sql.DB
	logger *zap.Logger
}

// New opens a connection pool to TimescaleDB (PostgreSQL-compatible) and verifies
// connectivity with a ping.  It does NOT run schema migrations; call SetupSchema
// separately after construction.
func New(dsn string, maxOpen, maxIdle int, logger *zap.Logger) (*DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("storage: DSN is empty — set storage.dsn in config or DATABASE_URL env var")
	}

	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: opening pool: %w", err)
	}

	pool.SetMaxOpenConns(maxOpen)
	pool.SetMaxIdleConns(maxIdle)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(10 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("storage: ping failed: %w", err)
	}

	return &DB{pool: pool, logger: logger}, nil
}

// Close releases all connections in the pool.
func (d *DB) Close() error {
	return d.pool.Close()
}

// ─── schema ───────────────────────────────────────────────────────────────────

// SetupSchema creates all required tables, hypertable, indexes, and retention
// policies.  Every statement is idempotent so it is safe to call on every startup.
func (d *DB) SetupSchema(ctx context.Context) error {
	steps := []struct {
		name string
		sql  string
	}{
		{
			"enable timescaledb extension",
			`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`,
		},
		{
			"create metrics table",
			`CREATE TABLE IF NOT EXISTS metrics (
				time        TIMESTAMPTZ NOT NULL,
				service     TEXT        NOT NULL,
				status_code INT,
				latency_ms  FLOAT,
				is_healthy  BOOLEAN,
				error       TEXT
			)`,
		},
		{
			// create_hypertable returns an error if already a hypertable unless
			// if_not_exists => TRUE is passed.
			"create metrics hypertable",
			`SELECT create_hypertable('metrics', 'time', if_not_exists => TRUE)`,
		},
		{
			"create metrics service+time index",
			`CREATE INDEX IF NOT EXISTS idx_metrics_service_time
			 ON metrics (service, time DESC)`,
		},
		{
			"create state_changes table",
			`CREATE TABLE IF NOT EXISTS state_changes (
				id         UUID        DEFAULT gen_random_uuid() PRIMARY KEY,
				time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				service    TEXT        NOT NULL,
				from_state TEXT        NOT NULL,
				to_state   TEXT        NOT NULL,
				reason     TEXT
			)`,
		},
		{
			"create state_changes service+time index",
			`CREATE INDEX IF NOT EXISTS idx_state_changes_service_time
			 ON state_changes (service, time DESC)`,
		},
		{
			"create incidents table",
			`CREATE TABLE IF NOT EXISTS incidents (
				id          UUID        DEFAULT gen_random_uuid() PRIMARY KEY,
				service     TEXT        NOT NULL,
				started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				resolved_at TIMESTAMPTZ,
				duration_ms BIGINT,
				summary     TEXT,
				root_state  TEXT
			)`,
		},
		{
			"create incidents service+started_at index",
			`CREATE INDEX IF NOT EXISTS idx_incidents_service_started
			 ON incidents (service, started_at DESC)`,
		},
		{
			"create alert_history table",
			`CREATE TABLE IF NOT EXISTS alert_history (
				id               BIGSERIAL PRIMARY KEY,
				created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				service          TEXT        NOT NULL,
				state            TEXT        NOT NULL,
				previous_state   TEXT,
				message          TEXT,
				response_time_ms BIGINT,
				channel          TEXT        NOT NULL,
				delivered        BOOLEAN     NOT NULL DEFAULT FALSE,
				error            TEXT,
				acknowledged     BOOLEAN     NOT NULL DEFAULT FALSE,
				acknowledged_at  TIMESTAMPTZ,
				acknowledged_by  TEXT
			)`,
		},
		{
			"create alert_history service+created index",
			`CREATE INDEX IF NOT EXISTS idx_alert_history_service_created
			 ON alert_history (service, created_at DESC)`,
		},
		{
			"create alert_history ack index",
			`CREATE INDEX IF NOT EXISTS idx_alert_history_ack
			 ON alert_history (acknowledged, created_at DESC)`,
		},
	}

	for _, step := range steps {
		if _, err := d.pool.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("storage: schema step %q failed: %w", step.name, err)
		}
		d.logger.Sugar().Debugw("schema step ok", "step", step.name)
	}

	// Retention policies — add_retention_policy is idempotent with if_not_exists.
	// Intentionally non-fatal: if TimescaleDB Community edition isn't available
	// the policy call will fail but the rest of the schema is fine.
	retentionSteps := []struct {
		name  string
		table string
		after string
	}{
		{"metrics retention 30d", "metrics", "30 days"},
		{"state_changes retention 90d", "state_changes", "90 days"},
	}
	for _, r := range retentionSteps {
		stmt := fmt.Sprintf(
			`SELECT add_retention_policy('%s', INTERVAL '%s', if_not_exists => TRUE)`,
			r.table, r.after,
		)
		if _, err := d.pool.ExecContext(ctx, stmt); err != nil {
			// Log and continue — retention policies are best-effort
			d.logger.Sugar().Warnw("retention policy skipped (non-fatal)",
				"step", r.name, "error", err)
		} else {
			d.logger.Sugar().Debugw("retention policy ok", "step", r.name)
		}
	}

	d.logger.Info("storage: schema ready")
	return nil
}

// WriteAlertHistory persists one alert delivery attempt asynchronously.
func (d *DB) WriteAlertHistory(a alerts.AlertDeliveryAttempt) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		const q = `
			INSERT INTO alert_history
				(created_at, service, state, previous_state, message, response_time_ms, channel, delivered, error)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8, $9)`

		if _, err := d.pool.ExecContext(ctx, q,
			a.Timestamp, a.ServiceName, a.State, a.PreviousState, a.Message, a.ResponseTimeMs, a.Channel, a.Delivered, a.Error,
		); err != nil {
			d.logger.Sugar().Errorw("storage: failed to write alert_history",
				"service", a.ServiceName,
				"channel", a.Channel,
				"error", err,
			)
		}
	}()
}

// ─── writes ───────────────────────────────────────────────────────────────────

// WriteMetric persists one poll result asynchronously.  The call returns
// immediately so DB latency never stalls the polling loop.  Errors are logged
// but not propagated.
func (d *DB) WriteMetric(m MetricRow) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		const q = `
			INSERT INTO metrics (time, service, status_code, latency_ms, is_healthy, error)
			VALUES ($1, $2, $3, $4, $5, $6)`

		if _, err := d.pool.ExecContext(ctx, q,
			m.Time, m.Service, m.StatusCode, m.LatencyMs, m.IsHealthy, m.Error,
		); err != nil {
			d.logger.Sugar().Errorw("storage: failed to write metric",
				"service", m.Service, "error", err)
		}
	}()
}

// WriteStateChange persists one FSM transition, then evaluates incident
// open/close logic.  Both operations run in the same goroutine so the
// incidents table stays causally consistent with state_changes.
//
// The call is asynchronous (fire-and-forget goroutine) so it does not
// block the polling loop.
func (d *DB) WriteStateChange(sc StateChangeRow) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := d.insertStateChange(ctx, sc); err != nil {
			d.logger.Sugar().Errorw("storage: failed to write state_change",
				"service", sc.Service, "error", err)
			// Still attempt incident logic — the state_change is best-effort
		}

		if err := d.handleIncident(ctx, sc.Service, sc.FromState, sc.ToState); err != nil {
			d.logger.Sugar().Errorw("storage: failed to handle incident",
				"service", sc.Service, "error", err)
		}
	}()
}

// insertStateChange does the raw INSERT.
func (d *DB) insertStateChange(ctx context.Context, sc StateChangeRow) error {
	const q = `
		INSERT INTO state_changes (service, from_state, to_state, reason)
		VALUES ($1, $2, $3, $4)`
	_, err := d.pool.ExecContext(ctx, q, sc.Service, sc.FromState, sc.ToState, sc.Reason)
	return err
}

// ─── incident logic ───────────────────────────────────────────────────────────

// handleIncident decides whether to open or close an incident for service based
// on the from→to state transition.
//
// Incident opens:  service transitions TO DEAD or UNHEALTHY (and no open
//
//	incident already exists for this service).
//
// Incident closes: service transitions TO HEALTHY or RECOVERING.
func (d *DB) handleIncident(ctx context.Context, service, fromState, toState string) error {
	switch toState {
	case "DEAD", "UNHEALTHY":
		// Only open a new incident if there is no currently open one.
		// Transitions like UNHEALTHY→DEAD should not open a second record.
		open, err := d.hasOpenIncident(ctx, service)
		if err != nil {
			return fmt.Errorf("checking open incident: %w", err)
		}
		if open {
			return nil // already tracking this outage
		}
		return d.openIncident(ctx, service, toState)

	case "HEALTHY", "RECOVERING":
		return d.closeIncident(ctx, service)
	}
	return nil
}

// hasOpenIncident returns true if the service already has an unresolved incident.
func (d *DB) hasOpenIncident(ctx context.Context, service string) (bool, error) {
	const q = `
		SELECT COUNT(*)
		FROM incidents
		WHERE service = $1 AND resolved_at IS NULL`
	var n int
	if err := d.pool.QueryRowContext(ctx, q, service).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// openIncident inserts a new, unresolved incident row.
func (d *DB) openIncident(ctx context.Context, service, rootState string) error {
	const q = `
		INSERT INTO incidents (service, started_at, root_state)
		VALUES ($1, NOW(), $2)`
	_, err := d.pool.ExecContext(ctx, q, service, rootState)
	if err != nil {
		return fmt.Errorf("opening incident for %s: %w", service, err)
	}
	d.logger.Sugar().Infow("storage: incident opened",
		"service", service, "root_state", rootState)
	return nil
}

// closeIncident resolves the open incident for service (if one exists), setting
// resolved_at, duration_ms, and an auto-generated summary.
func (d *DB) closeIncident(ctx context.Context, service string) error {
	const q = `
		UPDATE incidents
		SET
			resolved_at = NOW(),
			duration_ms = EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000,
			summary     = service || ' was ' || root_state ||
			              ' for ' || EXTRACT(EPOCH FROM (NOW() - started_at))::INT ||
			              's. Recovered at ' || TO_CHAR(NOW(), 'HH24:MI:SS UTC') || '.'
		WHERE service = $1 AND resolved_at IS NULL`

	res, err := d.pool.ExecContext(ctx, q, service)
	if err != nil {
		return fmt.Errorf("closing incident for %s: %w", service, err)
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		d.logger.Sugar().Infow("storage: incident closed", "service", service)
	}
	return nil
}
