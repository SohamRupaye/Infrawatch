const BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? ''

export function getToken(): string | null {
  return localStorage.getItem('infrawatch_token')
}

export function setToken(token: string) {
  localStorage.setItem('infrawatch_token', token)
}

export function clearToken() {
  localStorage.removeItem('infrawatch_token')
}

function authHeaders(): Record<string, string> {
  const token = getToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export class APIError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(options.headers as Record<string, string> | undefined),
    },
  })

  if (res.status === 401) {
    clearToken()
    window.location.href = '/login'
    throw new APIError(401, 'Unauthorized')
  }

  if (!res.ok) {
    let msg = res.statusText
    try {
      const body = (await res.json()) as { error?: string }
      msg = body.error ?? msg
    } catch {
      // ignore parse error
    }
    throw new APIError(res.status, msg)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
