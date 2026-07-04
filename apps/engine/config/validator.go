package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidationError accumulates multiple config errors.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

func (e *ValidationError) add(msg string, args ...interface{}) {
	e.Errors = append(e.Errors, fmt.Sprintf(msg, args...))
}

func (e *ValidationError) hasErrors() bool {
	return len(e.Errors) > 0
}

// Validate checks the config for logical errors.
func Validate(cfg *Config) error {
	ve := &ValidationError{}

	validateServices(cfg, ve)
	validateRedis(cfg, ve)
	validateAPI(cfg, ve)
	validateCircuit(cfg, ve)
	validateAnomalyConfig(cfg, ve)
	validateAlertsConfig(cfg, ve)
	validateHealingConfig(cfg, ve)

	if ve.hasErrors() {
		return ve
	}
	return nil
}

func validateServices(cfg *Config, ve *ValidationError) {
	names := make(map[string]bool)
	for i, svc := range cfg.Services {
		prefix := fmt.Sprintf("services[%d](%s)", i, svc.Name)

		if svc.Name == "" {
			ve.add("%s: name is required", prefix)
		} else if names[svc.Name] {
			ve.add("%s: duplicate service name '%s'", prefix, svc.Name)
		} else {
			names[svc.Name] = true
		}

		if svc.URL == "" {
			ve.add("%s: url is required", prefix)
		} else {
			u, err := url.Parse(svc.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				ve.add("%s: url must be a valid http/https URL, got '%s'", prefix, svc.URL)
			}
		}

		if svc.Interval < 0 {
			ve.add("%s: interval must be positive", prefix)
		}
		if svc.Timeout < 0 {
			ve.add("%s: timeout must be positive", prefix)
		}

		method := strings.ToUpper(svc.Method)
		validMethods := map[string]bool{"GET": true, "POST": true, "HEAD": true, "PUT": true, "OPTIONS": true}
		if !validMethods[method] {
			ve.add("%s: method '%s' is not supported", prefix, svc.Method)
		}

		if svc.ExpectStatus < 100 || svc.ExpectStatus > 599 {
			ve.add("%s: expect_status must be a valid HTTP status code", prefix)
		}

		for _, dep := range svc.Dependencies {
			if dep == svc.Name {
				ve.add("%s: service cannot depend on itself", prefix)
			}
		}

		for _, action := range svc.HealingActions {
			valid := map[string]bool{
				"docker_restart":  true,
				"kubectl_restart": true,
				"fallback":        true,
				"webhook":         true,
			}
			if !valid[action] {
				ve.add("%s: unknown healing action '%s'", prefix, action)
			}
		}

		for _, action := range svc.HealingActions {
			if action == "fallback" && svc.FallbackURL == "" {
				ve.add("%s: fallback_url required when healing action is 'fallback'", prefix)
			}
			if action == "webhook" && svc.HealingWebhook == "" {
				ve.add("%s: healing_webhook required when healing action is 'webhook'", prefix)
			}
			if action == "docker_restart" && svc.ContainerName == "" {
				ve.add("%s: container_name required when healing action is 'docker_restart'", prefix)
			}
			if action == "kubectl_restart" && (svc.Deployment == "" || svc.Namespace == "") {
				ve.add("%s: deployment and namespace required when healing action is 'kubectl_restart'", prefix)
			}
		}
	}
}

func validateRedis(cfg *Config, ve *ValidationError) {
	if cfg.Redis.Addr == "" {
		ve.add("redis.addr is required")
	}
	if cfg.Redis.DB < 0 || cfg.Redis.DB > 15 {
		ve.add("redis.db must be between 0 and 15")
	}
}

func validateAPI(cfg *Config, ve *ValidationError) {
	if cfg.API.Addr == "" {
		ve.add("api.addr is required")
	}
}

func validateCircuit(cfg *Config, ve *ValidationError) {
	if cfg.Circuit.FailureThreshold < 1 {
		ve.add("circuit.failure_threshold must be >= 1")
	}
	if cfg.Circuit.SuccessThreshold < 1 {
		ve.add("circuit.success_threshold must be >= 1")
	}
	if cfg.Circuit.Timeout <= 0 {
		ve.add("circuit.timeout must be positive")
	}
}

func validateAnomalyConfig(cfg *Config, ve *ValidationError) {
	if cfg.Anomaly.LatencyMultiplier < 1.0 {
		ve.add("anomaly.latency_multiplier must be >= 1.0")
	}
	if cfg.Anomaly.MemoryGrowthRateMB <= 0 {
		ve.add("anomaly.memory_growth_rate_mb must be positive")
	}
}

func validateAlertsConfig(cfg *Config, ve *ValidationError) {
	if cfg.Alerts.Slack.Enabled && cfg.Alerts.Slack.WebhookURL == "" {
		ve.add("alerts.slack.webhook_url required when slack is enabled")
	}
	if cfg.Alerts.PagerDuty.Enabled && cfg.Alerts.PagerDuty.IntegrationKey == "" {
		ve.add("alerts.pagerduty.integration_key required when pagerduty is enabled")
	}
	if cfg.Alerts.Email.Enabled {
		if cfg.Alerts.Email.SMTPHost == "" {
			ve.add("alerts.email.smtp_host required when email is enabled")
		}
		if cfg.Alerts.Email.From == "" {
			ve.add("alerts.email.from required when email is enabled")
		}
		if len(cfg.Alerts.Email.Recipients) == 0 {
			ve.add("alerts.email.recipients must not be empty when email is enabled")
		}
	}
	if cfg.Alerts.Webhook.Enabled && cfg.Alerts.Webhook.URL == "" {
		ve.add("alerts.webhook.url required when webhook is enabled")
	}
}

func validateHealingConfig(cfg *Config, ve *ValidationError) {
	if cfg.Healing.MaxRestartAttempts < 0 {
		ve.add("healing.max_restart_attempts must be >= 0")
	}
}
