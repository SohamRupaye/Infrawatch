package handlers

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/api/store"
	"github.com/gin-gonic/gin"
)

// MetricsHandler serves time-series metrics for individual services.
type MetricsHandler struct{ deps Deps }

func NewMetricsHandler(deps Deps) *MetricsHandler { return &MetricsHandler{deps} }

// Get godoc
// GET /api/v1/metrics/:service?range=1h|6h|24h|7d
// Returns filtered metric points plus computed P50, P95, error rate, and uptime pct.
// When TimescaleDB is available the full historical window is served from the DB.
// When the DB is not configured the in-memory ring buffer is used as a fallback.
func (h *MetricsHandler) Get(c *gin.Context) {
	name := c.Param("service")
	rangeStr := c.DefaultQuery("range", "1h")
	cutoff := rangeCutoff(rangeStr)

	// ── TimescaleDB path ──────────────────────────────────────────────────────
	if h.deps.DB != nil {
		pts, err := h.deps.DB.QueryMetrics(c.Request.Context(), name, cutoff)
		if err != nil {
			h.deps.Logger.Sugar().Errorw("metrics: DB query failed, falling back to in-memory",
				"service", name, "error", err)
			// fall through to in-memory path
		} else {
			p50, p95 := computePercentilesFromDB(pts)
			errorRate := computeErrorRateFromDB(pts)
			c.JSON(http.StatusOK, gin.H{
				"service":    name,
				"range":      rangeStr,
				"total":      len(pts),
				"p50_ms":     p50,
				"p95_ms":     p95,
				"error_rate": errorRate,
				"source":     "timescaledb",
				"points":     pts,
			})
			return
		}
	}

	// ── In-memory fallback path ───────────────────────────────────────────────
	all := h.deps.Store.MetricsFor(name)

	filtered := make([]store.MetricPoint, 0, len(all))
	for _, pt := range all {
		if pt.Timestamp.After(cutoff) {
			filtered = append(filtered, pt)
		}
	}

	p50, p95 := computePercentiles(filtered)
	errorRate := computeErrorRate(filtered)

	c.JSON(http.StatusOK, gin.H{
		"service":    name,
		"range":      rangeStr,
		"total":      len(filtered),
		"p50_ms":     p50,
		"p95_ms":     p95,
		"error_rate": errorRate,
		"source":     "in-memory",
		"points":     filtered,
	})
}

// Baseline godoc
// GET /api/v1/metrics/:service/baseline
// Returns the current hourly P95 latency baseline.
// Uses TimescaleDB when available for an accurate 1-hour window.
func (h *MetricsHandler) Baseline(c *gin.Context) {
	name := c.Param("service")
	cutoff := time.Now().Add(-1 * time.Hour)

	// ── TimescaleDB path ──────────────────────────────────────────────────────
	if h.deps.DB != nil {
		pts, err := h.deps.DB.QueryMetrics(c.Request.Context(), name, cutoff)
		if err != nil {
			h.deps.Logger.Sugar().Warnw("baseline: DB query failed, falling back to in-memory",
				"service", name, "error", err)
			// fall through
		} else {
			var latencies []float64
			for _, p := range pts {
				if p.LatencyMs > 0 {
					latencies = append(latencies, p.LatencyMs)
				}
			}
			sort.Float64s(latencies)
			c.JSON(http.StatusOK, gin.H{
				"service":      name,
				"baseline_ms":  computeP95(latencies),
				"sample_count": len(latencies),
				"window":       "1h",
				"source":       "timescaledb",
			})
			return
		}
	}

	// ── In-memory fallback path ───────────────────────────────────────────────
	all := h.deps.Store.MetricsFor(name)

	var latencies []float64
	for _, pt := range all {
		if pt.Timestamp.After(cutoff) && pt.ResponseTimeMs > 0 {
			latencies = append(latencies, float64(pt.ResponseTimeMs))
		}
	}
	sort.Float64s(latencies)

	c.JSON(http.StatusOK, gin.H{
		"service":      name,
		"baseline_ms":  computeP95(latencies),
		"sample_count": len(latencies),
		"window":       "1h",
		"source":       "in-memory",
	})
}

