import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Download, Search, Siren } from 'lucide-react'
import { fetchIncidents } from '@/api/incidents'
import { Spinner } from '@/components/Spinner'
import { EmptyState } from '@/components/EmptyState'
import { formatDate, formatDuration } from '@/lib/utils'

const PAGE_SIZE = 25

export function Incidents() {
  const [service, setService] = useState('')
  const [offset, setOffset] = useState(0)
  const [expanded, setExpanded] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['incidents', service, offset],
    queryFn: () =>
      fetchIncidents({ service: service || undefined, limit: PAGE_SIZE, offset }),
  })

  const incidents = data?.incidents ?? []

  function toggle(id: string) {
    setExpanded((prev) => (prev === id ? null : id))
  }

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <p className="eyebrow flex items-center gap-2"><Siren className="size-4" /> Incident history</p>
          <h1 className="page-title">Incidents</h1>
          <p className="page-subtitle">Browse recoveries, durations, and timelines without losing the signal.</p>
        </div>
        <div className="flex items-center gap-3">
          <Search className="size-4" style={{ color: 'var(--text-3)' }} />
          <input
            type="text"
            placeholder="Filter by service…"
            value={service}
            onChange={(e) => { setService(e.target.value); setOffset(0) }}
            className="field w-64 placeholder:text-slate-300"
          />
        </div>
      </div>

      {isLoading && (
        <div className="flex justify-center py-20">
          <Spinner size="lg" />
        </div>
      )}
      {error && (
        <p className="text-sm text-red-600">{(error as Error).message}</p>
      )}

      {!isLoading && incidents.length === 0 && (
        <EmptyState
          title="No incidents found"
          description={service ? `No incidents for "${service}".` : 'No incidents recorded yet.'}
        />
      )}

      {incidents.length > 0 && (
        <>
          <div className="panel overflow-hidden">
            <table className="data-table">
              <thead>
                <tr>
                  <th className="px-4 py-3 text-left">Service</th>
                  <th className="px-4 py-3 text-left">Summary</th>
                  <th className="px-4 py-3 text-left">Started</th>
                  <th className="px-4 py-3 text-left">Duration</th>
                  <th className="px-4 py-3 text-left">Status</th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody>
                {incidents.map((inc) => (
                  <>
                    <tr
                      key={inc.id}
                      className="cursor-pointer"
                      onClick={() => toggle(inc.id)}
                    >
                      <td className="px-4 py-3 font-semibold" style={{ color: 'var(--text-1)' }}>{inc.service_name}</td>
                      <td className="px-4 py-3" style={{ color: 'var(--text-2)' }}>
                        {inc.summary || inc.root_state || '—'}
                      </td>
                      <td className="px-4 py-3" style={{ color: 'var(--text-3)' }}>{formatDate(inc.started_at)}</td>
                      <td className="px-4 py-3" style={{ color: 'var(--text-3)' }}>
                        {inc.duration_sec ? formatDuration(inc.duration_sec) : '—'}
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-bold ${
                            inc.open
                              ? 'bg-red-50 text-red-700 border-red-200'
                              : 'bg-gray-100 text-gray-500 border-gray-200'
                          }`}
                        >
                          {inc.open ? 'Open' : 'Resolved'}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <a
                            href={`/api/v1/incidents/${inc.id}/export`}
                            target="_blank"
                            rel="noopener noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            style={{ color: 'var(--text-3)' }}
                            className="icon-button size-8 shadow-none"
                            title="Export"
                          >
                            <Download className="size-3.5" />
                          </a>
                          {expanded === inc.id ? (
                            <ChevronDown className="size-4" style={{ color: 'var(--text-3)' }} />
                          ) : (
                            <ChevronRight className="size-4" style={{ color: 'var(--text-3)' }} />
                          )}
                        </div>
                      </td>
                    </tr>

                    {/* Timeline expansion */}
                    {expanded === inc.id && inc.timeline.length > 0 && (
                      <tr key={`${inc.id}-timeline`}>
                        <td colSpan={6} className="px-4 py-3" style={{ background: 'var(--surface-3)' }}>
                          <p className="mb-2 text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>
                            Timeline
                          </p>
                          <ol className="space-y-1.5">
                            {inc.timeline.map((ev, i) => (
                              <li key={i} className="flex items-center gap-3 text-xs" style={{ color: 'var(--text-2)' }}>
                                <span className="shrink-0" style={{ color: 'var(--text-3)' }}>{formatDate(ev.timestamp)}</span>
                                <span style={{ color: 'var(--text-3)' }}>{ev.from}</span>
                                <span style={{ color: 'var(--text-3)' }}>→</span>
                                <span style={{ color: 'var(--text-1)' }}>{ev.to}</span>
                                {ev.reason && <span style={{ color: 'var(--text-3)' }}>— {ev.reason}</span>}
                              </li>
                            ))}
                          </ol>
                        </td>
                      </tr>
                    )}
                  </>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          <div className="mt-4 flex items-center justify-between text-sm" style={{ color: 'var(--text-3)' }}>
            <span>
              Showing {offset + 1}–{offset + incidents.length}
              {data?.total ? ` of ${data.total}` : ''}
            </span>
            <div className="flex gap-2">
              <button
                disabled={offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
                className="action-button min-h-0 px-3 py-1 text-xs disabled:opacity-40"
              >
                Previous
              </button>
              <button
                disabled={!data?.has_more}
                onClick={() => setOffset((o) => o + PAGE_SIZE)}
                className="action-button min-h-0 px-3 py-1 text-xs disabled:opacity-40"
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
