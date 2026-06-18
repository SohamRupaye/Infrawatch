import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import * as Tabs from '@radix-ui/react-tabs'
import { ArrowLeft, Zap, RefreshCw, Gauge, Activity } from 'lucide-react'
import { toast } from 'sonner'
import { fetchService } from '@/api/services'
import { fetchMetrics, type MetricRange } from '@/api/metrics'
import { fetchIncidents } from '@/api/incidents'
import { resetCircuit } from '@/api/circuit'
import { triggerHeal } from '@/api/healing'
import { useLiveStore } from '@/store/liveStore'
import { StateBadge } from '@/components/StateBadge'
import { CircuitBadge } from '@/components/CircuitBadge'
import { MetricChart } from '@/components/MetricChart'
import { Spinner } from '@/components/Spinner'
import { EmptyState } from '@/components/EmptyState'
import { formatRelative, formatDate, formatDuration } from '@/lib/utils'
import type { ServiceState } from '@/api/types'

const RANGES: MetricRange[] = ['1h', '6h', '24h', '7d']

export function ServiceDetail() {
  const { name } = useParams<{ name: string }>()
  const svcName = decodeURIComponent(name ?? '')
  const qc = useQueryClient()

  const [range, setRange] = useState<MetricRange>('1h')
  const liveState = useLiveStore((s) => s.serviceStates[svcName])

  const { data: svc, isLoading } = useQuery({
    queryKey: ['service', svcName],
    queryFn: () => fetchService(svcName),
    refetchInterval: 15_000,
  })

  const { data: metricsData, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics', svcName, range],
    queryFn: () => fetchMetrics(svcName, range),
    refetchInterval: 30_000,
  })

  const { data: incidentsData } = useQuery({
    queryKey: ['incidents', svcName],
    queryFn: () => fetchIncidents({ service: svcName, limit: 20 }),
  })

  const resetMut = useMutation({
    mutationFn: () => resetCircuit(svcName),
    onSuccess: () => {
      toast.success('Circuit reset')
      void qc.invalidateQueries({ queryKey: ['service', svcName] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const healMut = useMutation({
    mutationFn: () => triggerHeal(svcName),
    onSuccess: () => toast.success('Heal triggered'),
    onError: (e: Error) => toast.error(e.message),
  })

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center py-20">
        <Spinner size="lg" />
      </div>
    )
  }

  if (!svc) {
    return (
      <div className="p-6">
        <p style={{ color: 'var(--text-2)' }}>Service not found: {svcName}</p>
      </div>
    )
  }

  const state: ServiceState = liveState?.state ?? svc.state
  const rtMs = liveState?.response_time_ms ?? svc.response_time_ms

  return (
    <div className="page">
      {/* Back */}
      <Link
        to="/"
        className="mb-5 inline-flex items-center gap-2 text-sm font-extrabold transition-colors hover:opacity-70"
        style={{ color: 'var(--text-3)' }}
      >
        <ArrowLeft className="size-3.5" /> Dashboard
      </Link>

      {/* Header */}
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text-1)' }}>{svc.name}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <StateBadge state={state} pulse />
            <CircuitBadge state={svc.circuit.state} />
            <span className="text-sm" style={{ color: 'var(--text-3)' }}>{svc.uptime_pct.toFixed(2)}% uptime</span>
            <span className="text-sm" style={{ color: 'var(--text-3)' }}>{rtMs} ms</span>
          </div>
          {svc.tags && svc.tags.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {svc.tags.map((t) => <span key={t} className="tag">{t}</span>)}
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => resetMut.mutate()}
            disabled={
              resetMut.isPending ||
              (svc.circuit.state !== 'OPEN' && svc.circuit.state !== 'HALF_OPEN')
            }
            className="action-button disabled:cursor-not-allowed disabled:opacity-40"
          >
            <RefreshCw className="size-3.5" /> Reset Circuit
          </button>
          <button
            onClick={() => healMut.mutate()}
            disabled={healMut.isPending}
            className="action-button disabled:opacity-40"
            style={{ background: '#f97316', color: '#fff' }}
          >
            <Zap className="size-3.5" /> Heal
          </button>
        </div>
      </div>

      {/* Tabs */}
      <Tabs.Root defaultValue="metrics">
        <Tabs.List
          className="mb-6 flex border-b"
          style={{ borderColor: 'var(--border)' }}
        >
          {['metrics', 'incidents', 'details'].map((tab) => (
            <Tabs.Trigger
              key={tab}
              value={tab}
              className="-mb-px px-5 pb-3 pt-1 text-sm font-medium capitalize transition-colors data-[state=active]:border-b-2 data-[state=active]:border-[var(--accent)] data-[state=active]:font-semibold data-[state=active]:text-[var(--accent)]"
              style={{ color: 'var(--text-3)' }}
            >
              {tab}
            </Tabs.Trigger>
          ))}
        </Tabs.List>

        {/* Metrics */}
        <Tabs.Content value="metrics">
          <div className="mb-4 flex items-center gap-2">
            {RANGES.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                  range === r
                    ? 'border-[var(--accent)] bg-[var(--accent)] text-white'
                    : 'hover:border-[var(--border-2)]'
                }`}
                style={range === r ? undefined : { color: 'var(--text-2)', borderColor: 'var(--border)' }}
              >
                {r}
              </button>
            ))}
            <span className="ml-auto text-xs" style={{ color: 'var(--text-3)' }}>
              {metricsData?.source === 'timescaledb' ? 'TimescaleDB' : 'In-memory'}
            </span>
          </div>

          {metricsLoading ? (
            <div className="flex justify-center py-10">
              <Spinner />
            </div>
          ) : (
            <>
              <div className="mb-5 grid gap-4 sm:grid-cols-2 md:grid-cols-4">
                {[
                  { label: 'P50 latency', value: `${metricsData?.p50_ms.toFixed(1) ?? '—'} ms`, icon: Gauge },
                  { label: 'P95 latency', value: `${metricsData?.p95_ms.toFixed(1) ?? '—'} ms`, icon: Activity },
                  {
                    label: 'Error rate',
                    value: `${((metricsData?.error_rate ?? 0) * 100).toFixed(1)}%`,
                    icon: Zap,
                  },
                  {
                    label: 'Uptime',
                    value: `${svc.uptime_pct.toFixed(2)}%`,
                    icon: Activity,
                  },
                ].map(({ label, value, icon: Icon }) => (
                  <div
                    key={label}
                    className="stat-tile">
                    <Icon className="mb-3 size-5" style={{ color: 'var(--accent)' }} />
                    <p className="text-xs" style={{ color: 'var(--text-3)' }}>{label}</p>
                    <p className="mt-1 text-lg font-bold" style={{ color: 'var(--text-1)' }}>{value}</p>
                  </div>
                ))}
              </div>
              <div className="panel p-5">
                <MetricChart
                  points={metricsData?.points ?? []}
                  p95={metricsData?.p95_ms}
                  height={240}
                />
              </div>
            </>
          )}
        </Tabs.Content>

        {/* Incidents */}
        <Tabs.Content value="incidents">
          {!incidentsData?.incidents.length ? (
            <EmptyState title="No incidents" description="No incidents recorded for this service." />
          ) : (
            <div className="space-y-2">
              {incidentsData.incidents.map((inc) => (
                <Link
                  key={inc.id}
                  to={`/incidents?service=${encodeURIComponent(svcName)}`}
                  className="panel flex items-center justify-between px-4 py-3 text-sm transition-opacity hover:opacity-80">
                  <div className="flex items-center gap-3">
                    <span className={`size-2 rounded-full ${inc.open ? 'bg-red-500' : 'bg-gray-300'}`} />
                    <span style={{ color: 'var(--text-1)' }}>{inc.summary || inc.root_state}</span>
                  </div>
                  <div className="text-right text-xs" style={{ color: 'var(--text-3)' }}>
                    <p>{formatDate(inc.started_at)}</p>
                    {inc.duration_sec && <p>{formatDuration(inc.duration_sec)}</p>}
                  </div>
                </Link>
              ))}
            </div>
          )}
        </Tabs.Content>

        {/* Details */}
        <Tabs.Content value="details">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="panel p-5">
              <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>
                Health State
              </h3>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Current</dt>
                  <dd><StateBadge state={state} /></dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Previous</dt>
                  <dd><StateBadge state={svc.previous_state} /></dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Consecutive fails</dt>
                  <dd style={{ color: 'var(--text-1)' }}>{svc.consecutive_fails}</dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Last checked</dt>
                  <dd style={{ color: 'var(--text-1)' }}>{formatRelative(svc.last_checked)}</dd>
                </div>
                {svc.error_message && (
                  <div className="flex justify-between gap-2">
                    <dt style={{ color: 'var(--text-3)' }}>Error</dt>
                    <dd className="text-right text-xs text-red-500">{svc.error_message}</dd>
                  </div>
                )}
              </dl>
            </div>

            <div className="panel p-5">
              <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>
                Circuit Breaker
              </h3>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>State</dt>
                  <dd><CircuitBadge state={svc.circuit.state} /></dd>
                </div>
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Consecutive fails</dt>
                  <dd style={{ color: 'var(--text-1)' }}>{svc.circuit.consecutive_fails}</dd>
                </div>
                {svc.circuit.opened_at && (
                  <div className="flex justify-between">
                    <dt style={{ color: 'var(--text-3)' }}>Opened</dt>
                    <dd style={{ color: 'var(--text-1)' }}>{formatRelative(svc.circuit.opened_at)}</dd>
                  </div>
                )}
                <div className="flex justify-between">
                  <dt style={{ color: 'var(--text-3)' }}>Last transition</dt>
                  <dd style={{ color: 'var(--text-1)' }}>{formatRelative(svc.circuit.last_transition)}</dd>
                </div>
              </dl>
            </div>

            {svc.fallback_url && (
              <div className="panel p-5 md:col-span-2">
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>
                  Active Fallback
                </h3>
                <a
                  href={svc.fallback_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm hover:underline"
                  style={{ color: 'var(--navy)' }}
                >
                  {svc.fallback_url}
                </a>
              </div>
            )}
          </div>
        </Tabs.Content>
      </Tabs.Root>
    </div>
  )
}
