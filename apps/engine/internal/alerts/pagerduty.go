package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

const pagerDutyEventsV2URL = "https://events.pagerduty.com/v2/enqueue"

// pdPayload is the PagerDuty Events API v2 payload.
type pdPayload struct {
	RoutingKey  string    `json:"routing_key"`
	EventAction string    `json:"event_action"` // trigger | resolve | acknowledge
	DedupKey    string    `json:"dedup_key"`
	Payload     pdDetails `json:"payload"`
}

type pdDetails struct {
	Summary  string `json:"summary"`
	Source   string `json:"source"`
	Severity string `json:"severity"` // critical | error | warning | info
	Class    string `json:"class"`
	Group    string `json:"group"`
}

// PagerDutyChannel sends alerts to PagerDuty via Events API v2.
type PagerDutyChannel struct {
	cfg    config.PagerDutyConfig
	client *http.Client
}

// NewPagerDutyChannel creates a PagerDutyChannel.
func NewPagerDutyChannel(cfg config.PagerDutyConfig) *PagerDutyChannel {
	return &PagerDutyChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Channel.
func (pd *PagerDutyChannel) Name() string { return "pagerduty" }

// Send implements Channel.
func (pd *PagerDutyChannel) Send(ctx context.Context, alert Alert) error {
	action := "trigger"
	if alert.State == "HEALTHY" || alert.State == "RECOVERING" {
		action = "resolve"
	}

	payload := pdPayload{
		RoutingKey:  pd.cfg.IntegrationKey,
		EventAction: action,
		DedupKey:    fmt.Sprintf("infrawatch-%s", alert.ServiceName),
		Payload: pdDetails{
			Summary:  fmt.Sprintf("[%s] %s — %s", alert.State, alert.ServiceName, alert.Message),
			Source:   "infrawatch",
			Severity: pdSeverity(alert.State),
			Class:    "health",
			Group:    alert.ServiceName,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal pagerduty payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pagerDutyEventsV2URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := pd.client.Do(req)
	if err != nil {
		return fmt.Errorf("send pagerduty: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned status %d", resp.StatusCode)
	}
	return nil
}

func pdSeverity(state string) string {
	switch state {
	case "DEAD":
		return "critical"
	case "UNHEALTHY":
		return "error"
	case "DEGRADED":
		return "warning"
	default:
		return "info"
	}
}
