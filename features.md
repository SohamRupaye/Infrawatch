# Infrawatch — Feature Map

Maps every implemented feature to its source files. Use this as a reference when navigating the codebase.

---

## Engine — Core Health Polling

**What:** One goroutine per service continuously fires HTTP health checks.

| File | Role |
|---|---|
| `apps/engine/internal/health/poller.go` | Manages the goroutine pool. Spawns/stops pollers on hot config changes received from Redis. |
| `apps/engine/internal/health/checker.go` | Executes a single HTTP check against a service URL and returns the raw result. |
| `apps/engine/internal/health/state.go` | State enum and transition rules (`UNKNOWN`, `HEALTHY`, `DEGRADED`, `UNHEALTHY`, `DEAD`, `RECOVERING`). |
| `apps/engine/internal/health/state_manager.go` | Owns the per-service state machine. Applies checker results, decides when to trigger healing, and publishes `StateChangeEvent` and `MetricEvent` to Redis Streams. |

---

## Engine — Circuit Breaker

**What:** Opens automatically after N consecutive failures, skips health checks during the open window, and half-open probes after the timeout.

| File | Role |
|---|---|
| `apps/engine/internal/circuit/breaker.go` | Per-service circuit breaker logic (`CLOSED → OPEN → HALF_OPEN → CLOSED`). |
| `apps/engine/internal/circuit/registry.go` | Registry holding one breaker per service name; thread-safe snapshot API consumed by the API layer. |

Config keys: `circuit.failure_threshold`, `circuit.success_threshold`, `circuit.timeout`

---

## Engine — Anomaly Detection

**What:** Detects latency spikes and container memory growth independently from health state, emitting anomaly events on a channel.

| File | Role |
|---|---|
| `apps/engine/internal/anomaly/detector.go` | Facade wrapping both sub-detectors. Exposes `RecordLatency`, `RecordMemory`, and the `Anomalies()` channel. |
| `apps/engine/internal/anomaly/latency.go` | Maintains a per-service hourly P95 baseline. Fires an anomaly when the latest response time exceeds `baseline × latency_multiplier`. |
| `apps/engine/internal/anomaly/memory.go` | Tracks container memory samples (bytes). Fires an anomaly when growth rate exceeds `memory_growth_rate_mb` MB/min. |

Config keys: `anomaly.latency_multiplier`, `anomaly.memory_growth_rate_mb`, `anomaly.baseline_window`, `anomaly.evaluation_window`

---

## Engine — Self-Healing

**What:** On state degradation, the engine attempts configurable recovery actions before alerting.

| File | Role |
|---|---|
| `apps/engine/internal/healing/healer.go` | Dispatches the correct action(s) from `healing_actions`. Logs each attempt to the `healing_events` Redis Stream. |
| `apps/engine/internal/healing/docker.go` | `docker_restart` action — restarts the named container via the Docker socket. |
| `apps/engine/internal/healing/kubectl.go` | `kubectl_restart` action — rolls out a Kubernetes deployment restart. |
| `apps/engine/internal/healing/fallback.go` | `fallback` action — switches traffic to `fallback_url` and registers it in the API's active-fallback registry. |

Per-service config keys: `healing_actions`, `container_name`, `namespace`, `deployment`, `fallback_url`, `healing_webhook`

Global config keys: `healing.enabled`, `healing.max_restart_attempts`, `healing.restart_cooldown`

---

## Engine — Alerting

**What:** Fires configured notification channels when a service changes state. Deduplicates alerts per incident and optionally re-alerts on worsening severity.

| File | Role |
|---|---|
| `apps/engine/internal/alerts/dispatcher.go` | Decides which channels to fire based on `alerts.state_rules`. Implements deduplication (one alert on outage start, one on recovery) and escalation logic. |

Supported channels: **Slack** (webhook), **PagerDuty** (Events API v2), **Email** (SMTP), **Generic Webhook**

