import { api } from './client'
import type { ServiceConfig, ServiceConfigInput } from './types'

export const fetchConfigServices = () =>
  api.get<{ services: ServiceConfig[]; total: number }>('/api/v1/config/services')

export const createConfigService = (data: ServiceConfigInput) =>
  api.post<ServiceConfig>('/api/v1/config/services', data)

export const updateConfigService = (name: string, data: ServiceConfigInput) =>
  api.put<ServiceConfig>(
    `/api/v1/config/services/${encodeURIComponent(name)}`,
    data,
  )

export const deleteConfigService = (name: string) =>
  api.delete<void>(`/api/v1/config/services/${encodeURIComponent(name)}`)
