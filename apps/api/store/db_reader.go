// Package store — db_reader.go
// Provides a read-only TimescaleDB client for the API.
// The engine owns all writes; the API only reads.
// Exception: WriteConfigAudit is a narrow write used to record config
// mutations against the config_audit table, which the API owns.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

// ── Result types ───────────────────────────────────────────────────────────

// DBMetricPoint is one row from the metrics hypertable.
type DBMetricPoint struct {
	Time       time.Time `json:"timestamp"`
	StatusCode int       `json:"status_code"`
	LatencyMs  float64   `json:"latency_ms"`
	IsHealthy  bool      `json:"is_healthy"`
	Error      string    `json:"error,omitempty"`
}

// DBIncident is one row from the incidents table, with optional timeline.
type DBIncident struct {
	ID         string          `json:"id"`
	Service    string          `json:"service_name"`
	StartedAt  time.Time       `json:"started_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	DurationMs *int64          `json:"duration_ms,omitempty"`
	Summary    string          `json:"summary"`
	RootState  string          `json:"root_state"`
	Open       bool            `json:"open"`
	Timeline   []DBStateChange `json:"timeline,omitempty"`
}

// DBStateChange is one row from the state_changes table used to build timelines.
type DBStateChange struct {
	Time      time.Time `json:"timestamp"`
	FromState string    `json:"from"`
	ToState   string    `json:"to"`
	Reason    string    `json:"reason"`
}

// DBAlertHistory is one row from alert_history.
type DBAlertHistory struct {
	ID             int64      `json:"id"`
	CreatedAt      time.Time  `json:"created_at"`
	Service        string     `json:"service_name"`
	State          string     `json:"state"`
	PreviousState  string     `json:"previous_state"`
	Message        string     `json:"message"`
	ResponseTimeMs int64      `json:"response_time_ms"`
	Channel        string     `json:"channel"`
	Delivered      bool       `json:"delivered"`
	Error          string     `json:"error,omitempty"`
	Acknowledged   bool       `json:"acknowledged"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
}

// LastServiceState carries the most recent state_change row per service,
// used to seed the in-memory StateStore on API restart.
type LastServiceState struct {
	Service   string
	ToState   string
	FromState string
	Time      time.Time
	Reason    string
}

// ── DBReader ───────────────────────────────────────────────────────────────

// DBReader holds a read-only connection pool to TimescaleDB.
type DBReader struct {
	pool   *sql.DB
	logger *zap.Logger
}

// NewDBReader opens and validates a connection pool to TimescaleDB.
// Returns nil, nil when dsn is empty (DB not configured — API degrades gracefully).
func NewDBReader(dsn string, maxOpen, maxIdle int, logger *zap.Logger) (*DBReader, error) {
	if dsn == "" {
		logger.Warn("storage.dsn is empty — TimescaleDB persistence disabled; API will serve in-memory data only")
		return nil, nil
	}

	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening DB pool: %w", err)
	}

	pool.SetMaxOpenConns(maxOpen)
	pool.SetMaxIdleConns(maxIdle)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("pinging TimescaleDB: %w", err)
	}

	logger.Sugar().Infow("TimescaleDB reader connected", "dsn_prefix", safeDSNPrefix(dsn))
	return &DBReader{pool: pool, logger: logger}, nil
}

// Close releases all DB connections.
func (r *DBReader) Close() error {
	return r.pool.Close()
}

// ── Restart recovery ───────────────────────────────────────────────────────

