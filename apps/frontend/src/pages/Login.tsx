import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Activity, KeyRound, Shield } from 'lucide-react'
import { setToken } from '@/api/client'

export function Login() {
  const navigate = useNavigate()
  const [token, setTokenValue] = useState('')
  const [error, setError] = useState('')

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const t = token.trim()
    if (!t) {
      setError('Paste a valid JWT token.')
      return
    }
    setToken(t)
    navigate('/', { replace: true })
  }

  function skipAuth() {
    // No-auth mode: just navigate. Endpoints will work if the API has no JWT secret set.
    navigate('/', { replace: true })
  }

  return (
    <div className="app-shell flex min-h-screen items-center justify-center p-5">
      <div className="hero-panel w-full max-w-md">
        <div className="hero-content">
        {/* Logo */}
        <div className="mb-6 flex items-center gap-2.5">
          <div className="flex size-11 items-center justify-center rounded-xl border-2" style={{ background: 'var(--accent)', borderColor: 'var(--ink)', boxShadow: '4px 4px 0 var(--ink)' }}>
            <Activity className="size-4 text-white" />
          </div>
          <div>
            <span className="block text-xl font-black" style={{ color: 'var(--text-1)' }}>Infrawatch</span>
            <span className="eyebrow">Ops cockpit</span>
          </div>
        </div>

        <h1 className="mb-1 flex items-center gap-2 text-2xl font-black" style={{ color: 'var(--text-1)' }}>
          <Shield className="size-6" style={{ color: 'var(--accent)' }} />
          Sign in
        </h1>
        <p className="mb-6 text-sm leading-6" style={{ color: 'var(--text-2)' }}>
          Paste a signed JWT token generated with your <code style={{ color: 'var(--text-2)' }}>api.jwt_secret</code>.
        </p>

        <form onSubmit={submit} className="space-y-4">
          <div>
            <label className="mb-1.5 block text-xs font-medium" style={{ color: 'var(--text-2)' }}>
              Bearer token
            </label>
            <div className="relative">
              <KeyRound className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2" style={{ color: 'var(--text-3)' }} />
              <input
                type="password"
                value={token}
                onChange={(e) => { setTokenValue(e.target.value); setError('') }}
                placeholder="eyJhbGciOiJIUzI1NiIsInR5…"
                className="field w-full pl-9"
              />
            </div>
          </div>

          {error && <p className="text-xs text-red-500">{error}</p>}

          <button
            type="submit"
            className="action-button action-primary w-full"
          >
            Sign in
          </button>
        </form>

        <div className="mt-4 text-center">
          <button
            onClick={skipAuth}
            className="text-xs hover:underline"
            style={{ color: 'var(--text-3)' }}
          >
            Continue without token (no-auth mode)
          </button>
        </div>
        </div>
      </div>
    </div>
  )
}
