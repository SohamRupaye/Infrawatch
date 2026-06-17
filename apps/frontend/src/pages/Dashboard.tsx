import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { RefreshCw, AlertTriangle, Activity, Clock3, ShieldCheck, LayoutGrid, List, Search, X } from 'lucide-react'
import { fetchServices } from '@/api/services'
import { useLiveStore } from '@/store/liveStore'
import { StateBadge, STATE_DOT } from '@/components/StateBadge'
import { Spinner } from '@/components/Spinner'
import { EmptyState } from '@/components/EmptyState'
import { cn } from '@/lib/utils'
import type { ServiceState } from '@/api/types'

const BORDER_BY_STATE: Record<ServiceState, string> = {
  HEALTHY:   'border-emerald-200',
  DEGRADED:  'border-yellow-200',
  UNHEALTHY: 'border-orange-200',
  DEAD:      'border-red-300',
  RECOVERING:'border-blue-200',
  UNKNOWN:   'border-gray-200',
}

type ViewMode = 'grid' | 'list'

export function Dashboard() {
  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['services'],
    queryFn: () => fetchServices(),
    refetchInterval: 30_000,
  })

  const liveStates = useLiveStore((s) => s.serviceStates)

  const [view, setView]           = useState<ViewMode>('grid')
  const [search, setSearch]       = useState('')
  const [activeTag, setActiveTag] = useState<string | null>(null)

  const services = useMemo(
    () => [...(data?.services ?? [])].sort((a, b) => a.name.localeCompare(b.name)),
    [data],
  )

  const allTags = useMemo(
    () => Array.from(new Set(services.flatMap((s) => s.tags ?? []))).sort(),
    [services],
  )

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return services.filter((svc) => {
      if (q && !svc.name.toLowerCase().includes(q)) return false
      if (activeTag && !(svc.tags ?? []).includes(activeTag)) return false
      return true
    })
  }, [services, search, activeTag])

  const counts = useMemo(
    () =>
      services.reduce((acc, svc) => {
        const state = liveStates[svc.name]?.state ?? svc.state
        acc[state] = (acc[state] ?? 0) + 1
        return acc
      }, {} as Record<ServiceState, number>),
    [services, liveStates],
  )

  return (
    <div className="page">
      {/* Header */}
      <div className="mb-6 flex items-center justify-between gap-4">
        <div className="flex items-center gap-2.5">
          <span className="relative flex size-1.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
            <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
          </span>
          <h1 className="text-sm font-semibold" style={{ color: 'var(--text-1)' }}>Dashboard</h1>
          {data && (
            <span className="text-xs" style={{ color: 'var(--text-3)' }}>
              {data.total} services monitored
            </span>
          )}
        </div>
        <button
          onClick={() => void refetch()}
          disabled={isFetching}
          className="action-button"
          title="Refresh services"
        >
          <RefreshCw className={cn('size-3.5', isFetching && 'animate-spin')} />
          Refresh
        </button>
      </div>

      {/* Summary tiles */}
      {services.length > 0 && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div className="stat-tile">
            <div className="flex items-center gap-3">
              <Activity className="size-4" style={{ color: 'var(--accent)' }} />
              <div>
                <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Services</p>
                <p className="text-lg font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>{services.length}</p>
              </div>
            </div>
          </div>
          <div className="stat-tile">
            <div className="flex items-center gap-3">
              <ShieldCheck className="size-4 text-emerald-500" />
              <div>
                <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Healthy</p>
                <p className="text-lg font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>{counts.HEALTHY ?? 0}</p>
              </div>
            </div>
          </div>
          <div className="stat-tile">
            <div className="flex items-center gap-3">
              <AlertTriangle className="size-4 text-orange-400" />
              <div>
                <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Needs eyes</p>
                <p className="text-lg font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>
                  {(counts.DEGRADED ?? 0) + (counts.UNHEALTHY ?? 0) + (counts.DEAD ?? 0)}
                </p>
              </div>
            </div>
          </div>
          <div className="stat-tile">
            <div className="flex items-center gap-3">
              <Clock3 className="size-4" style={{ color: 'var(--text-3)' }} />
              <div>
                <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Refresh</p>
                <p className="text-lg font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>30s</p>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* State summary pills */}
      {services.length > 0 && (
        <div className="mb-4 flex flex-wrap gap-2">
          {(['HEALTHY', 'DEGRADED', 'UNHEALTHY', 'DEAD', 'RECOVERING'] as ServiceState[]).map(
            (state) =>
              counts[state] ? (
                <div key={state} className="tag gap-2">
                  <span className={cn('size-2 rounded-full', STATE_DOT[state])} />
                  <span style={{ color: 'var(--text-1)' }}>{counts[state]}</span>
                  <span style={{ color: 'var(--text-3)' }}>{state}</span>
                </div>
              ) : null,
          )}
        </div>
      )}

      {/* Toolbar: search · tag filters · view toggle */}
      {services.length > 0 && (
        <div className="mb-5 flex flex-wrap items-center gap-2">
          {/* Search */}
          <div className="relative flex-1" style={{ minWidth: 180, maxWidth: 280 }}>
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2" style={{ color: 'var(--text-3)' }} />
            <input
              type="text"
              placeholder="Search services…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full rounded-lg border py-1.5 pl-8 pr-7 text-xs outline-none focus:ring-1"
              style={{
                background: 'var(--surface)',
                borderColor: 'var(--border-2)',
                color: 'var(--text-1)',
                // @ts-ignore
                '--tw-ring-color': 'var(--accent)',
              }}
            />
            {search && (
              <button
                onClick={() => setSearch('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 opacity-50 hover:opacity-100"
              >
                <X className="size-3" style={{ color: 'var(--text-2)' }} />
              </button>
            )}
          </div>

          {/* Tag pills */}
          <div className="flex flex-wrap gap-1.5">
            {allTags.map((tag) => (
              <button
                key={tag}
                onClick={() => setActiveTag(activeTag === tag ? null : tag)}
                className="rounded-md border px-2 py-0.5 text-[11px] font-medium transition-colors"
                style={
                  activeTag === tag
                    ? { background: 'var(--accent)', borderColor: 'var(--accent)', color: '#fff' }
                    : { background: 'var(--surface)', borderColor: 'var(--border-2)', color: 'var(--text-2)' }
                }
              >
                {tag}
              </button>
            ))}
          </div>

          {/* Spacer */}
          <div className="ml-auto flex items-center gap-1 rounded-lg border p-0.5" style={{ borderColor: 'var(--border)', background: 'var(--surface-2)' }}>
            <button
              onClick={() => setView('grid')}
              className="rounded-md p-1.5 transition-colors"
              title="Grid view"
              style={view === 'grid' ? { background: 'var(--surface)', color: 'var(--accent)', boxShadow: '0 1px 2px rgba(0,0,0,.06)' } : { color: 'var(--text-3)' }}
            >
              <LayoutGrid className="size-3.5" />
            </button>
            <button
              onClick={() => setView('list')}
              className="rounded-md p-1.5 transition-colors"
              title="List view"
              style={view === 'list' ? { background: 'var(--surface)', color: 'var(--accent)', boxShadow: '0 1px 2px rgba(0,0,0,.06)' } : { color: 'var(--text-3)' }}
            >
              <List className="size-3.5" />
            </button>
          </div>
        </div>
      )}

      {/* Loading / error */}
      {isLoading && (
        <div className="flex justify-center py-20">
          <Spinner size="lg" />
        </div>
      )}
      {error && (
        <div className="panel flex items-center gap-2 border-red-300 bg-red-50 px-4 py-3 text-sm text-red-600">
          <AlertTriangle className="size-4 shrink-0" />
          {(error as Error).message}
        </div>
      )}

      {!isLoading && !error && services.length === 0 && (
        <EmptyState
          title="No services configured"
          description="Add services in the Config section to start monitoring."
        />
      )}

      {!isLoading && !error && services.length > 0 && filtered.length === 0 && (
        <EmptyState title="No matches" description="Try a different search term or tag filter." />
      )}

      {/* ── Grid view ── */}
      {view === 'grid' && filtered.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filtered.map((svc) => {
            const live  = liveStates[svc.name]
            const state: ServiceState = live?.state ?? svc.state
            const rtMs  = live?.response_time_ms ?? svc.response_time_ms

            return (
              <Link
                key={svc.name}
                to={`/services/${encodeURIComponent(svc.name)}`}
                className={cn(
                  'group panel flex min-h-36 flex-col gap-3 p-4 transition-all hover:shadow-md hover:border-[var(--border-2)]',
                  BORDER_BY_STATE[state] ?? 'border-gray-200',
                )}
              >
                <div className="flex items-start justify-between gap-2">
                  <span className="truncate text-sm font-semibold" style={{ color: 'var(--text-1)' }} title={svc.name}>
                    {svc.name}
                  </span>
                  <StateBadge state={state} pulse className="shrink-0" />
                </div>

                {svc.tags && svc.tags.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {svc.tags.slice(0, 3).map((t) => (
                      <span key={t} className="tag">{t}</span>
                    ))}
                    {svc.tags.length > 3 && (
                      <span className="text-xs" style={{ color: 'var(--text-3)' }}>+{svc.tags.length - 3}</span>
                    )}
                  </div>
                )}

                <div className="mt-auto grid grid-cols-2 gap-2">
                  <div className="rounded-md border px-2.5 py-1.5" style={{ borderColor: 'var(--border)', background: 'var(--surface-2)' }}>
                    <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Latency</p>
                    <p className="mt-0.5 text-sm font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>{rtMs > 0 ? `${rtMs} ms` : '—'}</p>
                  </div>
                  <div className="rounded-md border px-2.5 py-1.5" style={{ borderColor: 'var(--border)', background: 'var(--surface-2)' }}>
                    <p className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Uptime</p>
                    <p className="mt-0.5 text-sm font-semibold tabular-nums" style={{ color: 'var(--text-1)' }}>{svc.uptime_pct.toFixed(1)}%</p>
                  </div>
                </div>
              </Link>
            )
          })}
        </div>
      )}

      {/* ── List view ── */}
      {view === 'list' && filtered.length > 0 && (
        <div className="panel overflow-hidden p-0">
          {/* Table header */}
          <div
            className="grid items-center gap-4 border-b px-4 py-2 text-[10px] font-semibold uppercase tracking-wider"
            style={{ gridTemplateColumns: '1fr 140px 100px 100px 90px', borderColor: 'var(--border)', background: 'var(--surface-2)', color: 'var(--text-3)' }}
          >
            <span>Service</span>
            <span>Tags</span>
            <span>State</span>
            <span className="tabular-nums">Latency</span>
            <span className="tabular-nums">Uptime</span>
          </div>
          {filtered.map((svc, i) => {
            const live  = liveStates[svc.name]
            const state: ServiceState = live?.state ?? svc.state
            const rtMs  = live?.response_time_ms ?? svc.response_time_ms

            return (
              <Link
                key={svc.name}
                to={`/services/${encodeURIComponent(svc.name)}`}
                className={cn(
                  'grid items-center gap-4 border-b px-4 py-2.5 text-sm transition-colors last:border-b-0',
                  BORDER_BY_STATE[state],
                )}
                style={{
                  gridTemplateColumns: '1fr 140px 100px 100px 90px',
                  borderBottomColor: i < filtered.length - 1 ? 'var(--border)' : 'transparent',
                  borderLeftWidth: 2,
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--surface-2)')}
                onMouseLeave={(e) => (e.currentTarget.style.background = '')}
              >
                <span className="truncate font-medium" style={{ color: 'var(--text-1)' }}>{svc.name}</span>
                <div className="flex flex-wrap gap-1">
                  {(svc.tags ?? []).slice(0, 2).map((t) => (
                    <span key={t} className="tag">{t}</span>
                  ))}
                  {(svc.tags ?? []).length > 2 && (
                    <span className="text-xs" style={{ color: 'var(--text-3)' }}>+{(svc.tags ?? []).length - 2}</span>
                  )}
                </div>
                <div><StateBadge state={state} /></div>
                <span className="tabular-nums text-xs" style={{ color: 'var(--text-2)' }}>{rtMs > 0 ? `${rtMs} ms` : '—'}</span>
                <span className="tabular-nums text-xs" style={{ color: 'var(--text-2)' }}>{svc.uptime_pct.toFixed(1)}%</span>
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}
