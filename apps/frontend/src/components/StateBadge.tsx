import { cn } from '@/lib/utils'
import { STATE_DOT } from '@/lib/stateStyles'
import type { ServiceState } from '@/api/types'

const STATE_STYLES: Record<ServiceState, string> = {
  HEALTHY:   'bg-emerald-50 text-emerald-700 border-emerald-200',
  DEGRADED:  'bg-yellow-50 text-yellow-700 border-yellow-200',
  UNHEALTHY: 'bg-orange-50 text-orange-700 border-orange-200',
  DEAD:      'bg-red-50 text-red-700 border-red-200',
  RECOVERING:'bg-blue-50 text-blue-700 border-blue-200',
  UNKNOWN:   'bg-gray-100 text-gray-500 border-gray-200',
}

interface Props {
  state: ServiceState
  pulse?: boolean
  className?: string
}

export function StateBadge({ state, pulse = false, className }: Props) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium',
        STATE_STYLES[state] ?? STATE_STYLES.UNKNOWN,
        className,
      )}
    >
      <span
        className={cn(
          'size-1.5 rounded-full',
          STATE_DOT[state] ?? STATE_DOT.UNKNOWN,
          pulse && state !== 'HEALTHY' && state !== 'UNKNOWN' && 'animate-pulse',
        )}
      />
      {state}
    </span>
  )
}
