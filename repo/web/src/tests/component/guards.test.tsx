// Component tests for RequireAuth guard — verifies blocked redirect,
// read-only state exposure, and permission gating behavior.
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { RequireAuth, useIsReadOnly } from '../../app/guards'
import { useAuthStore, type AuthUser } from '../../app/store'

// Helper to set store state before each test.
function setStoreState(overrides: {
  user?: AuthUser | null
  isLoading?: boolean
  compatibilityMode?: 'full' | 'read_only' | 'blocked' | 'warn' | null
}) {
  useAuthStore.setState({
    user: overrides.user ?? null,
    isLoading: overrides.isLoading ?? false,
    compatibilityMode: overrides.compatibilityMode ?? null,
  })
}

const baseUser: AuthUser = {
  id: '1',
  username: 'testuser',
  displayName: 'Test User',
  roles: ['learner'],
  permissions: ['catalog:read'],
  forcePasswordReset: false,
  mfaEnrolled: false,
  mfaVerified: false,
}

describe('RequireAuth guard', () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: null,
      isLoading: false,
      compatibilityMode: null,
    })
  })

  it('redirects to /login when no user', () => {
    setStoreState({ user: null })
    render(
      <MemoryRouter initialEntries={['/protected']}>
        <Routes>
          <Route path="/protected" element={
            <RequireAuth><div>Protected</div></RequireAuth>
          } />
          <Route path="/login" element={<div>Login Page</div>} />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Login Page')).toBeDefined()
  })

  it('redirects to /version-blocked when compatibilityMode is blocked', () => {
    setStoreState({ user: baseUser, compatibilityMode: 'blocked' })
    render(
      <MemoryRouter initialEntries={['/protected']}>
        <Routes>
          <Route path="/protected" element={
            <RequireAuth><div>Protected</div></RequireAuth>
          } />
          <Route path="/version-blocked" element={<div>Version Blocked</div>} />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Version Blocked')).toBeDefined()
  })

  it('redirects to /forbidden when required permission missing', () => {
    setStoreState({ user: baseUser })
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <Routes>
          <Route path="/admin" element={
            <RequireAuth requiredPermission="admin:everything"><div>Admin</div></RequireAuth>
          } />
          <Route path="/forbidden" element={<div>Access Denied</div>} />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Access Denied')).toBeDefined()
  })

  it('redirects to /forbidden when required role missing', () => {
    setStoreState({ user: baseUser })
    render(
      <MemoryRouter initialEntries={['/admin']}>
        <Routes>
          <Route path="/admin" element={
            <RequireAuth requiredRole="admin"><div>Admin</div></RequireAuth>
          } />
          <Route path="/forbidden" element={<div>Access Denied</div>} />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Access Denied')).toBeDefined()
  })

  it('renders children when user has required permission', () => {
    setStoreState({ user: baseUser })
    render(
      <MemoryRouter initialEntries={['/lib']}>
        <Routes>
          <Route path="/lib" element={
            <RequireAuth requiredPermission="catalog:read"><div>Library</div></RequireAuth>
          } />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Library')).toBeDefined()
  })

  it('renders children in read_only mode (does not block navigation)', () => {
    setStoreState({ user: baseUser, compatibilityMode: 'read_only' })
    render(
      <MemoryRouter initialEntries={['/lib']}>
        <Routes>
          <Route path="/lib" element={
            <RequireAuth requiredPermission="catalog:read"><div>Library</div></RequireAuth>
          } />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Library')).toBeDefined()
  })

  it('shows loading spinner when isLoading', () => {
    setStoreState({ isLoading: true })
    const { container } = render(
      <MemoryRouter initialEntries={['/protected']}>
        <Routes>
          <Route path="/protected" element={
            <RequireAuth><div>Protected</div></RequireAuth>
          } />
        </Routes>
      </MemoryRouter>
    )
    // The spinner has animate-spin class.
    const spinner = container.querySelector('.animate-spin')
    expect(spinner).not.toBeNull()
  })

  it('allows when user has any of requiredAnyPermission', () => {
    setStoreState({
      user: { ...baseUser, permissions: ['appeals:write'] },
    })
    render(
      <MemoryRouter initialEntries={['/disputes']}>
        <Routes>
          <Route path="/disputes" element={
            <RequireAuth requiredAnyPermission={['appeals:write', 'appeals:decide']}>
              <div>Disputes</div>
            </RequireAuth>
          } />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Disputes')).toBeDefined()
  })

  it('redirects when user has none of requiredAnyPermission', () => {
    setStoreState({ user: baseUser })
    render(
      <MemoryRouter initialEntries={['/disputes']}>
        <Routes>
          <Route path="/disputes" element={
            <RequireAuth requiredAnyPermission={['appeals:write', 'appeals:decide']}>
              <div>Disputes</div>
            </RequireAuth>
          } />
          <Route path="/forbidden" element={<div>Access Denied</div>} />
        </Routes>
      </MemoryRouter>
    )
    expect(screen.getByText('Access Denied')).toBeDefined()
  })
})

describe('useIsReadOnly', () => {
  function ReadOnlyIndicator() {
    const isReadOnly = useIsReadOnly()
    return <div>{isReadOnly ? 'READ_ONLY' : 'WRITABLE'}</div>
  }

  beforeEach(() => {
    useAuthStore.setState({ compatibilityMode: null })
  })

  it('returns true when compatibilityMode is read_only', () => {
    useAuthStore.setState({ compatibilityMode: 'read_only' })
    render(
      <MemoryRouter>
        <ReadOnlyIndicator />
      </MemoryRouter>
    )
    expect(screen.getByText('READ_ONLY')).toBeDefined()
  })

  it('returns false when compatibilityMode is full', () => {
    useAuthStore.setState({ compatibilityMode: 'full' })
    render(
      <MemoryRouter>
        <ReadOnlyIndicator />
      </MemoryRouter>
    )
    expect(screen.getByText('WRITABLE')).toBeDefined()
  })

  it('returns false when compatibilityMode is null', () => {
    useAuthStore.setState({ compatibilityMode: null })
    render(
      <MemoryRouter>
        <ReadOnlyIndicator />
      </MemoryRouter>
    )
    expect(screen.getByText('WRITABLE')).toBeDefined()
  })

  it('returns false when compatibilityMode is blocked', () => {
    useAuthStore.setState({ compatibilityMode: 'blocked' })
    render(
      <MemoryRouter>
        <ReadOnlyIndicator />
      </MemoryRouter>
    )
    expect(screen.getByText('WRITABLE')).toBeDefined()
  })
})

// ── Write-action suppression in read_only mode ──────────────────────────────
// Mirrors the pattern in ModerationPage.tsx:238 where action buttons are
// conditionally rendered: {!isReadOnly && item.status === 'pending' ? <buttons> : <fallback>}

describe('Write-action suppression in read_only mode', () => {
  /** Mimics the ModerationPage QueueRow pattern exactly. */
  function WriteCapableRow({ status }: { status: 'pending' | 'decided' }) {
    const isReadOnly = useIsReadOnly()
    return (
      <div>
        {!isReadOnly && status === 'pending' ? (
          <div>
            <button>Approve</button>
            <button>Reject</button>
            <button>Escalate</button>
          </div>
        ) : (
          <span>No actions available</span>
        )}
      </div>
    )
  }

  beforeEach(() => {
    useAuthStore.setState({ compatibilityMode: null })
  })

  it('shows write actions when not in read_only mode', () => {
    useAuthStore.setState({ compatibilityMode: null })
    render(
      <MemoryRouter>
        <WriteCapableRow status="pending" />
      </MemoryRouter>
    )
    expect(screen.getByText('Approve')).toBeDefined()
    expect(screen.getByText('Reject')).toBeDefined()
    expect(screen.getByText('Escalate')).toBeDefined()
  })

  it('hides write actions when in read_only mode', () => {
    useAuthStore.setState({ compatibilityMode: 'read_only' })
    render(
      <MemoryRouter>
        <WriteCapableRow status="pending" />
      </MemoryRouter>
    )
    expect(screen.queryByText('Approve')).toBeNull()
    expect(screen.queryByText('Reject')).toBeNull()
    expect(screen.queryByText('Escalate')).toBeNull()
    expect(screen.getByText('No actions available')).toBeDefined()
  })

  it('shows fallback for non-pending even when writable', () => {
    useAuthStore.setState({ compatibilityMode: null })
    render(
      <MemoryRouter>
        <WriteCapableRow status="decided" />
      </MemoryRouter>
    )
    expect(screen.queryByText('Approve')).toBeNull()
    expect(screen.getByText('No actions available')).toBeDefined()
  })

  it('shows write actions in full compatibility mode', () => {
    useAuthStore.setState({ compatibilityMode: 'full' })
    render(
      <MemoryRouter>
        <WriteCapableRow status="pending" />
      </MemoryRouter>
    )
    expect(screen.getByText('Approve')).toBeDefined()
  })
})
