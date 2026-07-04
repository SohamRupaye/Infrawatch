package alerts

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// Alert is the canonical alert payload passed to all channels.
type Alert struct {
	ServiceName    string
	State          string
	PreviousState  string
	Message        string
	ResponseTimeMs int64
	Timestamp      time.Time
	// DashboardURL is injected by the dispatcher if configured
	DashboardURL string
}

// Channel is the interface every alert backend must implement.
type Channel interface {
	Name() string
	Send(ctx context.Context, alert Alert) error
}

// AlertHistoryWriter persists every alert delivery attempt for audit/history.
type AlertHistoryWriter interface {
	WriteAlertHistory(AlertDeliveryAttempt)
}

// AlertDeliveryAttempt captures a single channel send result.
type AlertDeliveryAttempt struct {
	ServiceName    string
	State          string
	PreviousState  string
	Message        string
	ResponseTimeMs int64
	Channel        string
	Delivered      bool
	Error          string
	Timestamp      time.Time
}

// Dispatcher routes an Alert to the appropriate channels based on
// the state rules defined in config.
type Dispatcher struct {
	channels   map[string]Channel
	stateRules map[string][]string
	history    AlertHistoryWriter
	escalate   bool
	logger     *zap.Logger
	mu         sync.Mutex
	lifecycle  map[string]serviceAlertLifecycle
}

type serviceAlertLifecycle struct {
	InBadState     bool
	BadState       string
	ActiveChannels []string
}

// NewDispatcher creates a Dispatcher and initialises only the enabled channels.
func NewDispatcher(cfg config.AlertsConfig, history AlertHistoryWriter, logger *zap.Logger) (*Dispatcher, error) {
	channels := make(map[string]Channel)

	if cfg.Slack.Enabled {
		channels["slack"] = NewSlackChannel(cfg.Slack)
	}
	if cfg.PagerDuty.Enabled {
		channels["pagerduty"] = NewPagerDutyChannel(cfg.PagerDuty)
	}
	if cfg.Email.Enabled {
		ch, err := NewEmailChannel(cfg.Email)
		if err != nil {
			return nil, err
		}
		channels["email"] = ch
	}
	if cfg.Webhook.Enabled {
		channels["webhook"] = NewWebhookChannel(cfg.Webhook)
	}

	return &Dispatcher{
		channels:   channels,
		stateRules: cfg.StateRules,
		history:    history,
		escalate:   cfg.EscalateOnWorsening,
		logger:     logger,
		lifecycle:  make(map[string]serviceAlertLifecycle),
	}, nil
}

// Dispatch fires the alert to every channel mapped to alert.State.
// Each channel is called in its own goroutine; failures are logged but don't
// block or affect other channels.
func (d *Dispatcher) Dispatch(ctx context.Context, alert Alert) {
	channelNames, ok := d.selectChannelsForAlert(alert)
	if !ok || len(channelNames) == 0 {
		return
	}

	for _, name := range channelNames {
		ch, ok := d.channels[name]
		if !ok {
			// Channel not enabled — silently skip
			continue
		}

		go func(ch Channel, alert Alert) {
			err := ch.Send(ctx, alert)
			if err != nil {
				d.logger.Sugar().Errorw("alert dispatch failed",
					"channel", ch.Name(),
					"service", alert.ServiceName,
					"state", alert.State,
					"error", err,
				)
			} else {
				d.logger.Sugar().Debugw("alert dispatched",
					"channel", ch.Name(),
					"service", alert.ServiceName,
					"state", alert.State,
				)
			}
			d.recordAttempt(AlertDeliveryAttempt{
				ServiceName:    alert.ServiceName,
				State:          alert.State,
				PreviousState:  alert.PreviousState,
				Message:        alert.Message,
				ResponseTimeMs: alert.ResponseTimeMs,
				Channel:        ch.Name(),
				Delivered:      err == nil,
				Error:          errString(err),
				Timestamp:      alert.Timestamp,
			})
		}(ch, alert)
	}
}

func (d *Dispatcher) selectChannelsForAlert(alert Alert) ([]string, bool) {
	bad := isBadState(alert.State)
	recovered := isRecoveredState(alert.State)

	d.mu.Lock()
	defer d.mu.Unlock()

	lc := d.lifecycle[alert.ServiceName]

	switch {
	case bad && !lc.InBadState:
		lc.InBadState = true
		lc.BadState = alert.State
		lc.ActiveChannels = uniqueChannels(d.stateRules[alert.State])
		d.lifecycle[alert.ServiceName] = lc
		return lc.ActiveChannels, true

	case bad && lc.InBadState:
		if !d.escalate || !isWorsening(lc.BadState, alert.State) {
			return nil, false
		}
		lc.BadState = alert.State
		lc.ActiveChannels = uniqueChannels(append(lc.ActiveChannels, d.stateRules[alert.State]...))
		d.lifecycle[alert.ServiceName] = lc
		return lc.ActiveChannels, true

	case recovered && lc.InBadState:
		channels := uniqueChannels(lc.ActiveChannels)
		delete(d.lifecycle, alert.ServiceName)
		return channels, true

	case recovered && !lc.InBadState:
		return nil, false

	default:
		channelNames, ok := d.stateRules[alert.State]
		return uniqueChannels(channelNames), ok
	}
}

func (d *Dispatcher) recordAttempt(attempt AlertDeliveryAttempt) {
	if d.history == nil {
		return
	}
	d.history.WriteAlertHistory(attempt)
}

func uniqueChannels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func isBadState(state string) bool {
	switch state {
	case "DEGRADED", "UNHEALTHY", "DEAD":
		return true
	default:
		return false
	}
}

func isRecoveredState(state string) bool {
	switch state {
	case "RECOVERING", "HEALTHY":
		return true
	default:
		return false
	}
}

func severity(state string) int {
	switch state {
	case "DEGRADED":
		return 1
	case "UNHEALTHY":
		return 2
	case "DEAD":
		return 3
	default:
		return 0
	}
}

func isWorsening(prevBad, nextBad string) bool {
	return severity(nextBad) > severity(prevBad)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
