import { useEffect, useRef } from 'react'
import { useLiveStore } from '@/store/liveStore'
import type { WSEvent } from '@/api/types'
import { getToken } from '@/api/client'

const RECONNECT_DELAY_MS = 3_000

function buildWsUrl(): string {
  const base =
    (import.meta.env.VITE_WS_URL as string | undefined) ??
    `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`
  const token = getToken()
  return token ? `${base}?token=${encodeURIComponent(token)}` : base
}

/**
 * Establishes a persistent WebSocket connection to /ws, reconnects on
 * disconnect, and feeds every message into the Zustand live store.
 * Mount this once at the root of the authenticated layout.
 */
export function useWebSocket() {
  const applyEvent = useLiveStore((s) => s.applyEvent)
  const setWsConnected = useLiveStore((s) => s.setWsConnected)
  const wsRef = useRef<WebSocket | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const deadRef = useRef(false)

  useEffect(() => {
    deadRef.current = false

    function connect() {
      if (deadRef.current) return
      const ws = new WebSocket(buildWsUrl())
      wsRef.current = ws

      ws.onopen = () => setWsConnected(true)

      ws.onmessage = (e: MessageEvent) => {
        try {
          const event = JSON.parse(e.data as string) as WSEvent
          applyEvent(event)
        } catch {
          // ignore malformed frames
        }
      }

      ws.onclose = () => {
        setWsConnected(false)
        if (!deadRef.current) {
          timerRef.current = setTimeout(connect, RECONNECT_DELAY_MS)
        }
      }

      ws.onerror = () => ws.close()
    }

    connect()

    return () => {
      deadRef.current = true
      if (timerRef.current) clearTimeout(timerRef.current)
      wsRef.current?.close()
    }
  }, [applyEvent, setWsConnected])
}
