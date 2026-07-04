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

// webhookPayload is the JSON body sent to the generic webhook.
type webhookPayload struct {
	Event          string    `json:"event"`
	ServiceName    string    `json:"service_name"`
	State          string    `json:"state"`
	PreviousState  string    `json:"previous_state"`
	Message        string    `json:"message"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	Timestamp      time.Time `json:"timestamp"`
	DashboardURL   string    `json:"dashboard_url,omitempty"`
}

// WebhookChannel sends a JSON POST to a configurable URL.
type WebhookChannel struct {
	cfg    config.WebhookConfig
	client *http.Client
}

// NewWebhookChannel creates a WebhookChannel.
func NewWebhookChannel(cfg config.WebhookConfig) *WebhookChannel {
	return &WebhookChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Channel.
func (w *WebhookChannel) Name() string { return "webhook" }

// Send implements Channel.
func (w *WebhookChannel) Send(ctx context.Context, alert Alert) error {
	payload := webhookPayload{
		Event:          "infrawatch.state_change",
		ServiceName:    alert.ServiceName,
		State:          alert.State,
		PreviousState:  alert.PreviousState,
		Message:        alert.Message,
		ResponseTimeMs: alert.ResponseTimeMs,
		Timestamp:      alert.Timestamp,
		DashboardURL:   alert.DashboardURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "infrawatch-webhook/1.0")

	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
