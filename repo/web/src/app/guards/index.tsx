// Route guards — protect routes based on authentication and permissions.
import { Navigate, useLocation } from 'react-router-dom'
import { useAuthStore } from '../store'

interface RequireAuthProps {
  children: React.ReactNode
  requiredPermission?: string
  /** When set, the user must hold at least ONE of the listed permissions. */
  requiredAnyPermission?: string[]
  requiredRole?: string
}

export function RequireAuth({ children, requiredPermission, requiredAnyPermission, requiredRole }: RequireAuthProps) {
  const { user, isLoading, compatibilityMode } = useAuthStore()
  const location = useLocation()

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  if (compatibilityMode === 'blocked') {
    return <Navigate to="/version-blocked" replace />
  }

  if (requiredPermission && !user.permissions.includes(requiredPermission)) {
    return <Navigate to="/forbidden" replace />
  }

  if (requiredAnyPermission && requiredAnyPermission.length > 0) {
    const hasAny = requiredAnyPermission.some((p) => user.permissions.includes(p))
    if (!hasAny) {
      return <Navigate to="/forbidden" replace />
    }
  }

  if (requiredRole && !user.roles.includes(requiredRole)) {
    return <Navigate to="/forbidden" replace />
  }

  return <>{children}</>
}

// Block write actions in read-only compatibility mode
export function useIsReadOnly(): boolean {
  const { compatibilityMode } = useAuthStore()
  return compatibilityMode === 'read_only'
}
