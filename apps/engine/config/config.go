package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for Infrawatch.
type Config struct {
	Services     []ServiceConfig    `yaml:"services"`
	Redis        RedisConfig        `yaml:"redis"`
	API          APIConfig          `yaml:"api"`
	Circuit      CircuitConfig      `yaml:"circuit"`
	Anomaly      AnomalyConfig      `yaml:"anomaly"`
	Alerts       AlertsConfig       `yaml:"alerts"`
	Healing      HealingConfig      `yaml:"healing"`
	Docker       DockerConfig       `yaml:"docker"`
	Storage      StorageConfig      `yaml:"storage"`
	Alertmanager AlertmanagerConfig `yaml:"alertmanager"`
}

// Service modes. Active services are polled directly over HTTP by the
// engine. Passive services have no poll URL — their health is driven
// entirely by external signals (e.g. Alertmanager webhook alerts).
const (
	ModeActive  = "active"
	ModePassive = "passive"
)

// ServiceConfig defines a single monitored service.
type ServiceConfig struct {
	Name         string            `yaml:"name"           json:"name"`
	Mode         string            `yaml:"mode"           json:"mode"` // "active" (default) or "passive"
	URL          string            `yaml:"url"            json:"url"`
	Interval     time.Duration     `yaml:"interval"       json:"interval"`
	Timeout      time.Duration     `yaml:"timeout"        json:"timeout"`
	Method       string            `yaml:"method"         json:"method"`
	Headers      map[string]string `yaml:"headers"        json:"headers"`
	ExpectStatus int               `yaml:"expect_status"  json:"expect_status"`
	ExpectBody   string            `yaml:"expect_body"    json:"expect_body"` // optional string match in response
	Tags         []string          `yaml:"tags"           json:"tags"`
	Dependencies []string          `yaml:"dependencies"   json:"dependencies"`

	// Container for Docker-native health + self-healing
	ContainerName string `yaml:"container_name" json:"container_name"`
	Namespace     string `yaml:"namespace"      json:"namespace"`  // for K8s
	Deployment    string `yaml:"deployment"     json:"deployment"` // for K8s

	// Per-service overrides
	HealingActions []string `yaml:"healing_actions" json:"healing_actions"` // docker_restart, kubectl_restart, fallback, webhook
	FallbackURL    string   `yaml:"fallback_url"    json:"fallback_url"`
	HealingWebhook string   `yaml:"healing_webhook" json:"healing_webhook"`
}

// IsPassive reports whether the service is driven by external signals
// (e.g. Alertmanager) instead of the engine's own HTTP poller.
func (s ServiceConfig) IsPassive() bool {
	return s.Mode == ModePassive
}

// AlertmanagerConfig configures the API's Alertmanager webhook receiver.
type AlertmanagerConfig struct {
	Enabled       bool   `yaml:"enabled"`
	WebhookSecret string `yaml:"webhook_secret"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// APIConfig holds HTTP API server settings.
type APIConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	JWTSecret    string        `yaml:"jwt_secret"`
	AllowOrigins []string      `yaml:"allow_origins"`
}

// CircuitConfig defines circuit breaker defaults.
type CircuitConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"`
	SuccessThreshold int           `yaml:"success_threshold"`
	Timeout          time.Duration `yaml:"timeout"` // half-open probe delay
}

// AnomalyConfig defines anomaly detection settings.
type AnomalyConfig struct {
	LatencyMultiplier float64 `yaml:"latency_multiplier"` // default 2.0
}

// AlertsConfig holds multi-channel alert configuration.
type AlertsConfig struct {
	Slack     SlackConfig     `yaml:"slack"`
	PagerDuty PagerDutyConfig `yaml:"pagerduty"`
	Email     EmailConfig     `yaml:"email"`
	Webhook   WebhookConfig   `yaml:"webhook"`

	// StateRules maps state names to which channels fire
	StateRules          map[string][]string `yaml:"state_rules"`
	EscalateOnWorsening bool                `yaml:"escalate_on_worsening"`
}

