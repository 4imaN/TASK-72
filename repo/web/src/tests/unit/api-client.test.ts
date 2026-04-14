import { describe, it, expect, vi } from 'vitest'
import { apiFetch, PortalApiError } from '../../app/api/client'

describe('apiFetch', () => {
  it('returns parsed JSON on success', async () => {
    const mockRes = {
      ok: true,
      status: 200,
      json: async () => ({ status: 'ok' }),
    } as Response
    vi.mocked(fetch).mockResolvedValueOnce(mockRes)

    const result = await apiFetch('/api/v1/health')
    expect(result).toEqual({ status: 'ok' })
  })

  it('throws PortalApiError on non-ok response', async () => {
    const mockRes = {
      ok: false,
      status: 401,
      statusText: 'Unauthorized',
      json: async () => ({ code: 'auth.unauthenticated', message: 'Not authenticated' }),
    } as Response
    vi.mocked(fetch).mockResolvedValueOnce(mockRes)

    await expect(apiFetch('/api/v1/session')).rejects.toThrow(PortalApiError)
  })

  it('PortalApiError.isUnauthorized is true for 401', async () => {
    const mockRes = {
      ok: false,
      status: 401,
      statusText: 'Unauthorized',
      json: async () => ({ code: 'auth.unauthenticated', message: 'Not authenticated' }),
    } as Response
    vi.mocked(fetch).mockResolvedValueOnce(mockRes)

    try {
      await apiFetch('/api/v1/session')
    } catch (err) {
      expect(err).toBeInstanceOf(PortalApiError)
      expect((err as PortalApiError).isUnauthorized).toBe(true)
    }
  })

  it('returns undefined for 204 No Content', async () => {
    const mockRes = {
      ok: true,
      status: 204,
    } as Response
    vi.mocked(fetch).mockResolvedValueOnce(mockRes)

    const result = await apiFetch('/api/v1/session')
    expect(result).toBeUndefined()
  })

  it('sends X-Client-Version header', async () => {
    const mockRes = {
      ok: true,
      status: 200,
      json: async () => ({}),
    } as Response
    vi.mocked(fetch).mockResolvedValueOnce(mockRes)

    await apiFetch('/api/v1/ping')
    const [, init] = vi.mocked(fetch).mock.calls[0]
    const headers = new Headers(init?.headers)
    expect(headers.get('X-Client-Version')).toBeTruthy()
  })
})
