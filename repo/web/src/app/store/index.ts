// Zustand stores for client-side state.
import { create } from 'zustand'

// ── Auth / session shell state ───────────────────────────────────────────────

export interface AuthUser {
  id: string
  username: string
  displayName: string
  roles: string[]
  permissions: string[]
  forcePasswordReset: boolean
  mfaEnrolled: boolean
  mfaVerified: boolean
}

interface AuthState {
  user: AuthUser | null
  isLoading: boolean
  compatibilityMode: 'full' | 'read_only' | 'blocked' | 'warn' | null
  setUser: (user: AuthUser | null) => void
  setLoading: (loading: boolean) => void
  setCompatibilityMode: (mode: AuthState['compatibilityMode']) => void
  hasPermission: (code: string) => boolean
  hasRole: (role: string) => boolean
  isAdmin: () => boolean
}

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  isLoading: true,
  compatibilityMode: null,

  setUser: (user) => set({ user }),
  setLoading: (isLoading) => set({ isLoading }),
  setCompatibilityMode: (compatibilityMode) => set({ compatibilityMode }),

  hasPermission: (code) => {
    const { user } = get()
    if (!user) return false
    return user.permissions.includes(code)
  },

  hasRole: (role) => {
    const { user } = get()
    if (!user) return false
    return user.roles.includes(role)
  },

  isAdmin: () => {
    const { user } = get()
    if (!user) return false
    return user.roles.includes('admin')
  },
}))

// ── UI shell state ────────────────────────────────────────────────────────────

interface UIState {
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  toggleSidebar: () => void
}

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen: true,
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),
}))
