# Infrawatch

A self-hosted infrastructure monitoring and auto-healing platform built in Go and React. Infrawatch continuously health-checks your services, detects failures, triggers configurable recovery actions, and surfaces everything on a real-time node-graph dashboard — no page reload required.

---

## Architecture

Infrawatch is a decoupled monorepo split into three binaries and two backing stores.

```
┌──────────────────────────────────────────────────┐
│                  Browser                         │
│            apps/frontend (React + Vite)          │
│        polls /api/v1   ·   /ws (WebSocket)       │
└─────────────────────┬────────────────────────────┘
                      │ HTTP / WS
┌─────────────────────▼────────────────────────────┐
│               apps/api (Go / Gin)                │
│  REST + WebSocket gateway · in-memory read model │
│           reads Redis Streams                    │
└──────────┬──────────────────────┬────────────────┘
           │ reads                │ writes (config cmds)
┌──────────▼───────────┐  ┌──────▼──────────────────┐
│  TimescaleDB         │  │  Redis Streams           │
│  metrics             │  │  state_change_events     │
│  state_changes       │  │  metric_events           │
│  incidents           │  │  healing_events          │
│  alert_history       │  │  service_config_commands │
└──────────────────────┘  └──────────┬───────────────┘
                                     │ subscribes
                          ┌──────────▼───────────────┐
                          │   apps/engine (Go)        │
                          │  health pollers           │
                          │  state machine            │
                          │  circuit breakers         │
                          │  anomaly detection        │
                          │  self-healing             │
                          │  alert dispatcher         │
                          └───────────────────────────┘
```

| Component | Role |
|---|---|
| **Engine** (`apps/engine`) | Authoritative runtime. Runs one goroutine per service, evaluates state transitions, triggers healing actions, publishes events to Redis Streams, and persists history to TimescaleDB. |
| **API** (`apps/api`) | Read-model gateway. Subscribes to Redis Streams, maintains an in-memory service state cache, and serves REST + WebSocket endpoints. Falls back to TimescaleDB for historical queries. |
| **Frontend** (`apps/frontend`) | Vite + React SPA using `@xyflow/react`. Shows services as draggable nodes with live health state, dependency edges, and a detail panel. |
| **Redis Streams** | Low-latency pub/sub bus between Engine and API. |
| **TimescaleDB** | Long-term storage for metrics, incidents, state transitions, and alert history. Optional — API degrades gracefully to Redis-only mode. |

---

## How It Works

1. **Polling** — The Engine spawns one goroutine per service. Each poller fires an HTTP request on the configured `interval`.
2. **State machine** — Each service transitions through `UNKNOWN → HEALTHY → DEGRADED → UNHEALTHY → DEAD → RECOVERING`. Transitions are published to the `state_change_events` Redis Stream.
3. **Circuit breaker** — After `failure_threshold` consecutive failures the circuit opens, skipping health checks until the `timeout` half-open probe succeeds.
4. **Anomaly detection** — Latency spikes (P95 multiplier) are detected independently and emitted as anomaly events.
5. **Self-healing** — On state degradation the Engine runs the configured `healing_actions` (`docker_restart`, `kubectl_restart`, `fallback`, `webhook`) and logs the result to the `healing_events` stream.
6. **Alerting** — The alert dispatcher fires configured channels (Slack, PagerDuty, Email, generic Webhook) once per outage onset and once on recovery. Optional `escalate_on_worsening` re-alerts on severity increases.
7. **API read model** — The API subscribes to all Redis Streams and maintains an in-memory `ServiceView` per service, making REST reads O(1).
8. **Hot config reload** — Creating / updating / deleting a service via the REST API writes the change to `infrawatch.yaml` and publishes a command to the `service_config_commands` stream. The Engine picks it up and updates its poller set without restarting.
9. **Frontend** — The dashboard polls `/api/v1/services` and `/api/v1/config/services` every 2 seconds, repaints node colours, and redraws dependency edges in real time.

---

## Quick Start (Demo)

The fastest way to see Infrawatch in action with three simulated services:

```bash
git clone https://github.com/SohamRupaye/infrawatch.git
cd infrawatch
make demo
```

Open **http://localhost:3000** — you should see three nodes (`auth-service`, `db-service`, `api-gateway`).

Break a service and watch it go red:
```bash
curl http://localhost:4000/break/auth
```

Heal it manually:
```bash
curl http://localhost:4000/heal/auth
```

Stop the demo:
```bash
make demo-down
```

---

## Development Setup

### Prerequisites

