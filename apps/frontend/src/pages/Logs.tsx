import { useEffect, useRef, useState } from 'react'
import { Play, Square, Trash2 } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { getToken } from '@/api/client'
import { fetchServices } from '@/api/services'

const WS_BASE =
  (import.meta.env.VITE_WS_URL as string | undefined) ??
  `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}`

export function Logs() {
  const [selected, setSelected] = useState('')
  const [container, setContainer] = useState('')
  const [lines, setLines] = useState<string[]>([])
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data: servicesData } = useQuery({
    queryKey: ['services'],
    queryFn: () => fetchServices(),
    staleTime: 30_000,
  })

  function connect(name: string) {
    if (wsRef.current) {
      wsRef.current.close()
    }
    const token = getToken()
    const url = `${WS_BASE}/ws/logs/${encodeURIComponent(name)}${token ? `?token=${encodeURIComponent(token)}` : ''}`
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setConnected(true)
      setLines([`--- connected to ${name} ---`])
    }
    ws.onmessage = (e: MessageEvent) => {
      const text = e.data as string
      setLines((prev) => [...prev.slice(-2000), text])
    }
    ws.onclose = () => {
      setConnected(false)
      setLines((prev) => [...prev, '--- disconnected ---'])
    }
    ws.onerror = () => ws.close()
  }

  function disconnect() {
    wsRef.current?.close()
    wsRef.current = null
    setConnected(false)
  }

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [lines])

  useEffect(() => () => wsRef.current?.close(), [])

  return (
    <div className="page flex h-full flex-col" style={{ height: '100%' }}>
      <div className="mb-6">
        <h1 className="text-xl font-semibold" style={{ color: 'var(--text-1)' }}>Logs</h1>
        <p className="mt-1 text-sm" style={{ color: 'var(--text-3)' }}>
          Attach to a service container and stream its output in real time.
        </p>
      </div>

      {/* Controls */}
      <div className="panel mb-4 flex flex-wrap gap-3 p-3">
        <select
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
          disabled={connected}
          className="field min-w-48 flex-1"
        >
          <option value="">Select a service…</option>
          {(servicesData?.services ?? []).map((svc) => (
            <option key={svc.name} value={svc.name}>{svc.name}</option>
          ))}
        </select>
        <button
          onClick={() => {
            if (!selected) return
            setContainer(selected)
            connect(selected)
          }}
          disabled={!selected || connected}
          className="action-button action-primary disabled:opacity-40"
        >
          <Play className="size-3.5" /> Connect
        </button>
        <button
          onClick={disconnect}
          disabled={!connected}
          className="action-button disabled:opacity-40"
        >
          <Square className="size-3.5" /> Disconnect
        </button>
        <button
          onClick={() => setLines([])}
          disabled={lines.length === 0}
          className="icon-button disabled:opacity-40"
          title="Clear"
        >
          <Trash2 className="size-4" />
        </button>
      </div>

      {/* Status */}
      {container && (
        <div className="mb-2 flex items-center gap-2">
          <span className={`size-2 rounded-full ${connected ? 'bg-emerald-500' : 'bg-gray-400'}`} />
          <span className="text-xs" style={{ color: 'var(--text-3)' }}>
            {connected ? `streaming ${container}` : 'disconnected'}
          </span>
        </div>
      )}

      {/* Log pane */}
      <div
        className="flex-1 overflow-y-auto rounded-xl border p-4 font-mono text-xs leading-5"
        style={{ borderColor: 'var(--border)', background: '#101827', color: '#c9d1e8' }}
      >
        {lines.length === 0 ? (
          <p style={{ color: '#4a5470' }}>Select a service and press Connect to start streaming.</p>
        ) : (
          lines.map((line, i) => (
            <div
              key={i}
              style={{
                color: line.startsWith('---')
                  ? '#4a5470'
                  : line.toLowerCase().includes('error') || line.toLowerCase().includes('fatal')
                    ? '#f87171'
                    : line.toLowerCase().includes('warn')
                      ? '#fbbf24'
                      : '#c9d1e8',
              }}
            >
              {line}
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  )
}
