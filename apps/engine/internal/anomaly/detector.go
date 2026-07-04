package anomaly

import (
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// AnomalyType classifies what kind of anomaly was detected.
type AnomalyType string

const (
	AnomalyLatency AnomalyType = "LATENCY"
	AnomalyMemory  AnomalyType = "MEMORY"
)

// Anomaly represents a detected anomaly event.
type Anomaly struct {
	ServiceName string
	Type        AnomalyType
	Message     string
	Value       float64
	Baseline    float64
	Timestamp   time.Time
}

// Detector wraps both the latency and memory anomaly sub-systems.
type Detector struct {
	mu      sync.Mutex
	cfg     config.AnomalyConfig
	latency *LatencyDetector
	memory  *MemoryDetector
	logger  *zap.Logger

	// Channel other parts of the system subscribe to for anomaly events
	anomalyCh chan Anomaly
}

// NewDetector creates a Detector with both sub-detectors initialised.
func NewDetector(cfg config.AnomalyConfig, logger *zap.Logger) *Detector {
	return &Detector{
		cfg:       cfg,
		latency:   NewLatencyDetector(cfg),
		memory:    NewMemoryDetector(cfg),
		logger:    logger,
		anomalyCh: make(chan Anomaly, 256),
	}
}

// RecordLatency feeds a new latency observation into the detector.
// If an anomaly is detected, it is emitted on the anomaly channel.
func (d *Detector) RecordLatency(serviceName string, responseTime time.Duration) {
	anomaly, detected := d.latency.Record(serviceName, responseTime)
	if detected {
		d.logger.Sugar().Warnw("latency anomaly detected",
			"service", serviceName,
			"value_ms", anomaly.Value,
			"baseline_ms", anomaly.Baseline,
		)
		select {
		case d.anomalyCh <- anomaly:
		default:
			// Channel full — drop, not block
		}
	}
}

// RecordMemory feeds a container memory sample (in bytes) into the detector.
func (d *Detector) RecordMemory(serviceName string, memoryBytes int64) {
	anomaly, detected := d.memory.Record(serviceName, memoryBytes)
	if detected {
		d.logger.Sugar().Warnw("memory anomaly detected",
			"service", serviceName,
			"growth_mb_per_min", anomaly.Value,
			"threshold_mb_per_min", anomaly.Baseline,
		)
		select {
		case d.anomalyCh <- anomaly:
		default:
		}
	}
}

// Anomalies returns the channel on which anomaly events are published.
func (d *Detector) Anomalies() <-chan Anomaly {
	return d.anomalyCh
}

// BaselineFor returns the current hourly P95 baseline for a service (exposed
// for the metrics API).
func (d *Detector) BaselineFor(serviceName string) float64 {
	return d.latency.CurrentBaseline(serviceName)
}