| Tool | Version |
|---|---|
| Go | 1.21+ |
| Bun | 1.x |
| Docker + Compose | any recent |
| Redis | 7+ |
| TimescaleDB | PostgreSQL 15+ (optional) |

### 1 — Start backing services

```bash
docker compose -f docker/docker-compose.dev.yml up -d redis timescaledb
```

### 2 — Copy and edit the config

```bash
cp infrawatch.example.yaml infrawatch.yaml
# edit infrawatch.yaml — add your services, Redis addr, etc.
```

### 3 — Run the Engine

```bash
INFRAWATCH_CONFIG=infrawatch.yaml go run ./apps/engine/cmd/...
```

### 4 — Run the API

```bash
INFRAWATCH_CONFIG=infrawatch.yaml go run ./apps/api/cmd/...
# API listens on :8080
```

### 5 — Run the Frontend

```bash
cd apps/frontend
bun install
bun run dev
# Frontend on http://localhost:3000, proxies /api → localhost:8080
```

---

## Production Deployment

### Docker Compose (recommended)

```bash
# copy the example config and customise
cp infrawatch.example.yaml docker/engine.yaml

# set secrets via env vars
export POSTGRES_PASSWORD=strongpassword
export JWT_SECRET=yoursecret

make prod-up      # builds and starts all containers
make prod-down    # stops everything
```

The frontend is served by Nginx on **port 3000** and proxies `/api/` to the API container on port 8080.

### Bare metal

```bash
# build binaries
CGO_ENABLED=0 go build -o bin/infrawatch-engine ./apps/engine/cmd/...
CGO_ENABLED=0 go build -o bin/infrawatch-api    ./apps/api/cmd/...

# build frontend
cd apps/frontend && bun run build   # outputs to dist/
```

Run both binaries as `systemd` services. Configure Nginx to:
- Serve `apps/frontend/dist` at `/`
- Proxy `/api/` → `http://127.0.0.1:8080/api/`
- Proxy `/ws` → `http://127.0.0.1:8080/ws` (WebSocket upgrade)

### Environment variables

All secrets can be injected via env vars instead of hardcoding in YAML:

```yaml
redis:
  addr: "${REDIS_HOST}:6379"
  password: "${REDIS_PASSWORD}"
```

| Variable | Default | Description |
|---|---|---|
| `INFRAWATCH_CONFIG` | `infrawatch.yaml` | Path to the config file |
| `REDIS_ADDR` | from config | Override Redis address |
| `REDIS_PASSWORD` | `""` | Override Redis password |
| `DATABASE_URL` | from config | Override TimescaleDB DSN |
| `JWT_SECRET` | `""` | Enable JWT auth on the API |
| `POSTGRES_PASSWORD` | `infrawatch_secret` | TimescaleDB password (compose) |

---

## Configuration Reference

```yaml
redis:
  addr: "localhost:6379"
  password: ""

api:
  addr: ":8080"
  allow_origins: ["*"]      # CORS origins
  jwt_secret: ""            # set to enable JWT auth

circuit:
  failure_threshold: 3      # failures before circuit opens
  success_threshold: 1      # successes to close circuit
  timeout: 30s              # half-open probe delay

anomaly:
  latency_multiplier: 2.0   # P95 × multiplier = anomaly threshold

alerts:
  escalate_on_worsening: true
  slack:
    enabled: true
    webhook_url: "https://hooks.slack.com/..."
  pagerduty:
    enabled: false
    integration_key: "..."
  email:
    enabled: false
    smtp_host: "smtp.example.com"
    smtp_port: 587
    username: "..."
    password: "..."
    from: "alerts@example.com"
    recipients: ["on-call@example.com"]

healing:
  enabled: true
  max_restart_attempts: 3
  restart_cooldown: 60s

docker:
  socket_path: "/var/run/docker.sock"

storage:
  dsn: "postgres://infrawatch:secret@localhost:5432/infrawatch?sslmode=disable"
  max_open_conns: 10
  max_idle_conns: 5

# Drives mode: passive services (below) from Alertmanager alerts instead of
# Infrawatch's own HTTP polling — see "Alertmanager Integration" below.
alertmanager:
  webhook_secret: "..."

services:
  - name: "my-api"
    url: "https://my-api.example.com/health"
    interval: 15s
    timeout: 5s
    method: GET
    expect_status: 200
    tags: ["core", "api"]
    dependencies: ["my-db"]
    healing_actions: ["docker_restart", "webhook"]
    container_name: "my-api-container"
    healing_webhook: "https://hooks.example.com/restart"

  - name: "payments-worker"      # no url — driven by Alertmanager instead
    mode: passive
    healing_actions: ["kubectl_restart"]
    namespace: "production"
    deployment: "payments-worker"
```