// SlackConfig holds Slack webhook details.
type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url"`
	Channel    string `yaml:"channel"`
	Enabled    bool   `yaml:"enabled"`
}

// PagerDutyConfig holds PagerDuty integration settings.
type PagerDutyConfig struct {
	IntegrationKey string `yaml:"integration_key"`
	Enabled        bool   `yaml:"enabled"`
}

// EmailConfig holds SMTP configuration.
type EmailConfig struct {
	SMTPHost   string   `yaml:"smtp_host"`
	SMTPPort   int      `yaml:"smtp_port"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	From       string   `yaml:"from"`
	Recipients []string `yaml:"recipients"`
	Enabled    bool     `yaml:"enabled"`
}

// WebhookConfig holds generic outbound webhook settings.
type WebhookConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Enabled bool              `yaml:"enabled"`
}

// StorageConfig holds TimescaleDB connection settings.
type StorageConfig struct {
	DSN          string `yaml:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// HealingConfig holds global self-healing settings.
type HealingConfig struct {
	Enabled            bool          `yaml:"enabled"`
	KubeconfigPath     string        `yaml:"kubeconfig_path"`
	MaxRestartAttempts int           `yaml:"max_restart_attempts"`
	RestartCooldown    time.Duration `yaml:"restart_cooldown"`
}

// DockerConfig holds Docker auto-discovery settings.
type DockerConfig struct {
	AutoDiscover  bool   `yaml:"auto_discover"`
	SocketPath    string `yaml:"socket_path"`
	NetworkFilter string `yaml:"network_filter"`
}

// Load reads and parses the YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand env vars
	expanded := os.ExpandEnv(string(data))

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}

	applyDefaults(cfg)
	return cfg, nil
}

// Default returns a minimal usable config when no file is present.
func Default() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}
	if cfg.API.Addr == "" {
		cfg.API.Addr = ":8080"
	}
	if cfg.API.ReadTimeout == 0 {
		cfg.API.ReadTimeout = 15 * time.Second
	}
	if cfg.API.WriteTimeout == 0 {
		cfg.API.WriteTimeout = 15 * time.Second
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
	if cfg.Healing.MaxRestartAttempts == 0 {
		cfg.Healing.MaxRestartAttempts = 3
	}
	if cfg.Healing.RestartCooldown == 0 {
		cfg.Healing.RestartCooldown = 60 * time.Second
	}
	if cfg.Docker.SocketPath == "" {
		cfg.Docker.SocketPath = "/var/run/docker.sock"
	}
	if cfg.Storage.MaxOpenConns == 0 {
		cfg.Storage.MaxOpenConns = 10
	}
	if cfg.Storage.MaxIdleConns == 0 {
		cfg.Storage.MaxIdleConns = 5
	}

	// Apply service-level defaults
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		if svc.Mode == "" {
			svc.Mode = ModeActive
		}
		if svc.Interval == 0 {
			svc.Interval = 30 * time.Second
		}
		if svc.Timeout == 0 {
			svc.Timeout = 5 * time.Second
		}
		// Passive services have no URL to poll, so HTTP-check-specific
		// defaults would be meaningless.
		if svc.Mode == ModePassive {
			continue
		}
		if svc.Method == "" {
			svc.Method = "GET"
		}
		if svc.ExpectStatus == 0 {
			svc.ExpectStatus = 200
		}
	}

	// Default state rules: page on DEAD, Slack on DEGRADED+UNHEALTHY
	if cfg.Alerts.StateRules == nil {
		cfg.Alerts.StateRules = map[string][]string{
			"DEGRADED":   {"slack"},
			"UNHEALTHY":  {"slack"},
			"DEAD":       {"slack", "pagerduty", "email"},
			"RECOVERING": {"slack"},
			"HEALTHY":    {},
		}
	}
}
