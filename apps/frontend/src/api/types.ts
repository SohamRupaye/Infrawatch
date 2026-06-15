// ── State enums ───────────────────────────────────────────────────────────────

export type ServiceState =
  | 'UNKNOWN'
  | 'HEALTHY'
  | 'DEGRADED'
  | 'UNHEALTHY'
  | 'DEAD'
  | 'RECOVERING'

export type BreakerState = 'CLOSED' | 'OPEN' | 'HALF_OPEN'

// ── Core read models ──────────────────────────────────────────────────────────

export interface BreakerSnapshot {
  state: BreakerState
  consecutive_fails: number
  opened_at?: string
  last_transition: string
}

export interface ServiceView {
  name: string
  tags?: string[]
  state: ServiceState
  previous_state: ServiceState
  last_checked: string
  last_healthy: string
  response_time_ms: number
  error_message?: string
  consecutive_fails: number
  uptime_pct: number
  circuit: BreakerSnapshot
  fallback_url?: string
}

export interface TagSummary {
  tag: string
  count: number
}

// ── Metrics ───────────────────────────────────────────────────────────────────

/** In-memory ring-buffer metric point. */
export interface MetricPoint {
  timestamp: string
  ok: boolean
  response_time_ms: number
  status_code: number
  state: ServiceState
}

/** TimescaleDB metric row. */
export interface DBMetricPoint {
  timestamp: string
  status_code: number
  latency_ms: number
  is_healthy: boolean
  error?: string
}

export interface MetricsResponse {
  service: string
  range: string
  total: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  source: 'timescaledb' | 'in-memory'
  points: (MetricPoint | DBMetricPoint)[]
}

// ── Incidents ─────────────────────────────────────────────────────────────────

export interface IncidentEvent {
  timestamp: string
  from: string
  to: string
  reason: string
}

export interface Incident {
  id: string
  service_name: string
  started_at: string
  resolved_at?: string
  duration_sec?: number
  duration_ms?: number
  open: boolean
  timeline: IncidentEvent[]
  summary: string
  root_state?: string
}

export interface IncidentsResponse {
  incidents: Incident[]
  total: number
  has_more: boolean
  limit: number
  offset: number
}

// ── Alerts ────────────────────────────────────────────────────────────────────

export interface AlertHistory {
  id: number
  created_at: string
  service_name: string
  state: ServiceState
  previous_state: ServiceState
  message: string
  response_time_ms: number
  channel: string
  delivered: boolean
  error?: string
  acknowledged: boolean
  acknowledged_at?: string
  acknowledged_by?: string
}

// ── Config ────────────────────────────────────────────────────────────────────

/** Shape returned by GET /api/v1/config/services — Go time.Duration is nanoseconds. */
export interface ServiceConfig {
  name: string
  url: string
  interval: number
  timeout: number
  method?: string
  headers?: Record<string, string>
  expect_status?: number
  expect_body?: string
  tags?: string[]
  dependencies?: string[]
  container_name?: string
  namespace?: string
  deployment?: string
  healing_actions?: string[]
  fallback_url?: string
  healing_webhook?: string
}

/** Payload sent to POST/PUT endpoints — uses human-readable duration strings. */
export interface ServiceConfigInput {
  name: string
  url: string
  interval?: string
  timeout?: string
  method?: string
  headers?: Record<string, string>
  expect_status?: number
  expect_body?: string
  tags?: string[]
  dependencies?: string[]
  container_name?: string
  namespace?: string
  deployment?: string
  healing_actions?: string[]
  fallback_url?: string
  healing_webhook?: string
}

// ── Healing ───────────────────────────────────────────────────────────────────

export interface HealingRecord {
  service_name: string
  action: string
  success: boolean
  error?: string
  timestamp: string
}

// ── Public status ─────────────────────────────────────────────────────────────

export interface PublicStatusService {
  name: string
  state: ServiceState
  uptime_pct: number
  tags?: string[]
}

export interface PublicStatus {
  overall: string
  services: PublicStatusService[]
  updated_at: string
}

// ── WebSocket events ──────────────────────────────────────────────────────────

export interface MetricEvent {
  service_name: string
  timestamp: string
  ok: boolean
  response_time_ms: number
  status_code: number
  state: ServiceState
}

export interface StateChangeEvent {
  service_name: string
  previous_state: ServiceState
  new_state: ServiceState
  reason: string
  timestamp: string
}

export interface HealingEvent {
  service_name: string
  action: string
  success: boolean
  error?: string
  timestamp: string
}

export interface AnomalyEvent {
  service_name: string
  type: string
  message: string
  value: number
  baseline: number
  timestamp: string
}

export type WSEvent =
  | { type: 'metric'; payload: MetricEvent }
  | { type: 'state_change'; payload: StateChangeEvent }
  | { type: 'healing'; payload: HealingEvent }
  | { type: 'anomaly'; payload: AnomalyEvent }
