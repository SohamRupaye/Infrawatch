import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { PublicStatus, ServiceState } from '@/api/types'
import { formatRelative } from '@/lib/utils'
import { Activity, ShieldCheck } from 'lucide-react'

const STATE_COLOR: Record<ServiceState, string> = {
  HEALTHY:   'bg-emerald-500',
  DEGRADED:  'bg-yellow-500',
  UNHEALTHY: 'bg-orange-500',
  DEAD:      'bg-red-600',
  RECOVERING:'bg-blue-500',
  UNKNOWN:   'bg-gray-400',
}

const OVERALL_STYLE: Record<string, { border: string; bg: string; color: string }> = {
  operational: { border: '#10b98133', bg: '#10b98110', color: '#059669' },
  degraded:    { border: '#eab30833', bg: '#eab30810', color: '#d97706' },
  outage:      { border: '#ef444433', bg: '#ef444410', color: '#dc2626' },
}

export function StatusPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['public-status'],
    queryFn: () => api.get<PublicStatus>('/api/public/status'),
    refetchInterval: 30_000,
  })

  return (
    <div className="app-shell min-h-screen px-4 py-12">
      <div className="mx-auto max-w-3xl">
      {/* Header */}
      <div className="hero-panel mb-8">
        <div className="hero-content flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex size-12 items-center justify-center rounded-xl border-2" style={{ borderColor: 'var(--ink)', background: 'var(--accent)', boxShadow: '4px 4px 0 var(--ink)' }}>
            <Activity className="size-6 text-white" />
          </div>
          <div>
            <p className="eyebrow">Public status</p>
            <span className="text-3xl font-black" style={{ color: 'var(--text-1)' }}>System Status</span>
          </div>
        </div>
        {data && (
          <span className="tag">
            Updated {formatRelative(data.updated_at)}
          </span>
        )}
        </div>
      </div>

      {isLoading && (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-14 animate-pulse rounded-xl" style={{ background: 'var(--surface-3)' }} />
          ))}
        </div>
      )}

      {error && (
        <p className="text-sm text-red-400">{(error as Error).message}</p>
      )}

      {data && (
        <>
          {/* Overall status banner */}
          {(() => {
            const s = OVERALL_STYLE[data.overall] ?? OVERALL_STYLE.operational
            return (
              <div
                className="panel mb-8 flex items-center gap-3 px-5 py-4 text-sm font-black capitalize"
                style={{ background: s.bg, color: s.color }}
              >
                <ShieldCheck className="size-5" />
                {data.overall === 'operational'
                  ? 'All systems operational'
                  : data.overall === 'degraded'
                    ? 'Some systems degraded'
                    : 'Service outage'}
              </div>
            )
          })()}

          {/* Per-service rows */}
          <div className="space-y-2">
            {data.services.map((svc) => (
              <div
                key={svc.name}
                className="panel flex items-center justify-between px-5 py-4"
              >
                <div className="flex items-center gap-3">
                  <span className={`size-2.5 rounded-full ${STATE_COLOR[svc.state] ?? STATE_COLOR.UNKNOWN}`} />
                  <span className="text-sm font-semibold" style={{ color: 'var(--text-1)' }}>{svc.name}</span>
                  {(svc.tags ?? []).slice(0, 2).map((t) => (
                    <span key={t} className="tag">
                      {t}
                    </span>
                  ))}
                </div>
                <div className="flex items-center gap-4 text-xs" style={{ color: 'var(--text-3)' }}>
                  <span>{svc.uptime_pct.toFixed(1)}% uptime</span>
                  <span className="capitalize">{svc.state.toLowerCase()}</span>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
      </div>
    </div>
  )
}
