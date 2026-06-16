import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { Toaster } from 'sonner'
import { Layout } from '@/components/Layout'
import { Dashboard } from '@/pages/Dashboard'
import { DependencyGraph } from '@/pages/DependencyGraph'
import { ServiceDetail } from '@/pages/ServiceDetail'
import { Incidents } from '@/pages/Incidents'
import { Alerts } from '@/pages/Alerts'
import { ConfigEditor } from '@/pages/ConfigEditor'
import { Logs } from '@/pages/Logs'
import { StatusPage } from '@/pages/StatusPage'
import { Login } from '@/pages/Login'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: 1,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/status" element={<StatusPage />} />

          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="graph" element={<DependencyGraph />} />
            <Route path="services/:name" element={<ServiceDetail />} />
            <Route path="incidents" element={<Incidents />} />
            <Route path="alerts" element={<Alerts />} />
            <Route path="logs" element={<Logs />} />
            <Route path="config" element={<ConfigEditor />} />
          </Route>

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: { background: '#0f172a', border: '1px solid #1e293b', color: '#e2e8f0' },
        }}
      />
      <ReactQueryDevtools initialIsOpen={false} />
    </QueryClientProvider>
  )
}
