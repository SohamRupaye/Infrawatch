import { api } from './client'
import type { HealingRecord } from './types'

export const fetchHealingHistory = () =>
  api.get<{ events: HealingRecord[] }>('/api/v1/healing')

export const triggerHeal = (service: string) =>
  api.post<{ service: string; message: string }>(
    `/api/v1/services/${encodeURIComponent(service)}/heal`,
  )
