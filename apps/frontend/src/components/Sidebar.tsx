import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import {
  LayoutDashboard,
  GitFork,
  AlertTriangle,
  Bell,
  Settings,
  Activity,
  ScrollText,
  Globe,
  Wifi,
  WifiOff,
  PanelLeftClose,
  PanelLeftOpen,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useLiveStore } from '@/store/liveStore'

const NAV = [
  { to: '/',          icon: LayoutDashboard, label: 'Dashboard'        },
  { to: '/graph',     icon: GitFork,         label: 'Dependency Graph' },
  { to: '/incidents', icon: AlertTriangle,   label: 'Incidents'        },
  { to: '/alerts',    icon: Bell,            label: 'Alerts'           },
  { to: '/logs',      icon: ScrollText,      label: 'Logs'             },
  { to: '/config',    icon: Settings,        label: 'Config'           },
  { to: '/status',    icon: Globe,           label: 'Status Page'      },
]

export function Sidebar() {
  const wsConnected = useLiveStore((s) => s.wsConnected)
  const [collapsed, setCollapsed] = useState(false)

  return (
    <aside
      className={cn(
        'flex h-full shrink-0 flex-col border-r transition-[width] duration-200',
        collapsed ? 'w-[60px]' : 'w-56',
      )}
      style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      {/* Logo */}
      <div
        className={cn('flex h-14 items-center border-b px-4', collapsed ? 'justify-center' : 'justify-between gap-3')}
        style={{ borderColor: 'var(--border)' }}
      >
        <div className={cn('flex items-center gap-3', collapsed && 'justify-center')}>
          <div
            className="flex size-8 items-center justify-center rounded-lg"
            style={{ background: 'var(--accent)' }}
          >
            <Activity className="size-5 text-white" />
          </div>
          {!collapsed && (
            <div>
              <span className="block text-sm font-semibold tracking-tight" style={{ color: 'var(--text-1)' }}>Infrawatch</span>
              <span className="text-[10px] font-medium uppercase tracking-wider" style={{ color: 'var(--text-3)' }}>Ops cockpit</span>
            </div>
          )}
        </div>
        {!collapsed && (
          <button
            type="button"
            onClick={() => setCollapsed(true)}
            className="icon-button size-9 shadow-none"
            title="Collapse sidebar"
          >
            <PanelLeftClose className="size-4" />
          </button>
        )}
      </div>

      {collapsed && (
        <div className="border-b px-4 py-3" style={{ borderColor: 'var(--border)' }}>
          <button
            type="button"
            onClick={() => setCollapsed(false)}
            className="icon-button size-10"
            title="Expand sidebar"
          >
            <PanelLeftOpen className="size-4" />
          </button>
        </div>
      )}

      {/* Nav */}
      <nav className="flex-1 overflow-y-auto px-3 py-5">
        <ul className="space-y-2">
          {NAV.map(({ to, icon: Icon, label }) => (
            <li key={to}>
              <NavLink
                to={to}
                end={to === '/'}
                title={collapsed ? label : undefined}
                className={({ isActive }) =>
                  cn(
                    'group flex min-h-9 items-center gap-2.5 rounded-lg px-2.5 text-sm font-medium transition-all',
                    collapsed && 'justify-center px-0',
                    isActive
                      ? 'bg-[var(--accent)] text-white'
                      : 'text-[var(--text-2)] hover:bg-[var(--surface-3)] hover:text-[var(--text-1)]',
                  )
                }
                style={({ isActive }) => ({ color: isActive ? undefined : undefined })}
              >
                <Icon className="size-4 shrink-0" />
                {!collapsed && <span className="truncate">{label}</span>}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      {/* WS indicator */}
      <div
        className={cn('border-t p-3', collapsed ? 'flex justify-center' : '')}
        style={{ borderColor: 'var(--border)' }}
      >
        <div
          className={cn(
            'flex items-center gap-2 rounded-lg px-2.5 py-1.5',
            collapsed && 'size-9 justify-center p-0',
          )}
          style={{
            background: wsConnected ? '#ecfdf5' : '#fff7ed',
            border: `1px solid ${wsConnected ? '#a7f3d0' : '#fed7aa'}`,
          }}
          title={wsConnected ? 'Live' : 'Reconnecting'}
        >
          {wsConnected ? (
            <Wifi className="size-4 text-emerald-600" />
          ) : (
            <WifiOff className="size-4 text-orange-600" />
          )}
          {!collapsed && (
            <span className="text-xs font-medium" style={{ color: wsConnected ? '#047857' : '#c2410c' }}>
              {wsConnected ? 'Live stream' : 'Reconnecting'}
            </span>
          )}
        </div>
      </div>
    </aside>
  )
}
