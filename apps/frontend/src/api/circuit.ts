import { api } from './client'
import type { BreakerSnapshot } from './types'

export const fetchCircuits = () =>
  api.get<{ circuits: Record<string, BreakerSnapshot>; total: number }>(
    '/api/v1/circuit',
  )

export const fetchCircuit = (service: string) =>
  api.get<BreakerSnapshot>(`/api/v1/circuit/${encodeURIComponent(service)}`)

export const resetCircuit = (service: string) =>
  api.post<{ service: string; message: string }>(
    `/api/v1/circuit/${encodeURIComponent(service)}/reset`,
  )
