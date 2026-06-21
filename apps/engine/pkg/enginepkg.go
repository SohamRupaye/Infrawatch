// Package enginepkg exports types and the Redis client wrapper that the API
// binary needs to read from the engine's event streams.
// The engine's internal packages are off-limits to other apps; this package
// is the deliberately narrow public surface.
package enginepkg

import (
	"context"
	"encoding/json"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/SohamRupaye/infrawatch/apps/engine/streams"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ── Stream name constants ──────────────────────────────────────────────────
// These are re-exported from apps/engine/streams so that API code can use
// enginepkg.Stream* without importing the streams package directly, while
// the engine's internal bus uses the streams package as the single source
// of truth.

const (
	StreamMetrics     = streams.Metrics
	StreamStateChange = streams.StateChange
	StreamHealing     = streams.Healing
	StreamAnomalies   = streams.Anomalies
	StreamConfig      = streams.Config
)

// ── Event types ────────────────────────────────────────────────────────────

// MetricEvent is published on every health check poll.
type MetricEvent struct {
	ServiceName    string    `json:"service_name"`
	Timestamp      time.Time `json:"timestamp"`
	OK             bool      `json:"ok"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	StatusCode     int       `json:"status_code"`
	State          string    `json:"state"`
}

// StateChangeEvent is published whenever a service changes health state.
type StateChangeEvent struct {
	ServiceName   string      `json:"service_name"`
	PreviousState string      `json:"previous_state"`
	NewState      string      `json:"new_state"`
	Reason        string      `json:"reason"`
	Timestamp     time.Time   `json:"timestamp"`
	Snapshot      interface{} `json:"snapshot"`
}

// HealingEvent records the outcome of a self-healing attempt.
type HealingEvent struct {
	ServiceName string    `json:"service_name"`
	Action      string    `json:"action"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// AnomalyEvent is published when an anomaly is detected.
type AnomalyEvent struct {
	ServiceName string    `json:"service_name"`
	Type        string    `json:"type"`
	Message     string    `json:"message"`
	Value       float64   `json:"value"`
	Baseline    float64   `json:"baseline"`
	Timestamp   time.Time `json:"timestamp"`
}

// ServiceConfigCommand is published by the API whenever service configuration
// is changed so the engine can apply updates without restart.
type ServiceConfigCommand struct {
	Action      string               `json:"action"` // upsert | delete
	ServiceName string               `json:"service_name"`
	Service     config.ServiceConfig `json:"service"`
	Timestamp   time.Time            `json:"timestamp"`
}

// ── Circuit breaker snapshot ───────────────────────────────────────────────

// BreakerState represents the three states of a circuit breaker.
type BreakerState string

const (
	BreakerStateClosed   BreakerState = "CLOSED"
	BreakerStateOpen     BreakerState = "OPEN"
	BreakerStateHalfOpen BreakerState = "HALF_OPEN"
)

// BreakerSnapshot is a read-only view of a circuit breaker.
type BreakerSnapshot struct {
	State            BreakerState `json:"state"`
	ConsecutiveFails int          `json:"consecutive_fails"`
	OpenedAt         time.Time    `json:"opened_at,omitempty"`
	LastTransition   time.Time    `json:"last_transition"`
}

// ── Service state snapshot ─────────────────────────────────────────────────

// Transition records a single state change event.
type Transition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// ServiceStateSnapshot is a read-only view of a service's health state.
type ServiceStateSnapshot struct {
	Name             string       `json:"name"`
	CurrentState     string       `json:"current_state"`
	PreviousState    string       `json:"previous_state"`
	LastChecked      time.Time    `json:"last_checked"`
	LastHealthy      time.Time    `json:"last_healthy"`
	ConsecutiveFails int          `json:"consecutive_fails"`
	ResponseTimeMs   int64        `json:"response_time_ms"`
	ErrorMessage     string       `json:"error_message,omitempty"`
	Transitions      []Transition `json:"transitions"`
	UptimePct        float64      `json:"uptime_pct"`
}

// ── Redis event bus (API-facing, read-only) ────────────────────────────────

// Bus provides read and write access to the Redis Streams event bus.
type Bus struct {
	rdb    *redis.Client
	logger *zap.Logger
}

// NewBus connects to Redis and returns a Bus.
func NewBus(ctx context.Context, addr, password string, logger *zap.Logger) (*Bus, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Bus{rdb: rdb, logger: logger}, nil
}

// Close shuts down the Redis client.
func (b *Bus) Close() error { return b.rdb.Close() }

// Client returns the raw Redis client for advanced queries (XRevRangeN etc.).
func (b *Bus) Client() *redis.Client { return b.rdb }

// Subscribe reads events from a stream starting from startID (use "$" for new-only).
// Calls handler for each message. Blocks until ctx is cancelled.
func (b *Bus) Subscribe(ctx context.Context, stream, startID string, handler func(id string, payload []byte)) {
	lastID := startID
	if lastID == "" {
		lastID = "$"
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		results, err := b.rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{stream, lastID},
			Count:   100,
			Block:   2 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil || err == context.Canceled || err == context.DeadlineExceeded {
				continue
			}
			b.logger.Sugar().Errorw("stream read error", "stream", stream, "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, result := range results {
			for _, msg := range result.Messages {
				if payload, ok := msg.Values["payload"].(string); ok {
					handler(msg.ID, []byte(payload))
				}
				lastID = msg.ID
			}
		}
	}
}

// PublishStateChange publishes a state transition event (used by circuit reset).
func (b *Bus) PublishStateChange(ctx context.Context, evt StateChangeEvent) {
	b.publish(ctx, StreamStateChange, evt)
}

// PublishHealCommand queues a manual heal command for the engine to pick up.
func (b *Bus) PublishHealCommand(ctx context.Context, serviceName string) error {
	return b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "infrawatch:heal_commands",
		MaxLen: 1000,
		Approx: true,
		Values: map[string]interface{}{
			"service_name": serviceName,
			"action":       "manual_heal",
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		},
	}).Err()
}

// PublishCircuitReset queues a circuit reset command for the engine.
func (b *Bus) PublishCircuitReset(ctx context.Context, serviceName string) error {
	return b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "infrawatch:circuit_resets",
		MaxLen: 1000,
		Approx: true,
		Values: map[string]interface{}{
			"service_name": serviceName,
			"action":       "circuit_reset",
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		},
	}).Err()
}

// PublishServiceConfigCommand queues a config update command for the engine.
func (b *Bus) PublishServiceConfigCommand(ctx context.Context, cmd ServiceConfigCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamConfig,
		MaxLen: 2000,
		Approx: true,
		Values: map[string]interface{}{"payload": string(data)},
	}).Err()
}

func (b *Bus) publish(ctx context.Context, stream string, evt interface{}) {
	data, err := json.Marshal(evt)
	if err != nil {
		b.logger.Sugar().Errorw("failed to marshal event", "stream", stream, "error", err)
		return
	}
	if err := b.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		MaxLen: streams.MaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(data)},
	}).Err(); err != nil {
		b.logger.Sugar().Errorw("failed to publish to stream", "stream", stream, "error", err)
	}
}
