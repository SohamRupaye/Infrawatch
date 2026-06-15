import { api } from './client'
import type { ServiceView, TagSummary } from './types'

export interface ServicesResponse {
  services: ServiceView[]
  total: number
}

export const fetchServices = (tag?: string) =>
  api.get<ServicesResponse>(
    `/api/v1/services${tag ? `?tag=${encodeURIComponent(tag)}` : ''}`,
  )

export const fetchService = (name: string) =>
  api.get<ServiceView>(`/api/v1/services/${encodeURIComponent(name)}`)

export const fetchTags = () =>
  api.get<{ tags: TagSummary[] }>('/api/v1/tags')
