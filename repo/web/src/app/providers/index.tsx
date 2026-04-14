// App-level React providers.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { type ReactNode, useEffect } from 'react'
import { useAuthStore } from '../store'
import { api, PortalApiError } from '../api/client'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      retry: (failureCount, error) => {
        // Never retry auth errors
        if (error instanceof PortalApiError && (error.isUnauthorized || error.isForbidden)) {
          return false
        }
        return failureCount < 2
      },
    },
  },
})

interface SessionResponse {
  user: {
    id: string
    username: string
    display_name: string
    roles: string[]
    permissions: string[]
    force_password_reset: boolean
    mfa_enrolled: boolean
    mfa_verified: boolean
  }
  compatibility_mode: 'full' | 'read_only' | 'blocked' | 'warn'
}

function SessionLoader({ children }: { children: ReactNode }) {
  const { setUser, setLoading, setCompatibilityMode } = useAuthStore()

  useEffect(() => {
    api.get<SessionResponse>('/session')
      .then((res) => {
        setUser({
          id:                 res.user.id,
          username:           res.user.username,
          displayName:        res.user.display_name,
          roles:              res.user.roles,
          permissions:        res.user.permissions,
          forcePasswordReset: res.user.force_password_reset,
          mfaEnrolled:        res.user.mfa_enrolled,
          mfaVerified:        res.user.mfa_verified,
        })
        setCompatibilityMode(res.compatibility_mode)
      })
      .catch((err) => {
        if (err instanceof PortalApiError && err.isUnauthorized) {
          setUser(null)
        }
      })
      .finally(() => setLoading(false))
  }, [setUser, setLoading, setCompatibilityMode])

  return <>{children}</>
}

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <SessionLoader>
        {children}
      </SessionLoader>
    </QueryClientProvider>
  )
}
