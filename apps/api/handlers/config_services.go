package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/api/store"
	engineconfig "github.com/SohamRupaye/infrawatch/apps/engine/config"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

var configMu sync.Mutex

// ConfigServiceHandler exposes service config CRUD with YAML persistence.
type ConfigServiceHandler struct{ deps Deps }

func NewConfigServiceHandler(deps Deps) *ConfigServiceHandler {
	return &ConfigServiceHandler{deps: deps}
}

type serviceUpsertRequest struct {
	Name           string            `json:"name"`
	Mode           string            `json:"mode"` // "active" (default) or "passive"
	URL            string            `json:"url"`
	Interval       string            `json:"interval"`
	Timeout        string            `json:"timeout"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	ExpectStatus   int               `json:"expect_status"`
	ExpectBody     string            `json:"expect_body"`
	Tags           []string          `json:"tags"`
	Dependencies   []string          `json:"dependencies"`
	ContainerName  string            `json:"container_name"`
	Namespace      string            `json:"namespace"`
	Deployment     string            `json:"deployment"`
	HealingActions []string          `json:"healing_actions"`
	FallbackURL    string            `json:"fallback_url"`
	HealingWebhook string            `json:"healing_webhook"`
}

// List godoc
// GET /api/v1/config/services
func (h *ConfigServiceHandler) List(c *gin.Context) {
	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"services": cfg.Services, "total": len(cfg.Services)})
}

// Create godoc
// POST /api/v1/config/services
func (h *ConfigServiceHandler) Create(c *gin.Context) {
	var req serviceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	svc, err := requestToService(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}
	if serviceIndex(cfg.Services, svc.Name) >= 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "service already exists: " + svc.Name})
		return
	}

	cfg.Services = append(cfg.Services, svc)
	if err := validateConfigForPersist(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := saveConfigFile(h.deps.Cfg.ConfigPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config: " + err.Error()})
		return
	}

	if err := h.deps.Bus.PublishServiceConfigCommand(c.Request.Context(), enginepkg.ServiceConfigCommand{
		Action:      "upsert",
		ServiceName: svc.Name,
		Service:     svc,
		Timestamp:   time.Now().UTC(),
	}); err != nil {
		c.JSON(http.StatusAccepted, gin.H{
			"warning": "service persisted, but runtime apply command failed",
			"error":   err.Error(),
			"service": svc,
		})
		return
	}

	if h.deps.DB != nil {
		userID, _ := c.Get("user_id")
		h.deps.DB.WriteConfigAudit(store.ConfigAuditEntry{
			Action:  "create",
			Service: svc.Name,
			UserID:  fmt.Sprintf("%v", userID),
			Details: svc,
		})
	}

	c.JSON(http.StatusCreated, gin.H{"ok": true, "service": svc})
}

// Update godoc
// PUT /api/v1/config/services/:name
func (h *ConfigServiceHandler) Update(c *gin.Context) {
	target := c.Param("name")

	var req serviceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Name == "" {
		req.Name = target
	}
	if req.Name != target {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service name in body must match path parameter"})
		return
	}

	svc, err := requestToService(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}
	idx := serviceIndex(cfg.Services, target)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found: " + target})
		return
	}

	cfg.Services[idx] = svc
	if err := validateConfigForPersist(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := saveConfigFile(h.deps.Cfg.ConfigPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config: " + err.Error()})
		return
	}

	if err := h.deps.Bus.PublishServiceConfigCommand(c.Request.Context(), enginepkg.ServiceConfigCommand{
		Action:      "upsert",
		ServiceName: svc.Name,
		Service:     svc,
		Timestamp:   time.Now().UTC(),
	}); err != nil {
		c.JSON(http.StatusAccepted, gin.H{
			"warning": "service updated in YAML, but runtime apply command failed",
			"error":   err.Error(),
			"service": svc,
		})
		return
	}

	if h.deps.DB != nil {
		userID, _ := c.Get("user_id")
		h.deps.DB.WriteConfigAudit(store.ConfigAuditEntry{
			Action:  "update",
			Service: svc.Name,
			UserID:  fmt.Sprintf("%v", userID),
			Details: svc,
		})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "service": svc})
}

// Delete godoc
// DELETE /api/v1/config/services/:name
func (h *ConfigServiceHandler) Delete(c *gin.Context) {
	target := c.Param("name")

	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}
	idx := serviceIndex(cfg.Services, target)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found: " + target})
		return
	}

	cfg.Services = append(cfg.Services[:idx], cfg.Services[idx+1:]...)
	if err := validateConfigForPersist(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := saveConfigFile(h.deps.Cfg.ConfigPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config: " + err.Error()})
		return
	}

	if err := h.deps.Bus.PublishServiceConfigCommand(c.Request.Context(), enginepkg.ServiceConfigCommand{
		Action:      "delete",
		ServiceName: target,
		Timestamp:   time.Now().UTC(),
	}); err != nil {
		c.JSON(http.StatusAccepted, gin.H{
			"warning": "service removed from YAML, but runtime apply command failed",
			"error":   err.Error(),
		})
		return
	}

	if h.deps.DB != nil {
		userID, _ := c.Get("user_id")
		h.deps.DB.WriteConfigAudit(store.ConfigAuditEntry{
			Action:  "delete",
			Service: target,
			UserID:  fmt.Sprintf("%v", userID),
		})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "service": target})
}

func requestToService(req serviceUpsertRequest) (engineconfig.ServiceConfig, error) {
	if strings.TrimSpace(req.Name) == "" {
		return engineconfig.ServiceConfig{}, fmt.Errorf("name is required")
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = engineconfig.ModeActive
	}
	if mode != engineconfig.ModeActive && mode != engineconfig.ModePassive {
		return engineconfig.ServiceConfig{}, fmt.Errorf("mode must be '%s' or '%s'", engineconfig.ModeActive, engineconfig.ModePassive)
	}
	isPassive := mode == engineconfig.ModePassive

	if !isPassive && strings.TrimSpace(req.URL) == "" {
		return engineconfig.ServiceConfig{}, fmt.Errorf("url is required")
	}
	if isPassive && strings.TrimSpace(req.URL) != "" {
		return engineconfig.ServiceConfig{}, fmt.Errorf("url must not be set when mode is '%s'", engineconfig.ModePassive)
	}

	interval := 30 * time.Second
	timeout := 5 * time.Second
	var err error

	if req.Interval != "" {
		interval, err = time.ParseDuration(req.Interval)
		if err != nil {
			return engineconfig.ServiceConfig{}, fmt.Errorf("invalid interval: %w", err)
		}
	}
	if req.Timeout != "" {
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return engineconfig.ServiceConfig{}, fmt.Errorf("invalid timeout: %w", err)
		}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	expectStatus := req.ExpectStatus
	if expectStatus == 0 {
		expectStatus = 200
	}

	return engineconfig.ServiceConfig{
		Name:           req.Name,
		Mode:           mode,
		URL:            req.URL,
		Interval:       interval,
		Timeout:        timeout,
		Method:         method,
		Headers:        req.Headers,
		ExpectStatus:   expectStatus,
		ExpectBody:     req.ExpectBody,
		Tags:           req.Tags,
		Dependencies:   req.Dependencies,
		ContainerName:  req.ContainerName,
		Namespace:      req.Namespace,
		Deployment:     req.Deployment,
		HealingActions: req.HealingActions,
		FallbackURL:    req.FallbackURL,
		HealingWebhook: req.HealingWebhook,
	}, nil
}

func loadConfigFile(path string) (*engineconfig.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg engineconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfigFile(path string, cfg *engineconfig.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "infrawatch-config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func serviceIndex(services []engineconfig.ServiceConfig, name string) int {
	for i := range services {
		if services[i].Name == name {
			return i
		}
	}
	return -1
}

func validateConfigForPersist(cfg *engineconfig.Config) error {
	normalized := *cfg
	applyValidationDefaults(&normalized)
	return engineconfig.Validate(&normalized)
}

func applyValidationDefaults(cfg *engineconfig.Config) {
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.API.Addr == "" {
		cfg.API.Addr = ":8080"
	}
	if cfg.Circuit.FailureThreshold == 0 {
		cfg.Circuit.FailureThreshold = 3
	}
	if cfg.Circuit.SuccessThreshold == 0 {
		cfg.Circuit.SuccessThreshold = 1
	}
	if cfg.Circuit.Timeout == 0 {
		cfg.Circuit.Timeout = 30 * time.Second
	}
	if cfg.Anomaly.LatencyMultiplier == 0 {
		cfg.Anomaly.LatencyMultiplier = 2.0
	}
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		if svc.Mode == "" {
			svc.Mode = engineconfig.ModeActive
		}
		if svc.Interval == 0 {
			svc.Interval = 30 * time.Second
		}
		if svc.Timeout == 0 {
			svc.Timeout = 5 * time.Second
		}
		if svc.Mode == engineconfig.ModePassive {
			continue
		}
		if svc.Method == "" {
			svc.Method = "GET"
		}
		if svc.ExpectStatus == 0 {
			svc.ExpectStatus = 200
		}
	}
}
