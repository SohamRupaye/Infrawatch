# InfraWatch System Architecture

## Components
- Engine (`apps/engine`): health polling, FSM transitions, alerting, healing, persistent writes.
- API (`apps/api`): read-model cache, query APIs, config mutation APIs, status page.
- Redis Streams: low-latency event bus between engine and API.
- TimescaleDB: long-term storage for:
  - `metrics`
  - `state_changes`
  - `incidents`
  - `alert_history`

## Runtime Configuration Flow (No Restart)
1. Client calls API config endpoint (`/api/v1/config/services` CRUD).
2. API validates and atomically writes updated `infrawatch.yaml`.
3. API publishes `infrawatch:service_config_commands` stream event.
4. Engine subscriber applies command immediately:
   - `upsert`: starts/restarts service poll loop.
   - `delete`: stops loop and removes service from active set.

## Alert Pipeline
1. Engine state transition triggers `Dispatcher.Dispatch`.
2. Dispatcher deduplicates per service outage lifecycle:
   - first bad-state alert only
   - first recovery alert only
   - optional worsening escalation if enabled.
3. Each channel send result is persisted to `alert_history`.
4. API exposes history and acknowledgement endpoints.

## Tags / Grouping
- Tags are sourced from YAML service config.
- API merges tags into service responses and supports:
  - service filtering by tag
  - incidents grouped by tag
  - metrics grouped by tag

## Public Status Surface
- `/status`: unauthenticated HTML status page.
- `/api/public/status`: backing JSON endpoint.
- Shows current state + latency + tags + 24h uptime (DB-backed when available, in-memory fallback otherwise).
