package events

import (
	"context"
	"time"
)

// Publisher provides a typed, high-level API for emitting events.
// It wraps Bus and adds convenience constructors for each event type.
type Publisher struct {
	bus *Bus
}

// NewPublisher wraps a Bus.
func NewPublisher(bus *Bus) *Publisher {
	return &Publisher{bus: bus}
}

// ServiceHealthy emits a HEALTHY state-change event.
func (p *Publisher) ServiceHealthy(ctx context.Context, serviceName, prevState string) {
	p.bus.PublishStateChange(ctx, StateChangeEvent{
		ServiceName:   serviceName,
		PreviousState: prevState,
		NewState:      "HEALTHY",
		Reason:        "consecutive successful health checks",
		Timestamp:     time.Now(),
	})
}

// ServiceDead emits a DEAD state-change event with the triggering error.
func (p *Publisher) ServiceDead(ctx context.Context, serviceName, prevState, reason string) {
	p.bus.PublishStateChange(ctx, StateChangeEvent{
		ServiceName:   serviceName,
		PreviousState: prevState,
		NewState:      "DEAD",
		Reason:        reason,
		Timestamp:     time.Now(),
	})
}

// HealingAttempt emits the result of a healing action.
func (p *Publisher) HealingAttempt(ctx context.Context, serviceName, action string, success bool, errMsg string) {
	p.bus.PublishHealingEvent(ctx, HealingEvent{
		ServiceName: serviceName,
		Action:      action,
		Success:     success,
		Error:       errMsg,
		Timestamp:   time.Now(),
	})
}

// Anomaly emits an anomaly detection event.
func (p *Publisher) Anomaly(ctx context.Context, serviceName, anomalyType, message string, value, baseline float64) {
	p.bus.PublishAnomaly(ctx, AnomalyEvent{
		ServiceName: serviceName,
		Type:        anomalyType,
		Message:     message,
		Value:       value,
		Baseline:    baseline,
		Timestamp:   time.Now(),
	})
}
