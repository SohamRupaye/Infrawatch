import { cn } from '@/lib/utils'
import type { BreakerState } from '@/api/types'

const STYLES: Record<BreakerState, string> = {
  CLOSED:    'bg-emerald-50 text-emerald-700 border-emerald-200',
  OPEN:      'bg-red-50 text-red-700 border-red-200',
  HALF_OPEN: 'bg-yellow-50 text-yellow-700 border-yellow-200',
}

interface Props {
  state: BreakerState
  className?: string
}

export function CircuitBadge({ state, className }: Props) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium',
        STYLES[state] ?? STYLES.CLOSED,
        className,
      )}
    >
      {state.replace('_', ' ')}
    </span>
  )
}