// GroupByTag godoc
// GET /api/v1/metrics/grouped/by-tag?range=1h|6h|24h|7d
func (h *MetricsHandler) GroupByTag(c *gin.Context) {
	rangeStr := c.DefaultQuery("range", "1h")
	cutoff := rangeCutoff(rangeStr)

	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}
	tagsByService := serviceTagsMap(cfg.Services)

	type agg struct {
		Tag      string  `json:"tag"`
		Points   int     `json:"points"`
		Errors   int     `json:"errors"`
		Latency  float64 `json:"avg_latency_ms"`
		services int
	}

	acc := make(map[string]*agg)
	accumulate := func(service string, ok bool, latency float64) {
		for _, tag := range tagsByService[service] {
			a := acc[tag]
			if a == nil {
				a = &agg{Tag: tag}
				acc[tag] = a
			}
			a.Points++
			if !ok {
				a.Errors++
			}
			a.Latency += latency
		}
	}

	if h.deps.DB != nil {
		for service := range tagsByService {
			points, err := h.deps.DB.QueryMetrics(c.Request.Context(), service, cutoff)
			if err != nil {
				continue
			}
			for _, p := range points {
				accumulate(service, p.IsHealthy, p.LatencyMs)
			}
		}
	} else {
		for service := range tagsByService {
			points := h.deps.Store.MetricsFor(service)
			for _, p := range points {
				if p.Timestamp.After(cutoff) {
					accumulate(service, p.OK, float64(p.ResponseTimeMs))
				}
			}
		}
	}

	type outRow struct {
		Tag        string  `json:"tag"`
		Points     int     `json:"points"`
		ErrorRate  float64 `json:"error_rate"`
		AvgLatency float64 `json:"avg_latency_ms"`
	}

	out := make([]outRow, 0, len(acc))
	for _, a := range acc {
		avg := 0.0
		if a.Points > 0 {
			avg = a.Latency / float64(a.Points)
		}
		errorRate := 0.0
		if a.Points > 0 {
			errorRate = float64(a.Errors) / float64(a.Points) * 100
		}
		out = append(out, outRow{
			Tag:        a.Tag,
			Points:     a.Points,
			ErrorRate:  errorRate,
			AvgLatency: avg,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	c.JSON(http.StatusOK, gin.H{"groups": out, "total": len(out), "range": rangeStr})
}

// --- helpers ---

func rangeCutoff(r string) time.Time {
	now := time.Now()
	switch r {
	case "6h":
		return now.Add(-6 * time.Hour)
	case "24h":
		return now.Add(-24 * time.Hour)
	case "7d":
		return now.Add(-7 * 24 * time.Hour)
	default: // "1h"
		return now.Add(-1 * time.Hour)
	}
}

// computePercentilesFromDB computes P50/P95 from DB metric points.
func computePercentilesFromDB(pts []store.DBMetricPoint) (p50, p95 float64) {
	var lat []float64
	for _, p := range pts {
		if p.LatencyMs > 0 {
			lat = append(lat, p.LatencyMs)
		}
	}
	sort.Float64s(lat)
	return computeP50(lat), computeP95(lat)
}

// computeErrorRateFromDB computes error rate (0–100) from DB metric points.
func computeErrorRateFromDB(pts []store.DBMetricPoint) float64 {
	if len(pts) == 0 {
		return 0
	}
	var errors int
	for _, p := range pts {
		if !p.IsHealthy {
			errors++
		}
	}
	return float64(errors) / float64(len(pts)) * 100.0
}

// computePercentiles computes P50/P95 from in-memory metric points.
func computePercentiles(pts []store.MetricPoint) (p50, p95 float64) {
	var lat []float64
	for _, pt := range pts {
		if pt.ResponseTimeMs > 0 {
			lat = append(lat, float64(pt.ResponseTimeMs))
		}
	}
	sort.Float64s(lat)
	p50 = computeP50(lat)
	p95 = computeP95(lat)
	return
}

func computeP50(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Floor(0.50 * float64(len(sorted))))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func computeP95(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

func computeErrorRate(pts []store.MetricPoint) float64 {
	if len(pts) == 0 {
		return 0
	}
	var errors int
	for _, pt := range pts {
		if !pt.OK {
			errors++
		}
	}
	return float64(errors) / float64(len(pts)) * 100.0
}
