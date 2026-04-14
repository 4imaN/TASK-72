// API client — wraps fetch with auth cookie forwarding and error handling.
import { APP_VERSION, CLIENT_VERSION_HEADER } from '../version'

export const API_BASE = '/api/v1'

export interface ApiError {
  code: string
  message: string
  fields?: Record<string, string[]>
  traceId?: string
}

export class PortalApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly error: ApiError,
  ) {
    super(error.message)
    this.name = 'PortalApiError'
  }

  get isUnauthorized() { return this.status === 401 }
  get isForbidden()    { return this.status === 403 }
  get isReadOnly()     { return this.error.code === 'compatibility.read_only' }
  get isBlocked()      { return this.error.code === 'compatibility.blocked' }
}

async function parseError(res: Response): Promise<ApiError> {
  try {
    const body = await res.json()
    return {
      code:    body.code    ?? 'unknown',
      message: body.message ?? res.statusText,
      fields:  body.fields,
      traceId: body.trace_id,
    }
  } catch {
    return { code: 'parse_error', message: res.statusText }
  }
}

export async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const url = path.startsWith('http') ? path : `${API_BASE}${path}`

  const headers = new Headers(options.headers)
  headers.set(CLIENT_VERSION_HEADER, APP_VERSION)
  if (!headers.has('Content-Type') && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }

  const res = await fetch(url, {
    ...options,
    headers,
    credentials: 'include', // send HttpOnly session cookie
  })

  if (!res.ok) {
    const err = await parseError(res)
    throw new PortalApiError(res.status, err)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  get:    <T>(path: string, init?: RequestInit) =>
    apiFetch<T>(path, { ...init, method: 'GET' }),
  post:   <T>(path: string, body?: unknown, init?: RequestInit) =>
    apiFetch<T>(path, { ...init, method: 'POST',  body: body ? JSON.stringify(body) : undefined }),
  put:    <T>(path: string, body?: unknown, init?: RequestInit) =>
    apiFetch<T>(path, { ...init, method: 'PUT',   body: body ? JSON.stringify(body) : undefined }),
  patch:  <T>(path: string, body?: unknown, init?: RequestInit) =>
    apiFetch<T>(path, { ...init, method: 'PATCH', body: body ? JSON.stringify(body) : undefined }),
  delete: <T>(path: string, init?: RequestInit) =>
    apiFetch<T>(path, { ...init, method: 'DELETE' }),
}