Config keys: `alerts.slack`, `alerts.pagerduty`, `alerts.email`, `alerts.webhook`, `alerts.state_rules`, `alerts.escalate_on_worsening`

---

## Engine — Persistence

**What:** Writes all observable events to TimescaleDB for long-term history and dashboard cold-start seeding.

| File | Role |
|---|---|
| `apps/engine/internal/storage/db.go` | Writes to four hypertables: `metrics`, `state_changes`, `incidents`, `alert_history`. |

Tables:

| Table | Contents |
|---|---|
| `metrics` | Per-check latency, status code, and OK flag with timestamp. |
| `state_changes` | Every `from → to` state transition with reason string. |
| `incidents` | Computed incident records with open/resolved status. |
| `alert_history` | Every dispatched alert with channel, severity, and ack status. |

---

## Engine — Event Bus

**What:** Redis Streams used as the pub/sub backbone between Engine and API.

| File | Role |
|---|---|
| `apps/engine/pkg/enginepkg.go` | Defines stream names, event structs (`StateChangeEvent`, `MetricEvent`, `HealingEvent`), and `Bus` helpers for publish/subscribe. |

Streams:

| Stream | Publisher | Subscribers |
|---|---|---|
| `state_change_events` | Engine state manager | API state store, API WebSocket broadcaster |
| `metric_events` | Engine state manager | API state store |
| `healing_events` | Engine healer | API healing history endpoint |
| `service_config_commands` | API config handler | Engine poller manager |

---

## Engine — Hot Config Reload

**What:** The Engine subscribes to `service_config_commands`. When the API creates, updates, or deletes a service, the Engine adds or removes the corresponding goroutine with no process restart.

| File | Role |
|---|---|
| `apps/engine/cmd/main.go` | Subscribes to `service_config_commands` and calls `poller.Apply()`. |
| `apps/engine/internal/health/poller.go` | `Apply(services []ServiceConfig)` diffs the new config against the running set and starts/stops goroutines accordingly. |

---

## API — State Store (Read Model)

**What:** In-memory cache of `ServiceView` per service, rebuilt from Redis Streams. Seeded from TimescaleDB on startup when available.

| File | Role |
|---|---|
| `apps/api/store/state_store.go` | Subscribes to `state_change_events` and `metric_events`. Exposes `AllServices()`, `GetService()`, `MetricsFor()`. |
| `apps/api/store/db_reader.go` | Read-only TimescaleDB queries for metrics, incidents, state history, and alert history. Used by handlers for range queries. |

---

## API — REST Endpoints

| File | Endpoints |
|---|---|
| `apps/api/handlers/services.go` | `GET /api/v1/services`, `GET /api/v1/services/:name`, `GET /api/v1/tags` |
| `apps/api/handlers/config_services.go` | `GET/POST/PUT/DELETE /api/v1/config/services[/:name]` |
| `apps/api/handlers/metrics.go` | `GET /api/v1/metrics/:service`, `/metrics/:service/baseline`, `/metrics/grouped/by-tag` |
| `apps/api/handlers/circuit.go` | `GET /api/v1/circuit`, `/circuit/:service`, `POST /circuit/:service/reset` |
| `apps/api/handlers/incidents.go` | `GET /api/v1/incidents`, `/incidents/:id`, `/incidents/:id/export`, `/incidents/grouped/by-tag` |
| `apps/api/handlers/alerts.go` | `GET /api/v1/alerts/history`, `POST /alerts/:id/ack` |
| `apps/api/handlers/heal.go` | `GET /api/v1/healing`, `POST /api/v1/services/:name/heal` |
| `apps/api/handlers/status.go` | `GET /status` (HTML), `GET /api/public/status` (JSON) |
| `apps/api/handlers/logs.go` | `GET /api/v1/logs/:container` (HTTP stream), `GET /ws/logs/:container` (WebSocket) |

---

## API — WebSocket

