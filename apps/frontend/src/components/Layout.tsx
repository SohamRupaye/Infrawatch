import type { ReactNode } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { useWebSocket } from '@/hooks/useWebSocket'

interface Props {
  children?: ReactNode
}

export function Layout({ children }: Props) {
  useWebSocket()

  return (
    <div className="app-shell flex h-screen" style={{ color: 'var(--text-1)' }}>
      <Sidebar />
      <main className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 overflow-y-auto">
          {children ?? <Outlet />}
        </div>
      </main>
    </div>
  )
}
