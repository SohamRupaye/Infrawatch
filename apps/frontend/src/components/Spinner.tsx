import { cn } from '@/lib/utils'

interface Props {
  className?: string
  size?: 'sm' | 'md' | 'lg'
}

const SIZE: Record<NonNullable<Props['size']>, string> = {
  sm: 'size-4 border-2',
  md: 'size-6 border-2',
  lg: 'size-10 border-4',
}

export function Spinner({ className, size = 'md' }: Props) {
  return (
    <div
      className={cn(
        'animate-spin rounded-full border-slate-700 border-t-slate-300',
        SIZE[size],
        className,
      )}
      role="status"
      aria-label="Loading"
    />
  )
}
