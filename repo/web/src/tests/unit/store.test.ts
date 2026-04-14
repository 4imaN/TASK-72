import { describe, it, expect, beforeEach } from 'vitest'
import { useAuthStore } from '../../app/store'

describe('AuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, isLoading: true, compatibilityMode: null })
  })

  it('starts with no user and loading true', () => {
    const state = useAuthStore.getState()
    expect(state.user).toBeNull()
    expect(state.isLoading).toBe(true)
  })

  it('setUser stores user', () => {
    const user = {
      id: '1',
      username: 'testuser',
      displayName: 'Test User',
      roles: ['learner'],
      permissions: ['catalog:read', 'learning:enroll'],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    }
    useAuthStore.getState().setUser(user)
    expect(useAuthStore.getState().user).toEqual(user)
  })

  it('hasPermission returns false when no user', () => {
    expect(useAuthStore.getState().hasPermission('catalog:read')).toBe(false)
  })

  it('hasPermission returns true for granted permission', () => {
    useAuthStore.getState().setUser({
      id: '1',
      username: 'u',
      displayName: 'U',
      roles: ['learner'],
      permissions: ['catalog:read'],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    expect(useAuthStore.getState().hasPermission('catalog:read')).toBe(true)
    expect(useAuthStore.getState().hasPermission('admin:anything')).toBe(false)
  })

  it('hasRole returns false for missing role', () => {
    useAuthStore.getState().setUser({
      id: '1',
      username: 'u',
      displayName: 'U',
      roles: ['learner'],
      permissions: [],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    expect(useAuthStore.getState().hasRole('admin')).toBe(false)
    expect(useAuthStore.getState().hasRole('learner')).toBe(true)
  })

  it('isAdmin returns true only for admin role', () => {
    useAuthStore.getState().setUser({
      id: '1',
      username: 'a',
      displayName: 'Admin',
      roles: ['admin'],
      permissions: [],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    expect(useAuthStore.getState().isAdmin()).toBe(true)
  })

  it('setCompatibilityMode stores mode', () => {
    useAuthStore.getState().setCompatibilityMode('read_only')
    expect(useAuthStore.getState().compatibilityMode).toBe('read_only')
  })

  it('setCompatibilityMode stores blocked mode', () => {
    useAuthStore.getState().setCompatibilityMode('blocked')
    expect(useAuthStore.getState().compatibilityMode).toBe('blocked')
  })

  it('setCompatibilityMode stores full mode', () => {
    useAuthStore.getState().setCompatibilityMode('full')
    expect(useAuthStore.getState().compatibilityMode).toBe('full')
  })

  it('setCompatibilityMode stores warn mode', () => {
    useAuthStore.getState().setCompatibilityMode('warn')
    expect(useAuthStore.getState().compatibilityMode).toBe('warn')
  })

  it('setCompatibilityMode can be reset to null', () => {
    useAuthStore.getState().setCompatibilityMode('read_only')
    expect(useAuthStore.getState().compatibilityMode).toBe('read_only')
    useAuthStore.getState().setCompatibilityMode(null)
    expect(useAuthStore.getState().compatibilityMode).toBeNull()
  })

  it('compatibilityMode does not affect permission checks', () => {
    useAuthStore.getState().setUser({
      id: '1',
      username: 'u',
      displayName: 'U',
      roles: ['learner'],
      permissions: ['catalog:read'],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    useAuthStore.getState().setCompatibilityMode('read_only')
    expect(useAuthStore.getState().hasPermission('catalog:read')).toBe(true)
    expect(useAuthStore.getState().hasRole('learner')).toBe(true)
  })

  it('hasPermission checks exports:write for finance role', () => {
    useAuthStore.getState().setUser({
      id: '2',
      username: 'fin',
      displayName: 'Finance',
      roles: ['finance'],
      permissions: ['reconciliation:read', 'reconciliation:write', 'exports:write'],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    expect(useAuthStore.getState().hasPermission('exports:write')).toBe(true)
    expect(useAuthStore.getState().hasPermission('config:write')).toBe(false)
  })

  it('logout clears user and permissions', () => {
    useAuthStore.getState().setUser({
      id: '1',
      username: 'u',
      displayName: 'U',
      roles: ['admin'],
      permissions: ['catalog:read'],
      forcePasswordReset: false,
      mfaEnrolled: false,
      mfaVerified: false,
    })
    expect(useAuthStore.getState().isAdmin()).toBe(true)
    useAuthStore.getState().setUser(null)
    expect(useAuthStore.getState().user).toBeNull()
    expect(useAuthStore.getState().hasPermission('catalog:read')).toBe(false)
    expect(useAuthStore.getState().isAdmin()).toBe(false)
  })
})
