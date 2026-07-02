package healing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// fallbackRegistry stores the currently active fallback URL per service.
// Exposed through the API so the dashboard can show reroute state.
var (
	fallbackMu      sync.RWMutex
	activeFallbacks = make(map[string]string) // service name → fallback URL
)

// GetActiveFallback returns the active fallback URL for a service, if any.
func GetActiveFallback(serviceName string) (string, bool) {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	url, ok := activeFallbacks[serviceName]
	return url, ok
}

// ClearFallback removes the fallback entry for a service (called when recovered).
func ClearFallback(serviceName string) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	delete(activeFallbacks, serviceName)
}

// AllFallbacks returns a copy of all active fallback mappings.
func AllFallbacks() map[string]string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	out := make(map[string]string, len(activeFallbacks))
	for k, v := range activeFallbacks {
		out[k] = v
	}
	return out
}

// healingWebhookPayload is the JSON body sent to a service's healing_webhook.
type healingWebhookPayload struct {
	Event       string    `json:"event"`
	ServiceName string    `json:"service_name"`
	Action      string    `json:"action"`
	FallbackURL string    `json:"fallback_url,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// FallbackHealer handles two soft healing actions:
//  1. Reroute — records the fallback URL and optionally notifies via webhook
//  2. Webhook  — fires the healing_webhook URL directly with a JSON payload
type FallbackHealer struct {
	client *http.Client
	logger *zap.Logger
}

// NewFallbackHealer creates a FallbackHealer.
func NewFallbackHealer(logger *zap.Logger) *FallbackHealer {
	return &FallbackHealer{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// Reroute records the service's FallbackURL in the in-memory registry and
// optionally fires the healing webhook to notify downstream systems.
func (f *FallbackHealer) Reroute(ctx context.Context, svc config.ServiceConfig) HealResult {
	if svc.FallbackURL == "" {
		return HealResult{
			Action:  "fallback",
			Success: false,
			Error:   fmt.Errorf("fallback: fallback_url is not set for service %s", svc.Name),
		}
	}

	// Register the active fallback
	fallbackMu.Lock()
	activeFallbacks[svc.Name] = svc.FallbackURL
	fallbackMu.Unlock()

	f.logger.Sugar().Infow("traffic rerouted to fallback",
		"service", svc.Name,
		"fallback_url", svc.FallbackURL,
	)

	// If the service also has a healing webhook, notify it
	if svc.HealingWebhook != "" {
		payload := healingWebhookPayload{
			Event:       "infrawatch.fallback_activated",
			ServiceName: svc.Name,
			Action:      "fallback",
			FallbackURL: svc.FallbackURL,
			Timestamp:   time.Now(),
		}
		if err := f.postWebhook(ctx, svc.HealingWebhook, payload); err != nil {
			f.logger.Sugar().Warnw("fallback webhook notification failed",
				"service", svc.Name,
				"error", err,
			)
			// Non-fatal — the fallback registration itself succeeded
		}
	}

	return HealResult{
		Action:    "fallback",
		Success:   true,
		Timestamp: time.Now(),
	}
}

// Webhook POSTs a healing notification to the service's HealingWebhook URL.
func (f *FallbackHealer) Webhook(ctx context.Context, svc config.ServiceConfig) HealResult {
	if svc.HealingWebhook == "" {
		return HealResult{
			Action:  "webhook",
			Success: false,
			Error:   fmt.Errorf("webhook: healing_webhook is not set for service %s", svc.Name),
		}
	}

	payload := healingWebhookPayload{
		Event:       "infrawatch.healing_webhook",
		ServiceName: svc.Name,
		Action:      "webhook",
		Timestamp:   time.Now(),
	}

	if err := f.postWebhook(ctx, svc.HealingWebhook, payload); err != nil {
		return HealResult{
			Action:  "webhook",
			Success: false,
			Error:   fmt.Errorf("webhook: POST failed: %w", err),
		}
	}

	f.logger.Sugar().Infow("healing webhook fired",
		"service", svc.Name,
		"url", svc.HealingWebhook,
	)

	return HealResult{
		Action:    "webhook",
		Success:   true,
		Timestamp: time.Now(),
	}
}

func (f *FallbackHealer) postWebhook(ctx context.Context, url string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "infrawatch-healer/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
