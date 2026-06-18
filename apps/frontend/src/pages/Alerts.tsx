import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCheck, X } from 'lucide-react'
import { toast } from 'sonner'
import { fetchAlerts, acknowledgeAlert } from '@/api/alerts'
import { Spinner } from '@/components/Spinner'
import { EmptyState } from '@/components/EmptyState'
import { StateBadge } from '@/components/StateBadge'
import { formatDate, formatRelative } from '@/lib/utils'

export function Alerts() {
  const [service, setService] = useState('')
  const [channel, setChannel] = useState('')
  const [unackedOnly, setUnackedOnly] = useState(false)
  const qc = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['alerts', service, channel, unackedOnly],
    queryFn: () =>
      fetchAlerts({
        service: service || undefined,
        channel: channel || undefined,
        unacked: unackedOnly,
        limit: 100,
      }),
    refetchInterval: 20_000,
  })

  const ackMut = useMutation({
    mutationFn: (id: number) => acknowledgeAlert(id),
    onSuccess: () => {
      toast.success('Alert acknowledged')
      void qc.invalidateQueries({ queryKey: ['alerts'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const alerts = data?.alerts ?? []
  const unacked = alerts.filter((a) => !a.acknowledged).length

  return (
    <div className="page">
      {/* Header */}
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <h1 className="text-sm font-semibold" style={{ color: 'var(--text-1)' }}>Alerts</h1>
          {unacked > 0 && (
            <span className="rounded-md px-2 py-0.5 text-xs font-medium" style={{ background: '#fef3c7', color: '#b45309' }}>
              {unacked} unacked
            </span>
          )}
        </div>

        {/* Filters inline */}
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <input
              type="text"
              placeholder="Service…"
              value={service}
              onChange={(e) => setService(e.target.value)}
              className="field w-36 pr-6"
            />
            {service && (
              <button onClick={() => setService('')} className="absolute right-2 top-1/2 -translate-y-1/2 opacity-50 hover:opacity-100">
                <X className="size-3" style={{ color: 'var(--text-2)' }} />
              </button>
            )}
          </div>
          <div className="relative">
            <input
              type="text"
              placeholder="Channel…"
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              className="field w-28 pr-6"
            />
            {channel && (
              <button onClick={() => setChannel('')} className="absolute right-2 top-1/2 -translate-y-1/2 opacity-50 hover:opacity-100">
                <X className="size-3" style={{ color: 'var(--text-2)' }} />
              </button>
            )}
          </div>
          <label className="flex cursor-pointer items-center gap-1.5 text-xs" style={{ color: 'var(--text-2)' }}>
            <input
              type="checkbox"
              checked={unackedOnly}
              onChange={(e) => setUnackedOnly(e.target.checked)}
              className="size-3.5 accent-[var(--accent)]"
            />
            Unacked only
          </label>
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

      {!isLoading && alerts.length === 0 && (
        <EmptyState
          title="No alerts"
          description="No alerts match the current filters."
        />
      )}

      {alerts.length > 0 && (
        <div className="panel overflow-hidden">
          <table className="data-table">
            <thead>
              <tr>
                <th className="px-4 py-3 text-left">Service</th>
                <th className="px-4 py-3 text-left">State</th>
                <th className="px-4 py-3 text-left">Message</th>
                <th className="px-4 py-3 text-left">Channel</th>
                <th className="px-4 py-3 text-left">Time</th>
                <th className="px-4 py-3 text-left">Delivered</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {alerts.map((alert) => (
                <tr
                  key={alert.id}
                  className={alert.acknowledged ? 'opacity-45' : ''}
                >
                  <td className="px-4 py-3 font-semibold" style={{ color: 'var(--text-1)' }}>{alert.service_name}</td>
                  <td className="px-4 py-3">
                    <StateBadge state={alert.state} />
                  </td>
                  <td className="max-w-xs px-4 py-3" style={{ color: 'var(--text-2)' }} title={alert.message}>
                    <span className="block truncate">{alert.message}</span>
                  </td>
                  <td className="px-4 py-3" style={{ color: 'var(--text-3)' }}>{alert.channel}</td>
                  <td className="px-4 py-3" style={{ color: 'var(--text-3)' }} title={formatDate(alert.created_at)}>
                    {formatRelative(alert.created_at)}
                  </td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium ${alert.delivered ? 'text-emerald-600' : 'text-red-600'}`}>
                      {alert.delivered ? 'Yes' : 'No'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    {!alert.acknowledged && (
                      <button
                        onClick={() => ackMut.mutate(alert.id)}
                        disabled={ackMut.isPending}
                        title="Acknowledge"
                        className="icon-button size-9 disabled:opacity-40"
                      >
                        <CheckCheck className="size-4" />
                      </button>
                    )}
                    {alert.acknowledged && (
                      <span className="text-xs" style={{ color: 'var(--text-3)' }}>
                        ack {alert.acknowledged_by ? `by ${alert.acknowledged_by}` : ''}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
