package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/gin-gonic/gin"
)

// AlertmanagerWebhookHandler receives Alertmanager's webhook_config payload
// and translates alerts mapped to passive-mode services into external alert
// signals for the engine. Alertmanager can't do interactive JWT auth, so
// this route is authenticated with a separate shared secret instead.
type AlertmanagerWebhookHandler struct{ deps Deps }

func NewAlertmanagerWebhookHandler(deps Deps) *AlertmanagerWebhookHandler {
	return &AlertmanagerWebhookHandler{deps}
}

// alertmanagerAlert mirrors the subset of Alertmanager's webhook alert
// object this handler cares about.
type alertmanagerAlert struct {
	Status      string            `json:"status"` // "firing" or "resolved"
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type alertmanagerWebhookPayload struct {
	Alerts []alertmanagerAlert `json:"alerts"`
}

// serviceLabel is the Alertmanager label used to map an alert to an
// Infrawatch service name.
const serviceLabel = "infrawatch_service"

// Receive godoc
// POST /api/v1/webhooks/alertmanager
// Requires header X-Webhook-Secret to match api.alertmanager.webhook_secret.
func (h *AlertmanagerWebhookHandler) Receive(c *gin.Context) {
	secret := h.deps.Cfg.AlertmanagerWebhookSecret
	if secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "alertmanager webhook is disabled: set alertmanager.webhook_secret in the configuration to enable it",
		})
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Webhook-Secret")), []byte(secret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing X-Webhook-Secret header"})
		return
	}

	var payload alertmanagerWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	accepted := make([]string, 0, len(payload.Alerts))
	skipped := make([]gin.H, 0)

	for _, alert := range payload.Alerts {
		serviceName := alert.Labels[serviceLabel]
		if serviceName == "" {
			skipped = append(skipped, gin.H{"reason": "missing " + serviceLabel + " label"})
			continue
		}

		idx := serviceIndex(cfg.Services, serviceName)
		if idx < 0 {
			skipped = append(skipped, gin.H{"service": serviceName, "reason": "unknown service"})
			continue
		}
		if !cfg.Services[idx].IsPassive() {
			skipped = append(skipped, gin.H{"service": serviceName, "reason": "service is not mode=passive"})
			continue
		}

		reason := alert.Annotations["summary"]
		if reason == "" {
			reason = alert.Annotations["description"]
		}

		if err := h.deps.Bus.PublishExternalAlertSignal(ctx, enginepkg.ExternalAlertSignal{
			ServiceName: serviceName,
			Status:      alert.Status,
			Reason:      reason,
			Timestamp:   time.Now().UTC(),
		}); err != nil {
			skipped = append(skipped, gin.H{"service": serviceName, "reason": "failed to publish: " + err.Error()})
			continue
		}
		accepted = append(accepted, serviceName)
	}

	c.JSON(http.StatusAccepted, gin.H{"accepted": accepted, "skipped": skipped})
}
