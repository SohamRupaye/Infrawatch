import { api } from './client'
import type { AlertHistory } from './types'

export interface AlertsQuery {
  service?: string
  channel?: string
  unacked?: boolean
  limit?: number
}

export const fetchAlerts = (q: AlertsQuery = {}) => {
  const params = new URLSearchParams()
  if (q.service) params.set('service', q.service)
  if (q.channel) params.set('channel', q.channel)
  if (q.unacked) params.set('unacked', 'true')
  if (q.limit != null) params.set('limit', String(q.limit))
  const qs = params.toString()
  return api.get<{ alerts: AlertHistory[]; total: number }>(
    `/api/v1/alerts/history${qs ? `?${qs}` : ''}`,
  )
}

export const acknowledgeAlert = (id: number, acknowledgedBy = 'manual') =>
  api.post<{ success: boolean }>(`/api/v1/alerts/${id}/ack`, {
    acknowledged_by: acknowledgedBy,
  })
