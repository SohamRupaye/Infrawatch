package anomaly

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

const (
	// maxSamples is the ring-buffer size per (service, hour) bucket.
	maxSamples = 1000
	// p95Percentile is the target percentile for threshold calculation.
	p95Percentile = 0.95
)

// latencySeries holds response time samples for one (service, hour) bucket.
type latencySeries struct {
	mu      sync.Mutex
	samples []float64 // milliseconds
	cursor  int
	full    bool
}

func newLatencySeries() *latencySeries {
	return &latencySeries{samples: make([]float64, maxSamples)}
}

// add inserts a new sample into the ring buffer.
func (ls *latencySeries) add(ms float64) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.samples[ls.cursor] = ms
	ls.cursor = (ls.cursor + 1) % maxSamples
	if ls.cursor == 0 {
		ls.full = true
	}
}

// p95 computes the 95th percentile of current samples.
func (ls *latencySeries) p95() float64 {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	var valid []float64
	limit := ls.cursor
	if ls.full {
		limit = maxSamples
	}
	for i := 0; i < limit; i++ {
		valid = append(valid, ls.samples[i])
	}
	if len(valid) == 0 {
		return 0
	}
	sorted := make([]float64, len(valid))
	copy(sorted, valid)
	sort.Float64s(sorted)

	idx := int(math.Ceil(p95Percentile*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// seriesKey builds a map key for a (service, hour) pair.
func seriesKey(serviceName string, hour int) string {
	return fmt.Sprintf("%s:%02d", serviceName, hour)
}

// LatencyDetector maintains per-service, per-hour P95 baselines and fires
// when a new sample exceeds (multiplier × baseline).
type LatencyDetector struct {
	mu         sync.Mutex
	cfg        config.AnomalyConfig
	series     map[string]*latencySeries // keyed by seriesKey(name, hour)
	lastAlerts map[string]time.Time      // rate-limit repeated alerts
}

// NewLatencyDetector creates a LatencyDetector.
func NewLatencyDetector(cfg config.AnomalyConfig) *LatencyDetector {
	return &LatencyDetector{
		cfg:        cfg,
		series:     make(map[string]*latencySeries),
		lastAlerts: make(map[string]time.Time),
	}
}

// Record adds a sample and returns an Anomaly if one is detected.
func (d *LatencyDetector) Record(serviceName string, rt time.Duration) (Anomaly, bool) {
	ms := float64(rt.Milliseconds())
	hour := time.Now().Hour()
	key := seriesKey(serviceName, hour)

	d.mu.Lock()
	s, ok := d.series[key]
	if !ok {
		s = newLatencySeries()
		d.series[key] = s
	}
	d.mu.Unlock()

	s.add(ms)

	baseline := s.p95()
	if baseline == 0 {
		return Anomaly{}, false
	}

	threshold := baseline * d.cfg.LatencyMultiplier
	if ms < threshold {
		return Anomaly{}, false
	}

	// Rate-limit: only fire once per 5 minutes per service
	d.mu.Lock()
	lastAlert, alerted := d.lastAlerts[serviceName]
	if alerted && time.Since(lastAlert) < 5*time.Minute {
		d.mu.Unlock()
		return Anomaly{}, false
	}
	d.lastAlerts[serviceName] = time.Now()
	d.mu.Unlock()

	return Anomaly{
		ServiceName: serviceName,
		Type:        AnomalyLatency,
		Message:     fmt.Sprintf("P95 latency %.0fms exceeds %.1fx baseline %.0fms", ms, d.cfg.LatencyMultiplier, baseline),
		Value:       ms,
		Baseline:    baseline,
		Timestamp:   time.Now(),
	}, true
}

// CurrentBaseline returns the current hour's P95 for a service.
func (d *LatencyDetector) CurrentBaseline(serviceName string) float64 {
	hour := time.Now().Hour()
	key := seriesKey(serviceName, hour)
	d.mu.Lock()
	s, ok := d.series[key]
	d.mu.Unlock()
	if !ok {
		return 0
	}
	return s.p95()
}
