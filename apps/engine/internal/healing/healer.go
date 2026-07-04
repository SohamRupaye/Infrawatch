package healing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// HealResult captures the outcome of a single healing attempt.
type HealResult struct {
	Action    string
	Success   bool
	Error     error
	Timestamp time.Time
}

// ErrorString returns the error as a string, or "" if nil.
func (r *HealResult) ErrorString() string {
	if r.Error != nil {
		return r.Error.Error()
	}
	return ""
}

// attemptTracker tracks per-service restart counts for enforcement of MaxRestartAttempts.
type attemptTracker struct {
	mu       sync.Mutex
	counts   map[string]int
	resetAt  map[string]time.Time
	window   time.Duration
}

func newAttemptTracker(window time.Duration) *attemptTracker {
	return &attemptTracker{
		counts:  make(map[string]int),
		resetAt: make(map[string]time.Time),
		window:  window,
	}
}

func (t *attemptTracker) canAttempt(name string, max int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Reset counter if the cooldown window has elapsed since the last decay
	if last, ok := t.resetAt[name]; ok && time.Since(last) > t.window {
		delete(t.counts, name)
		delete(t.resetAt, name)
	}

	if t.counts[name] >= max {
		return false
	}
	t.counts[name]++
	if _, ok := t.resetAt[name]; !ok {
		t.resetAt[name] = time.Now()
	}
	return true
}

// Healer orchestrates self-healing actions for a service.
// It reads the ordered list of HealingActions from the service config and
// executes each in sequence until one succeeds.
type Healer struct {
	cfg      config.HealingConfig
	docker   *DockerHealer
	kubectl  *KubectlHealer
	fallback *FallbackHealer
	tracker  *attemptTracker
	logger   *zap.Logger
}

// NewHealer creates a Healer. Returns an error only if a required sub-healer
// cannot be initialised (e.g. Docker socket unreachable and docker actions are
// configured — but we fail lazily at heal time to stay flexible).
func NewHealer(cfg config.HealingConfig, dockerSocketPath string, logger *zap.Logger) (*Healer, error) {
	window := cfg.RestartCooldown
	if window <= 0 {
		window = 60 * time.Second
	}

	h := &Healer{
		cfg:      cfg,
		docker:   NewDockerHealer(dockerSocketPath, logger),
		kubectl:  NewKubectlHealer(cfg.KubeconfigPath, logger),
		fallback: NewFallbackHealer(logger),
		tracker:  newAttemptTracker(window),
		logger:   logger,
	}
	return h, nil
}

// Cooldown returns the configured restart cooldown duration.
func (h *Healer) Cooldown() time.Duration {
	return h.cfg.RestartCooldown
}

// Heal executes healing actions for the given service. It tries each action in
// the configured order and returns the result of the first one that succeeds.
// If max restart attempts are exhausted, it returns a failure result.
func (h *Healer) Heal(ctx context.Context, svc config.ServiceConfig) HealResult {
	if !h.cfg.Enabled {
		return HealResult{
			Action:    "none",
			Success:   false,
			Error:     fmt.Errorf("healing is disabled in config"),
			Timestamp: time.Now(),
		}
	}

	max := h.cfg.MaxRestartAttempts
	if max <= 0 {
		max = 3
	}

	if !h.tracker.canAttempt(svc.Name, max) {
		return HealResult{
			Action:    "none",
			Success:   false,
			Error:     fmt.Errorf("max restart attempts (%d) exhausted for service %s", max, svc.Name),
			Timestamp: time.Now(),
		}
	}

	actions := svc.HealingActions
	if len(actions) == 0 {
		// Infer a reasonable default when container_name is set
		if svc.ContainerName != "" {
			actions = []string{"docker_restart"}
		} else if svc.Deployment != "" {
			actions = []string{"kubectl_restart"}
		} else {
			return HealResult{
				Action:    "none",
				Success:   false,
				Error:     fmt.Errorf("no healing_actions configured for service %s", svc.Name),
				Timestamp: time.Now(),
			}
		}
	}

	sugar := h.logger.Sugar()
	for _, action := range actions {
		sugar.Infow("attempting healing action", "service", svc.Name, "action", action)

		var result HealResult
		switch action {
		case "docker_restart":
			result = h.docker.Restart(ctx, svc)
		case "kubectl_restart":
			result = h.kubectl.Restart(ctx, svc)
		case "fallback":
			result = h.fallback.Reroute(ctx, svc)
		case "webhook":
			result = h.fallback.Webhook(ctx, svc)
		default:
			sugar.Warnw("unknown healing action, skipping", "action", action)
			continue
		}

		result.Timestamp = time.Now()
		if result.Success {
			sugar.Infow("healing action succeeded",
				"service", svc.Name,
				"action", action,
			)
			return result
		}
		sugar.Warnw("healing action failed, trying next",
			"service", svc.Name,
			"action", action,
			"error", result.Error,
		)
	}

	return HealResult{
		Action:    "all_failed",
		Success:   false,
		Error:     fmt.Errorf("all healing actions failed for service %s", svc.Name),
		Timestamp: time.Now(),
	}
}
