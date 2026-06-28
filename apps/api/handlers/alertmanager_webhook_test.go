package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newWebhookTestRouter(t *testing.T, secret, configYAML string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	configPath := filepath.Join(t.TempDir(), "infrawatch.yaml")
	if configYAML != "" {
		if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}
	}

	deps := Deps{
		Cfg: &APIConfig{
			ConfigPath:                configPath,
			AlertmanagerWebhookSecret: secret,
		},
	}
	h := NewAlertmanagerWebhookHandler(deps)

	r := gin.New()
	r.POST("/api/v1/webhooks/alertmanager", h.Receive)
	return r
}

func TestAlertmanagerWebhook_DisabledWhenNoSecretConfigured(t *testing.T) {
	r := newWebhookTestRouter(t, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(`{"alerts":[]}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when webhook secret is unset, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertmanagerWebhook_RejectsMissingSecretHeader(t *testing.T) {
	r := newWebhookTestRouter(t, "correct-secret", "services: []\n")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(`{"alerts":[]}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no X-Webhook-Secret header, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertmanagerWebhook_RejectsWrongSecretHeader(t *testing.T) {
	r := newWebhookTestRouter(t, "correct-secret", "services: []\n")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(`{"alerts":[]}`))
	req.Header.Set("X-Webhook-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong X-Webhook-Secret, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertmanagerWebhook_SkipsAlertMissingServiceLabel(t *testing.T) {
	r := newWebhookTestRouter(t, "correct-secret", "services: []\n")

	body := `{"alerts":[{"status":"firing","labels":{},"annotations":{}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(body))
	req.Header.Set("X-Webhook-Secret", "correct-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing infrawatch_service label") {
		t.Fatalf("expected skip reason for missing label, got body: %s", w.Body.String())
	}
}

func TestAlertmanagerWebhook_SkipsUnknownService(t *testing.T) {
	r := newWebhookTestRouter(t, "correct-secret", "services: []\n")

	body := `{"alerts":[{"status":"firing","labels":{"infrawatch_service":"ghost"},"annotations":{}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(body))
	req.Header.Set("X-Webhook-Secret", "correct-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown service") {
		t.Fatalf("expected skip reason for unknown service, got body: %s", w.Body.String())
	}
}

func TestAlertmanagerWebhook_SkipsActiveModeService(t *testing.T) {
	configYAML := "services:\n  - name: web\n    mode: active\n    url: http://example.invalid\n"
	r := newWebhookTestRouter(t, "correct-secret", configYAML)

	body := `{"alerts":[{"status":"firing","labels":{"infrawatch_service":"web"},"annotations":{}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/alertmanager", strings.NewReader(body))
	req.Header.Set("X-Webhook-Secret", "correct-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not mode=passive") {
		t.Fatalf("expected skip reason for active-mode service, got body: %s", w.Body.String())
	}
}
