// Component tests for AppLayout sidebar — role-based navigation visibility.
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AppLayout } from '../../app/layout/AppLayout'
import { useAuthStore, type AuthUser } from '../../app/store'

function setUser(user: AuthUser | null) {
  useAuthStore.setState({
    user,
    isLoading: false,
    compatibilityMode: 'full',
  })
}

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={['/library']}>
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/library" element={<div>library content</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('AppLayout sidebar visibility', () => {
  beforeEach(() => {
    setUser(null)
  })

  it('admin user does not see My Learning or My Progress', () => {
    setUser({
      id: 'a', username: 'admin', displayName: 'Admin',
      roles: ['admin'], permissions: ['catalog:read', 'learning:enroll', 'learning:progress'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.queryByRole('link', { name: /my learning/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /my progress/i })).toBeNull()
  })

  it('learner sees My Learning and My Progress', () => {
    setUser({
      id: 'l', username: 'learner', displayName: 'Learner',
      roles: ['learner'], permissions: ['catalog:read', 'learning:enroll', 'learning:progress'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.getByRole('link', { name: /my learning/i })).toBeDefined()
    expect(screen.getByRole('link', { name: /my progress/i })).toBeDefined()
  })

  it('finance user sees Disputes via appeals:decide permission', () => {
    setUser({
      id: 'f', username: 'finance', displayName: 'Finance',
      roles: ['finance'], permissions: ['reconciliation:read', 'appeals:decide'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.getByRole('link', { name: /disputes/i })).toBeDefined()
  })

  it('procurement user sees Disputes via appeals:write', () => {
    setUser({
      id: 'p', username: 'proc', displayName: 'Proc',
      roles: ['procurement'], permissions: ['orders:read', 'appeals:write'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.getByRole('link', { name: /disputes/i })).toBeDefined()
  })

  it('learner does not see admin Config link', () => {
    setUser({
      id: 'l', username: 'learner', displayName: 'Learner',
      roles: ['learner'], permissions: ['catalog:read'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.queryByRole('link', { name: /^config$/i })).toBeNull()
  })

  it('admin sees Config link', () => {
    setUser({
      id: 'a', username: 'admin', displayName: 'Admin',
      roles: ['admin'], permissions: ['catalog:read'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.getByRole('link', { name: /^config$/i })).toBeDefined()
  })

  it('user without users:read does not see Users link', () => {
    setUser({
      id: 'l', username: 'learner', displayName: 'Learner',
      roles: ['learner'], permissions: ['catalog:read'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.queryByRole('link', { name: /^users$/i })).toBeNull()
  })

  it('moderator sees Moderation link', () => {
    setUser({
      id: 'm', username: 'mod', displayName: 'Mod',
      roles: ['moderator'], permissions: ['catalog:read', 'moderation:write'],
      forcePasswordReset: false, mfaEnrolled: false, mfaVerified: true,
    })
    renderLayout()
    expect(screen.getByRole('link', { name: /moderation/i })).toBeDefined()
  })
})
