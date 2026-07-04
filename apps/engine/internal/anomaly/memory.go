package anomaly

import (
	"fmt"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

// memorySample is a timestamped memory reading.
type memorySample struct {
	bytes     int64
	timestamp time.Time
}

// memoryTracker holds a rolling window of memory samples for one service.
type memoryTracker struct {
	mu      sync.Mutex
	samples []memorySample
	window  time.Duration
}

func newMemoryTracker(window time.Duration) *memoryTracker {
	return &memoryTracker{
		window: window,
	}
}

// add inserts a new sample and prunes samples older than the window.
func (m *memoryTracker) add(bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.samples = append(m.samples, memorySample{bytes: bytes, timestamp: now})

	// Prune old samples
	cutoff := now.Add(-m.window)
	var fresh []memorySample
	for _, s := range m.samples {
		if s.timestamp.After(cutoff) {
			fresh = append(fresh, s)
		}
	}
	m.samples = fresh
}

// growthRateMBPerMin computes the linear growth rate over the current window.
// Returns 0 if there are fewer than 2 samples.
func (m *memoryTracker) growthRateMBPerMin() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.samples) < 2 {
		return 0
	}

	// Use first and last sample in the window for a simple linear estimate
	first := m.samples[0]
	last := m.samples[len(m.samples)-1]

	durationMins := last.timestamp.Sub(first.timestamp).Minutes()
	if durationMins <= 0 {
		return 0
	}

	deltaBytes := float64(last.bytes - first.bytes)
	deltaMB := deltaBytes / (1024 * 1024)
	return deltaMB / durationMins
}

// MemoryDetector watches container memory growth rates and fires anomalies.
type MemoryDetector struct {
	mu         sync.Mutex
	cfg        config.AnomalyConfig
	trackers   map[string]*memoryTracker
	lastAlerts map[string]time.Time
}

// NewMemoryDetector creates a MemoryDetector.
func NewMemoryDetector(cfg config.AnomalyConfig) *MemoryDetector {
	return &MemoryDetector{
		cfg:        cfg,
		trackers:   make(map[string]*memoryTracker),
		lastAlerts: make(map[string]time.Time),
	}
}

// Record adds a memory sample (in bytes) for a service and returns an Anomaly
// if the growth rate exceeds the configured threshold.
func (d *MemoryDetector) Record(serviceName string, memBytes int64) (Anomaly, bool) {
	d.mu.Lock()
	tracker, ok := d.trackers[serviceName]
	if !ok {
		tracker = newMemoryTracker(d.cfg.EvaluationWindow)
		d.trackers[serviceName] = tracker
	}
	d.mu.Unlock()

	tracker.add(memBytes)
	rate := tracker.growthRateMBPerMin()

	if rate < d.cfg.MemoryGrowthRateMB {
		return Anomaly{}, false
	}

	// Rate-limit: only fire once per 10 minutes per service
	d.mu.Lock()
	lastAlert, alerted := d.lastAlerts[serviceName]
	if alerted && time.Since(lastAlert) < 10*time.Minute {
		d.mu.Unlock()
		return Anomaly{}, false
	}
	d.lastAlerts[serviceName] = time.Now()
	d.mu.Unlock()

	return Anomaly{
		ServiceName: serviceName,
		Type:        AnomalyMemory,
		Message: fmt.Sprintf(
			"memory growing at %.1fMB/min, threshold is %.1fMB/min — possible leak",
			rate, d.cfg.MemoryGrowthRateMB,
		),
		Value:     rate,
		Baseline:  d.cfg.MemoryGrowthRateMB,
		Timestamp: time.Now(),
	}, true
}
