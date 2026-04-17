// Component tests for LoginPage — form validation, submission, MFA step.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from '../../features/auth/LoginPage'
import { useAuthStore } from '../../app/store'

// Mock the api client module
vi.mock('../../app/api/client', async () => {
  const actual = await vi.importActual<any>('../../app/api/client')
  return {
    ...actual,
    api: {
      post: vi.fn(),
      get: vi.fn(),
    },
  }
})

import { api } from '../../app/api/client'

function renderLogin() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: null,
      isLoading: false,
      compatibilityMode: null,
    })
    vi.resetAllMocks()
  })

  it('renders Welcome back heading', () => {
    renderLogin()
    expect(screen.getByRole('heading', { name: /welcome back/i })).toBeDefined()
  })

  it('renders username and password inputs', () => {
    renderLogin()
    expect(screen.getByLabelText(/username/i)).toBeDefined()
    expect(screen.getByLabelText(/password/i)).toBeDefined()
  })

  it('submit button is visible', () => {
    renderLogin()
    expect(screen.getByRole('button', { name: /sign in/i })).toBeDefined()
  })

  it('empty form submit shows validation errors', async () => {
    const user = userEvent.setup()
    renderLogin()
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => {
      expect(screen.getAllByText(/required/i).length).toBeGreaterThan(0)
    })
  })

  it('calls api.post /auth/login on valid submit', async () => {
    const user = userEvent.setup()
    ;(api.post as any).mockResolvedValue({
      requires_mfa: false,
      user: {
        id: 'u1', username: 'alice', display_name: 'Alice',
        roles: ['learner'], permissions: ['catalog:read'],
        force_password_reset: false, mfa_enrolled: false, mfa_verified: false,
      },
      compatibility_mode: 'full',
    })
    renderLogin()
    await user.type(screen.getByLabelText(/username/i), 'alice')
    await user.type(screen.getByLabelText(/password/i), 'pass123')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => {
      expect(api.post).toHaveBeenCalledWith('/auth/login', {
        username: 'alice',
        password: 'pass123',
      })
    })
  })

  it('switches to MFA step when requires_mfa is true', async () => {
    const user = userEvent.setup()
    ;(api.post as any).mockResolvedValue({
      requires_mfa: true,
      compatibility_mode: 'full',
    })
    renderLogin()
    await user.type(screen.getByLabelText(/username/i), 'alice')
    await user.type(screen.getByLabelText(/password/i), 'pass123')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => {
      expect(screen.getByText(/6-digit code/i)).toBeDefined()
    })
  })

  it('displays API error message on login failure', async () => {
    const user = userEvent.setup()
    const { PortalApiError } = await import('../../app/api/client')
    ;(api.post as any).mockRejectedValue(
      new PortalApiError(401, { code: 'auth.unauthenticated', message: 'Invalid credentials' }),
    )
    renderLogin()
    await user.type(screen.getByLabelText(/username/i), 'alice')
    await user.type(screen.getByLabelText(/password/i), 'wrong')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => {
      expect(screen.getByText(/invalid credentials/i)).toBeDefined()
    })
  })
})
