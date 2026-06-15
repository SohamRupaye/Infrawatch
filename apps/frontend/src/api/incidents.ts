import { api } from './client'
import type { IncidentsResponse, Incident } from './types'

export interface IncidentsQuery {
  service?: string
  tag?: string
  limit?: number
  offset?: number
}

export const fetchIncidents = (q: IncidentsQuery = {}) => {
  const params = new URLSearchParams()
  if (q.service) params.set('service', q.service)
  if (q.tag) params.set('tag', q.tag)
  if (q.limit != null) params.set('limit', String(q.limit))
  if (q.offset != null) params.set('offset', String(q.offset))
  const qs = params.toString()
  return api.get<IncidentsResponse>(`/api/v1/incidents${qs ? `?${qs}` : ''}`)
}

export const fetchIncident = (id: string) =>
  api.get<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}`)

export const fetchIncidentsGroupedByTag = () =>
  api.get<{ groups: Record<string, Incident[]> }>('/api/v1/incidents/grouped/by-tag')
