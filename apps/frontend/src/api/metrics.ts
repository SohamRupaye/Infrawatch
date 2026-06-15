import { api } from './client'
import type { MetricsResponse } from './types'

export type MetricRange = '1h' | '6h' | '24h' | '7d'

export const fetchMetrics = (service: string, range: MetricRange = '1h') =>
  api.get<MetricsResponse>(
    `/api/v1/metrics/${encodeURIComponent(service)}?range=${range}`,
  )

export const fetchBaseline = (service: string) =>
  api.get<{ service: string; p95_ms: number; sample_count: number }>(
    `/api/v1/metrics/${encodeURIComponent(service)}/baseline`,
  )
