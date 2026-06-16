import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
  ReferenceLine,
} from 'recharts'
import type { MetricPoint, DBMetricPoint } from '@/api/types'
import { formatDate } from '@/lib/utils'

interface ChartPoint {
  timestamp: string
  latency: number
  healthy: boolean
}

function normalise(pts: (MetricPoint | DBMetricPoint)[]): ChartPoint[] {
  return pts.map((p) => {
    if ('latency_ms' in p) {
      return { timestamp: p.timestamp, latency: p.latency_ms, healthy: p.is_healthy }
    }
    return {
      timestamp: p.timestamp,
      latency: p.response_time_ms,
      healthy: p.ok,
    }
  })
}

interface Props {
  points: (MetricPoint | DBMetricPoint)[]
  p95?: number
  height?: number
}

export function MetricChart({ points, p95, height = 200 }: Props) {
  const data = normalise(points).slice(-200)

  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data} margin={{ top: 4, right: 8, left: -16, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#e2e5ef" />
        <XAxis
          dataKey="timestamp"
          tickFormatter={(v: string) => formatDate(v).split(',')[1]?.trim() ?? ''}
          stroke="#c8ccdb"
          tick={{ fill: '#8990aa', fontSize: 10 }}
          interval="preserveStartEnd"
        />
        <YAxis
          stroke="#c8ccdb"
          tick={{ fill: '#8990aa', fontSize: 10 }}
          unit="ms"
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#ffffff',
            border: '1px solid #e2e5ef',
            borderRadius: 6,
          }}
          labelStyle={{ color: '#4a5470', fontSize: 11 }}
          itemStyle={{ color: '#313851', fontSize: 11 }}
          labelFormatter={(v) => formatDate(String(v))}
          formatter={(v) => [`${Number(v).toFixed(1)} ms`, 'Latency']}
        />
        {p95 && (
          <ReferenceLine
            y={p95}
            stroke="#f59e0b"
            strokeDasharray="4 2"
            label={{ value: 'p95', fill: '#f59e0b', fontSize: 10 }}
          />
        )}
        <Line
          type="monotone"
          dataKey="latency"
          stroke="#313851"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 3, fill: '#313851' }}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}
