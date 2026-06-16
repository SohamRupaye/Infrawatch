import { create } from 'zustand'
import type { ServiceState, AnomalyEvent, HealingEvent, WSEvent } from '@/api/types'

interface ServiceLiveState {
  state: ServiceState
  previous_state: ServiceState
  response_time_ms: number
  last_updated: string
}

interface LiveStore {
  /** Latest live state per service — overlaid on top of REST data. */
  serviceStates: Record<string, ServiceLiveState>
  /** Rolling 50-entry anomaly feed. */
  recentAnomalies: AnomalyEvent[]
  /** Rolling 50-entry healing feed. */
  recentHealing: HealingEvent[]
  /** Whether the WebSocket is currently connected. */
  wsConnected: boolean
  setWsConnected: (connected: boolean) => void
  applyEvent: (event: WSEvent) => void
  clear: () => void
}

export const useLiveStore = create<LiveStore>((set) => ({
  serviceStates: {},
  recentAnomalies: [],
  recentHealing: [],
  wsConnected: false,

  setWsConnected: (connected) => set({ wsConnected: connected }),

  applyEvent: (event) => {
    switch (event.type) {
      case 'state_change':
        set((s) => ({
          serviceStates: {
            ...s.serviceStates,
            [event.payload.service_name]: {
              state: event.payload.new_state,
              previous_state: event.payload.previous_state,
              response_time_ms:
                s.serviceStates[event.payload.service_name]?.response_time_ms ?? 0,
              last_updated: event.payload.timestamp,
            },
          },
        }))
        break

      case 'metric':
        set((s) => {
          const prev = s.serviceStates[event.payload.service_name]
          return {
            serviceStates: {
              ...s.serviceStates,
              [event.payload.service_name]: {
                state: event.payload.state,
                previous_state: prev?.state ?? 'UNKNOWN',
                response_time_ms: event.payload.response_time_ms,
                last_updated: event.payload.timestamp,
              },
            },
          }
        })
        break

      case 'anomaly':
        set((s) => ({
          recentAnomalies: [event.payload, ...s.recentAnomalies].slice(0, 50),
        }))
        break

      case 'healing':
        set((s) => ({
          recentHealing: [event.payload, ...s.recentHealing].slice(0, 50),
        }))
        break
    }
  },

  clear: () =>
    set({ serviceStates: {}, recentAnomalies: [], recentHealing: [] }),
}))
