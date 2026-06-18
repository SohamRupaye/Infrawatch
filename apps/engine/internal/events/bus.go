package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/streams"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// MetricEvent is published on every health check poll.
type MetricEvent struct {
	ServiceName    string    `json:"service_name"`
	Timestamp      time.Time `json:"timestamp"`
	OK             bool      `json:"ok"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	StatusCode     int       `json:"status_code"`
	State          string    `json:"state"`
}

// StateChangeEvent is published whenever a service changes state.
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

// Bus wraps the Redis client and provides typed publish methods.
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
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Bus{rdb: rdb, logger: logger}, nil
}

// Close shuts down the Redis client.
func (b *Bus) Close() error {
	return b.rdb.Close()
}

// PublishMetric publishes a metric event to the metrics stream.
func (b *Bus) PublishMetric(ctx context.Context, evt MetricEvent) {
	b.publish(ctx, streams.Metrics, evt)
}

// PublishStateChange publishes a state transition event.
func (b *Bus) PublishStateChange(ctx context.Context, evt StateChangeEvent) {
	b.publish(ctx, streams.StateChange, evt)
}

// PublishHealingEvent publishes a healing outcome event.
func (b *Bus) PublishHealingEvent(ctx context.Context, evt HealingEvent) {
	b.publish(ctx, streams.Healing, evt)
}

// PublishAnomaly publishes an anomaly detection event.
func (b *Bus) PublishAnomaly(ctx context.Context, evt AnomalyEvent) {
	b.publish(ctx, streams.Anomalies, evt)
}

// publish serialises evt to JSON and adds it to the named Redis Stream.
func (b *Bus) publish(ctx context.Context, stream string, evt interface{}) {
	data, err := json.Marshal(evt)
	if err != nil {
		b.logger.Sugar().Errorw("failed to marshal event", "stream", stream, "error", err)
		return
	}

	args := &redis.XAddArgs{
		Stream: stream,
		MaxLen: streams.MaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(data)},
	}

	if err := b.rdb.XAdd(ctx, args).Err(); err != nil {
		b.logger.Sugar().Errorw("failed to publish to stream", "stream", stream, "error", err)
	}
}

// Subscribe reads events from a stream starting from the given ID (or "$" for
// new-only). It blocks until the context is cancelled, calling handler for
// each message.
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

// Client exposes the raw Redis client for advanced queries (metrics API).
func (b *Bus) Client() *redis.Client {
	return b.rdb
}
