import type { ReactNode } from 'react'
import { SearchX } from 'lucide-react'
import { cn } from '@/lib/utils'

interface Props {
  icon?: ReactNode
  title: string
  description?: string
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: Props) {
  return (
    <div
      className={cn('flex flex-col items-start gap-2 rounded-xl border px-6 py-8', className)}
      style={{ borderColor: 'var(--border)', background: 'var(--surface)' }}
    >
      <div
        className="mb-1 flex size-9 items-center justify-center rounded-lg"
        style={{ background: 'var(--surface-3)', color: 'var(--text-3)' }}
      >
        <div className="[&>svg]:size-4">{icon ?? <SearchX />}</div>
      </div>
      <p className="text-sm font-semibold" style={{ color: 'var(--text-1)' }}>{title}</p>
      {description && (
        <p className="text-sm leading-relaxed" style={{ color: 'var(--text-3)' }}>{description}</p>
      )}
      {action && <div className="pt-2">{action}</div>}
    </div>
  )
}
