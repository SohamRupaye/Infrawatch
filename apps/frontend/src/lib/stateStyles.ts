import type { ServiceState } from '@/api/types'

export const STATE_DOT: Record<ServiceState, string> = {
  HEALTHY:   'bg-emerald-500',
  DEGRADED:  'bg-yellow-500',
  UNHEALTHY: 'bg-orange-500',
  DEAD:      'bg-red-500',
  RECOVERING:'bg-blue-500',
  UNKNOWN:   'bg-gray-400',
}