// GetLastStatesPerService returns the most recent state_change row for every
// known service. Used to pre-populate the in-memory StateStore on API startup.
func (r *DBReader) GetLastStatesPerService(ctx context.Context) ([]LastServiceState, error) {
	const q = `
		SELECT DISTINCT ON (service)
			service,
			from_state,
			to_state,
			time,
			COALESCE(reason, '')
		FROM state_changes
		ORDER BY service, time DESC
	`
	rows, err := r.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("querying last states: %w", err)
	}
	defer rows.Close()

	var out []LastServiceState
	for rows.Next() {
		var s LastServiceState
		if err := rows.Scan(&s.Service, &s.FromState, &s.ToState, &s.Time, &s.Reason); err != nil {
			return nil, fmt.Errorf("scanning last state row: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── Metrics queries ────────────────────────────────────────────────────────

// QueryMetrics returns all metric rows for service since the given cutoff time,
// ordered chronologically. Used by the metrics REST endpoint.
func (r *DBReader) QueryMetrics(ctx context.Context, service string, since time.Time) ([]DBMetricPoint, error) {
	const q = `
		SELECT
			time,
			COALESCE(status_code, 0),
			COALESCE(latency_ms, 0),
			COALESCE(is_healthy, false),
			COALESCE(error, '')
		FROM metrics
		WHERE service = $1
		  AND time > $2
		ORDER BY time ASC
	`
	rows, err := r.pool.QueryContext(ctx, q, service, since)
	if err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	defer rows.Close()

	var out []DBMetricPoint
	for rows.Next() {
		var p DBMetricPoint
		if err := rows.Scan(&p.Time, &p.StatusCode, &p.LatencyMs, &p.IsHealthy, &p.Error); err != nil {
			return nil, fmt.Errorf("scanning metric row: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── Incident queries ───────────────────────────────────────────────────────

// QueryIncidents returns incidents from the DB, optionally filtered by service.
// Results are ordered: open incidents first, then most-recent-first.
// limit defaults to 50 (max 500); offset enables cursor-style pagination.
func (r *DBReader) QueryIncidents(ctx context.Context, service string, limit, offset int) ([]DBIncident, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	const q = `
		SELECT
			id::text,
			service,
			started_at,
			resolved_at,
			duration_ms,
			COALESCE(summary, ''),
			COALESCE(root_state, '')
		FROM incidents
		WHERE ($1 = '' OR service = $1)
		ORDER BY
			(resolved_at IS NULL) DESC,
			started_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.QueryContext(ctx, q, service, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("querying incidents: %w", err)
	}
	defer rows.Close()

	var out []DBIncident
	for rows.Next() {
		var inc DBIncident
		if err := rows.Scan(
			&inc.ID,
			&inc.Service,
			&inc.StartedAt,
			&inc.ResolvedAt,
			&inc.DurationMs,
			&inc.Summary,
			&inc.RootState,
		); err != nil {
			return nil, fmt.Errorf("scanning incident row: %w", err)
		}
		inc.Open = inc.ResolvedAt == nil
		out = append(out, inc)
	}
	return out, rows.Err()
}

// GetIncidentByID returns a single incident with its full state-change timeline.
// Returns nil, nil when no incident with that ID exists.
func (r *DBReader) GetIncidentByID(ctx context.Context, id string) (*DBIncident, error) {
	const q = `
		SELECT
			id::text,
			service,
			started_at,
			resolved_at,
			duration_ms,
			COALESCE(summary, ''),
			COALESCE(root_state, '')
		FROM incidents
		WHERE id = $1::uuid
	`
	var inc DBIncident
	err := r.pool.QueryRowContext(ctx, q, id).Scan(
		&inc.ID,
		&inc.Service,
		&inc.StartedAt,
		&inc.ResolvedAt,
		&inc.DurationMs,
		&inc.Summary,
		&inc.RootState,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching incident %s: %w", id, err)
	}
	inc.Open = inc.ResolvedAt == nil

	// Attach the state-change timeline for this incident window
	timeline, err := r.queryTimeline(ctx, inc.Service, inc.StartedAt, inc.ResolvedAt)
	if err != nil {
		// Non-fatal: return the incident without timeline rather than 500
		r.logger.Sugar().Warnw("failed to load incident timeline", "id", id, "error", err)
	} else {
		inc.Timeline = timeline
	}

	return &inc, nil
}

// queryTimeline returns all state_change rows for a service within the given
// time window [from, to). to == nil means "up to now" (open incident).
func (r *DBReader) queryTimeline(ctx context.Context, service string, from time.Time, to *time.Time) ([]DBStateChange, error) {
	const q = `
		SELECT time, from_state, to_state, COALESCE(reason, '')
		FROM state_changes
		WHERE service = $1
		  AND time >= $2
		  AND ($3::timestamptz IS NULL OR time <= $3)
		ORDER BY time ASC
	`
	rows, err := r.pool.QueryContext(ctx, q, service, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying timeline: %w", err)
	}
	defer rows.Close()

	var out []DBStateChange
	for rows.Next() {
		var sc DBStateChange
		if err := rows.Scan(&sc.Time, &sc.FromState, &sc.ToState, &sc.Reason); err != nil {
			return nil, fmt.Errorf("scanning state change row: %w", err)
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// QueryAlertHistory returns recent alert delivery attempts with optional
// filtering by service/channel/ack status.
func (r *DBReader) QueryAlertHistory(ctx context.Context, service, channel string, onlyUnacked bool, limit int) ([]DBAlertHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	const q = `
		SELECT
			id,
			created_at,
			service,
			state,
			COALESCE(previous_state, ''),
			COALESCE(message, ''),
			COALESCE(response_time_ms, 0),
			channel,
			delivered,
			COALESCE(error, ''),
			acknowledged,
			acknowledged_at,
			COALESCE(acknowledged_by, '')
		FROM alert_history
		WHERE ($1 = '' OR service = $1)
		  AND ($2 = '' OR channel = $2)
		  AND ($3 = FALSE OR acknowledged = FALSE)
		ORDER BY created_at DESC
		LIMIT $4
	`
	rows, err := r.pool.QueryContext(ctx, q, service, channel, onlyUnacked, limit)
	if err != nil {
		return nil, fmt.Errorf("querying alert history: %w", err)
	}
	defer rows.Close()

	var out []DBAlertHistory
	for rows.Next() {
		var row DBAlertHistory
		if err := rows.Scan(
			&row.ID,
			&row.CreatedAt,
			&row.Service,
			&row.State,
			&row.PreviousState,
			&row.Message,
			&row.ResponseTimeMs,
			&row.Channel,
			&row.Delivered,
			&row.Error,
			&row.Acknowledged,
			&row.AcknowledgedAt,
			&row.AcknowledgedBy,
		); err != nil {
			return nil, fmt.Errorf("scanning alert history row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AcknowledgeAlert marks an alert_history row acknowledged by user.
func (r *DBReader) AcknowledgeAlert(ctx context.Context, id int64, by string) (bool, error) {
	const q = `
		UPDATE alert_history
		SET acknowledged = TRUE,
		    acknowledged_at = NOW(),
		    acknowledged_by = $2
		WHERE id = $1
	`
	res, err := r.pool.ExecContext(ctx, q, id, by)
	if err != nil {
		return false, fmt.Errorf("acknowledging alert %d: %w", id, err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// QueryUptimeByService returns uptime percentage per service for the given
// lookback window.
func (r *DBReader) QueryUptimeByService(ctx context.Context, since time.Time) (map[string]float64, error) {
	const q = `
		SELECT
			service,
			AVG(CASE WHEN is_healthy THEN 1.0 ELSE 0.0 END) * 100.0 AS uptime_pct
		FROM metrics
		WHERE time > $1
		GROUP BY service
	`
	rows, err := r.pool.QueryContext(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("querying uptime by service: %w", err)
	}
	defer rows.Close()

	out := make(map[string]float64)
	for rows.Next() {
		var service string
		var uptime float64
		if err := rows.Scan(&service, &uptime); err != nil {
			return nil, fmt.Errorf("scanning uptime row: %w", err)
		}
		out[service] = uptime
	}
	return out, rows.Err()
}

// ── Config audit ───────────────────────────────────────────────────────────

// ConfigAuditEntry describes one config mutation for the audit log.
type ConfigAuditEntry struct {
	Action  string      `json:"action"` // create | update | delete
	Service string      `json:"service"`
	UserID  string      `json:"user_id,omitempty"` // JWT subject claim, empty in no-auth mode
	Details interface{} `json:"details,omitempty"` // full service config snapshot (create/update) or nil (delete)
}

// WriteConfigAudit asynchronously persists a config mutation to the
// config_audit table.  Errors are logged and non-fatal — audit failures must
// never block the mutation response.
func (r *DBReader) WriteConfigAudit(entry ConfigAuditEntry) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		details, _ := json.Marshal(entry.Details)

		const q = `
			INSERT INTO config_audit (action, service, user_id, details)
			VALUES ($1, $2, $3, $4)`

		if _, err := r.pool.ExecContext(ctx, q,
			entry.Action, entry.Service, entry.UserID, details,
		); err != nil {
			r.logger.Sugar().Errorw("failed to write config audit",
				"action", entry.Action,
				"service", entry.Service,
				"error", err,
			)
		}
	}()
}

// ── helpers ────────────────────────────────────────────────────────────────

// safeDSNPrefix returns the non-sensitive prefix of a DSN for logging.
func safeDSNPrefix(dsn string) string {
	if len(dsn) > 30 {
		return dsn[:30] + "..."
	}
	return dsn
}
