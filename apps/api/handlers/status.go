package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

type StatusHandler struct{ deps Deps }

func NewStatusHandler(deps Deps) *StatusHandler { return &StatusHandler{deps: deps} }

type publicStatusService struct {
	Name      string   `json:"name"`
	State     string   `json:"state"`
	Uptime24h float64  `json:"uptime_24h_pct"`
	LatencyMs int64    `json:"latency_ms"`
	Tags      []string `json:"tags,omitempty"`
}

// Data godoc
// GET /api/public/status
func (h *StatusHandler) Data(c *gin.Context) {
	services := h.deps.Store.AllServices()
	uptimeByService := make(map[string]float64)

	if h.deps.DB != nil {
		if up, err := h.deps.DB.QueryUptimeByService(c.Request.Context(), time.Now().Add(-24*time.Hour)); err == nil {
			uptimeByService = up
		}
	}

	tagsByService := map[string][]string{}
	if cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath); err == nil {
		tagsByService = serviceTagsMap(cfg.Services)
	}

	out := make([]publicStatusService, 0, len(services))
	overall := "UP"
	for _, svc := range services {
		uptime := svc.UptimePct
		if v, ok := uptimeByService[svc.Name]; ok {
			uptime = v
		}
		out = append(out, publicStatusService{
			Name:      svc.Name,
			State:     svc.State,
			Uptime24h: uptime,
			LatencyMs: svc.ResponseTimeMs,
			Tags:      tagsByService[svc.Name],
		})

		switch svc.State {
		case "DEAD", "UNHEALTHY":
			overall = "DOWN"
		case "DEGRADED", "RECOVERING":
			if overall == "UP" {
				overall = "DEGRADED"
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.JSON(http.StatusOK, gin.H{
		"generated_at": time.Now().UTC(),
		"overall":      overall,
		"services":     out,
		"total":        len(out),
	})
}

// Page godoc
// GET /status
func (h *StatusHandler) Page(c *gin.Context) {
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>InfraWatch Status</title>
  <style>
    :root { --bg: #f3f6fb; --card: #ffffff; --text: #152238; --muted: #5d6b82; --ok: #0f9d58; --warn: #f4a62a; --bad: #d93025; --border: #dbe4f3; }
    body { margin: 0; font-family: "IBM Plex Sans", -apple-system, Segoe UI, sans-serif; background: radial-gradient(circle at top, #e9f4ff, var(--bg)); color: var(--text); }
    .wrap { max-width: 960px; margin: 0 auto; padding: 28px 16px 40px; }
    .title { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 16px; }
    .badge { padding: 6px 10px; border-radius: 999px; font-size: 12px; font-weight: 700; letter-spacing: .02em; text-transform: uppercase; }
    .badge.UP { background: #d8f5e6; color: #0a6a3c; }
    .badge.DEGRADED { background: #fff2d8; color: #8a5a00; }
    .badge.DOWN { background: #ffe3e0; color: #8f1d16; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; overflow: hidden; box-shadow: 0 10px 20px rgba(12, 35, 64, .05); }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 12px 14px; font-size: 14px; border-bottom: 1px solid var(--border); }
    th { color: var(--muted); font-weight: 600; font-size: 12px; text-transform: uppercase; letter-spacing: .03em; }
    tr:last-child td { border-bottom: none; }
    .state { font-weight: 700; }
    .HEALTHY { color: var(--ok); }
    .RECOVERING, .DEGRADED { color: var(--warn); }
    .UNHEALTHY, .DEAD { color: var(--bad); }
    .muted { color: var(--muted); font-size: 13px; margin-top: 10px; }
    .tags { color: var(--muted); }
  </style>
</head>
<body>
  <main class="wrap">
    <div class="title">
      <h1>InfraWatch Status</h1>
      <span id="overall" class="badge">...</span>
    </div>
    <div class="card">
      <table>
        <thead><tr><th>Service</th><th>State</th><th>Uptime (24h)</th><th>Latency</th><th>Tags</th></tr></thead>
        <tbody id="rows"></tbody>
      </table>
    </div>
    <div id="meta" class="muted"></div>
  </main>
  <script>
    async function loadStatus() {
      const res = await fetch('/api/public/status', {cache: 'no-store'});
      const data = await res.json();
      const overall = document.getElementById('overall');
      overall.textContent = data.overall;
      overall.className = 'badge ' + data.overall;

      const rows = document.getElementById('rows');
      rows.innerHTML = '';
      for (const s of data.services) {
        const tr = document.createElement('tr');
        tr.innerHTML = '<td>' + s.name + '</td>' +
          '<td class="state ' + s.state + '">' + s.state + '</td>' +
          '<td>' + s.uptime_24h_pct.toFixed(2) + '%</td>' +
          '<td>' + s.latency_ms + 'ms</td>' +
          '<td class="tags">' + (s.tags || []).join(', ') + '</td>';
        rows.appendChild(tr);
      }
      document.getElementById('meta').textContent = 'Last updated: ' + new Date(data.generated_at).toLocaleString();
    }
    loadStatus();
    setInterval(loadStatus, 30000);
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}