**What:** Pushes live `StateChangeEvent` and `MetricEvent` messages to connected browser clients.

| File | Role |
|---|---|
| `apps/api/websocket/hub.go` | Fan-out hub managing all WebSocket connections. |
| `apps/api/websocket/vroadcaster.go` | Subscribes to Redis Streams and forwards events to the hub. |

Routes: `GET /ws`, `GET /ws/logs/:container`

---

## API — Dynamic Config (Hot-Reload Write Path)

**What:** REST CRUD for service configuration. Writes to `infrawatch.yaml` and publishes `service_config_commands` so the Engine reacts immediately.

| File | Role |
|---|---|
| `apps/api/handlers/config_services.go` | Validates input, merges changes into the YAML file on disk, and publishes a reload command to Redis. |

---

## API — Public Status Page

**What:** Unauthenticated HTML page and JSON endpoint showing current per-service state, latency, tags, and 24-hour uptime.

| File | Role |
|---|---|
| `apps/api/handlers/status.go` | Renders the HTML status page and its JSON backing route. Uses TimescaleDB for uptime when available; falls back to in-memory state. |

Routes: `GET /status`, `GET /api/public/status`

---

## API — Docker Log Streaming

**What:** Streams Docker container logs over HTTP (snapshot) and WebSocket (live tail) by connecting to the Docker socket.

| File | Role |
|---|---|
| `apps/api/handlers/logs.go` | Opens the Docker socket, reads container logs, strips the multiplexing header, and streams to the client. |

Routes: `GET /api/v1/logs/:container?tail=500`, `GET /ws/logs/:container`

---

## Frontend — Dashboard

**What:** Real-time node-graph built on `@xyflow/react`. Services are draggable nodes; dependency edges are drawn automatically from config.

| File | Role |
|---|---|
| `apps/frontend/src/components/Dashboard.tsx` | Root component. Polls `/api/v1/config/services` and `/api/v1/services` every 2 seconds. Computes DAG layout, maps dependency edges, and wires up CRUD actions. |
| `apps/frontend/src/components/ServiceNode.tsx` | Individual service card showing name, state, response time, and tags. Colour-coded by state. |
| `apps/frontend/src/components/EdgeWithStatus.tsx` | Animated dependency edge coloured by the upstream service state. |
| `apps/frontend/src/components/ServicePanel.tsx` | Side panel shown on node click. Displays health overview, metrics, config, and Edit/Remove actions. |
| `apps/frontend/src/components/AddEditServiceModal.tsx` | Modal form for creating or editing a service config via the REST API. |
| `apps/frontend/src/services/api.ts` | Axios client targeting `/api/v1`. Handles PUT-then-POST upsert for service config. |
| `apps/frontend/src/types/index.ts` | TypeScript interfaces for `ServiceConfig`, `ServiceState`, `ConfigResponse`, `StateResponse`. |

State colours:

| State | Colour |
|---|---|
| `HEALTHY` | Green |
| `DEGRADED` | Yellow |
| `UNHEALTHY` | Orange |
| `DEAD` | Red |
| `RECOVERING` | Blue |
| `UNKNOWN` | Grey |

---

## Demo Environment

**What:** `make demo` spins up 6 Docker containers including a simulated Node.js service that exposes breakable health endpoints.

| File | Role |
|---|---|
| `test/infrawatch.demo.yaml` | Demo config with 3 services (`auth-service`, `db-service`, `api-gateway`) pointing at the demo-services container. |
| `test/demo-services.js` | Node.js HTTP server on port 4000. Exposes `/health/:svc`, `/break/:svc`, and `/heal/:svc` endpoints. |
| `test/docker-compose.demo.yml` | Compose override that mounts the demo config and adds the `demo-services` container. |
| `docker/docker-compose.yml` | Base production compose (redis, timescaledb, engine, api, frontend). |
| `Makefile` | `demo`, `demo-down`, `prod-up`, `prod-down`, `clean` targets. |