---

## Alertmanager Integration

Infrawatch's own HTTP poller works well when you don't already run a metrics
stack, but if you run Prometheus + Alertmanager, Infrawatch is meant to sit
**on top of** that instead of duplicating it: Alertmanager already knows
your service is unhealthy, Infrawatch's job is the part Alertmanager doesn't
do — a circuit breaker to avoid restart storms, self-healing actions, alert
escalation, and an audit trail of what was tried and whether it worked.

1. Mark a service `mode: passive` and omit its `url` — the engine still runs
   its normal per-service loop (circuit breaker, healing, alerting), it just
   never issues an HTTP check itself.
2. Set `alertmanager.webhook_secret` and configure Alertmanager to POST to
   `/api/v1/webhooks/alertmanager` with that secret in an `X-Webhook-Secret`
   header:
   ```yaml
   receivers:
     - name: infrawatch
       webhook_configs:
         - url: http://infrawatch-api:8080/api/v1/webhooks/alertmanager
           http_config:
             headers:
               X-Webhook-Secret: "${ALERTMANAGER_WEBHOOK_SECRET}"
   ```
3. Add an `infrawatch_service` label to the Prometheus alerts you want
   mapped to a passive service:
   ```yaml
   - alert: PaymentsWorkerDown
     labels:
       infrawatch_service: payments-worker
   ```

A `resolved` alert doesn't force the service straight back to `HEALTHY` — it
clears the external signal and lets the normal state machine ratchet back up
over the next few ticks, exactly like a real recovering HTTP check would.
Alerts for unknown services or services not in `mode: passive` are accepted
by the endpoint but skipped (visible in the response body), never silently
dropped.

---

## API Reference

All endpoints are under `/api/v1`. Authentication (JWT Bearer) is optional — enable by setting `jwt_secret` in config.

### Services

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/services` | All services with current health state. Supports `?tag=<tag>` filter. |
| `GET` | `/api/v1/services/:name` | Single service state. |
| `POST` | `/api/v1/services/:name/heal` | Queue a manual heal command for the engine. |

### Service Configuration (hot-reload)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/config/services` | Read active service configuration from YAML. |
| `POST` | `/api/v1/config/services` | Add a new service (engine picks it up instantly). |
| `PUT` | `/api/v1/config/services/:name` | Update an existing service config. |
| `DELETE` | `/api/v1/config/services/:name` | Remove a service from monitoring. |

### Metrics

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/metrics/:service` | Time-series metrics with P50, P95, error rate. Supports `?range=1h\|6h\|24h\|7d`. |
| `GET` | `/api/v1/metrics/:service/baseline` | Hourly P95 latency baseline. |
| `GET` | `/api/v1/metrics/grouped/by-tag` | Aggregated error rate and avg latency per tag. |

### Circuit Breakers

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/circuit` | State of all circuit breakers. |
| `GET` | `/api/v1/circuit/:service` | Circuit breaker state for one service. |
| `POST` | `/api/v1/circuit/:service/reset` | Manually reset a circuit breaker. |

### Incidents

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/incidents` | List all incidents. Supports `?service=<name>` and `?tag=<tag>` filters. |
| `GET` | `/api/v1/incidents/:id` | Single incident with full state-change timeline. |
| `GET` | `/api/v1/incidents/:id/export` | Export incident as JSON or Markdown (`?format=markdown`). |
| `GET` | `/api/v1/incidents/grouped/by-tag` | Incident counts grouped by service tag. |

### Alerts

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/alerts/history` | History of all dispatched alerts (TimescaleDB-backed). |
| `POST` | `/api/v1/alerts/:id/ack` | Acknowledge an alert. |

### Healing

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/healing` | Last N healing events from the healing stream. Supports `?limit=50`. |

### Tags

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/tags` | All tags with service counts. |

### Public Status Page

| Method | Path | Description |
|---|---|---|
| `GET` | `/status` | HTML status page (no auth required). |
| `GET` | `/api/public/status` | JSON backing data for the status page. |

### WebSocket

| Path | Description |
|---|---|
| `/ws` | Real-time state change and metric events pushed to the browser. |
| `/ws/logs/:container` | Live Docker container log stream over WebSocket. |

### Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | API liveness check. Returns storage connection status. |

---

## Makefile

```bash
make demo        # start full demo environment (6 containers)
make demo-down   # stop demo
make prod-up     # start production environment
make prod-down   # stop production
make clean       # remove all containers and volumes
```
