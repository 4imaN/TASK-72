// AppLayout — main shell with sidebar navigation and role-based menu items.
import { useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuthStore, useUIStore } from '../store'
import { api } from '../api/client'
import {
  BookOpen, ShoppingCart, DollarSign, Shield, Settings,
  Users, BarChart2, Flag, Menu, LogOut, Loader2,
} from 'lucide-react'

interface NavItem {
  label: string
  path: string
  icon: React.ReactNode
  permission?: string
  /** Show the item when the user holds ANY of the listed permissions. */
  anyPermission?: string[]
  role?: string
  /** Hide this item when the user holds ANY of these roles. Prevents
   *  admin from seeing learner-personal pages they'd never use. */
  hideForRoles?: string[]
}

const ALL_NAV_ITEMS: NavItem[] = [
  // Learner — personal study pages hidden from admin (admin manages, doesn't enroll)
  { label: 'Library',          path: '/library',              icon: <BookOpen size={18} />,    permission: 'catalog:read' },
  { label: 'Archive',          path: '/archive',              icon: <BookOpen size={18} />,    permission: 'catalog:read' },
  { label: 'My Learning',      path: '/paths',                icon: <BarChart2 size={18} />,   permission: 'learning:enroll',   hideForRoles: ['admin'] },
  { label: 'My Progress',      path: '/me/progress',          icon: <BarChart2 size={18} />,   permission: 'learning:progress', hideForRoles: ['admin'] },
  // Procurement
  { label: 'Orders',           path: '/procurement/orders',   icon: <ShoppingCart size={18} />,permission: 'orders:read' },
  { label: 'Disputes',         path: '/disputes',             icon: <Flag size={18} />,        anyPermission: ['appeals:write', 'appeals:decide'] },
  // Moderation
  { label: 'Moderation',       path: '/moderation/reviews',   icon: <Shield size={18} />,      permission: 'moderation:write' },
  // Finance
  { label: 'Reconciliation',   path: '/finance/reconciliation',icon: <DollarSign size={18} />, permission: 'reconciliation:read' },
  { label: 'Settlements',      path: '/finance/settlements',  icon: <DollarSign size={18} />,  permission: 'settlements:write' },
  { label: 'Approvals',        path: '/approvals',            icon: <Shield size={18} />,      permission: 'appeals:decide' },
  // Admin
  { label: 'Taxonomy',         path: '/admin/taxonomy',       icon: <BookOpen size={18} />,    role: 'admin' },
  { label: 'Config',           path: '/admin/config',         icon: <Settings size={18} />,    role: 'admin' },
  { label: 'Users',            path: '/admin/users',          icon: <Users size={18} />,       permission: 'users:read' },
  { label: 'Audit Log',        path: '/admin/audit',          icon: <Shield size={18} />,      permission: 'audit:read' },
]

export function AppLayout() {
  const { user, hasPermission, hasRole } = useAuthStore()
  const { sidebarOpen, toggleSidebar } = useUIStore()
  const location = useLocation()
  const navigate = useNavigate()
  const [loggingOut, setLoggingOut] = useState(false)

  async function handleLogout() {
    setLoggingOut(true)
    try {
      await api.post('/auth/logout')
    } catch {
      // ignore logout errors — clear session regardless
    } finally {
      useAuthStore.getState().setUser(null)
      navigate('/login')
    }
  }

  const visibleItems = ALL_NAV_ITEMS.filter((item) => {
    // Hide items that don't make sense for certain roles (e.g. personal
    // learner pages for an admin who manages the system, not studies in it)
    if (item.hideForRoles && user?.roles?.some((r) => item.hideForRoles!.includes(r))) {
      return false
    }
    if (item.role)           return hasRole(item.role)
    if (item.anyPermission)  return item.anyPermission.some((p) => hasPermission(p))
    if (item.permission)     return hasPermission(item.permission)
    return true
  })

  return (
    <div className="flex h-screen overflow-hidden" style={{ background: '#0c0f16' }}>
      {/* Sidebar — deep navy gradient with warm amber active states */}
      <aside
        className={`flex flex-col transition-all duration-200 ${
          sidebarOpen ? 'w-60' : 'w-[56px]'
        }`}
        style={{
          background: 'linear-gradient(180deg, #111827 0%, #0d1117 100%)',
          borderRight: '1px solid rgba(255,255,255,0.06)',
        }}
      >
        {/* Logo bar */}
        <div className="flex h-14 items-center justify-between px-3" style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
          {sidebarOpen && (
            <div className="flex items-center gap-2">
              <div className="h-7 w-7 rounded-lg flex items-center justify-center" style={{ background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' }}>
                <BookOpen size={14} className="text-white" />
              </div>
              <span className="text-sm font-bold tracking-tight text-white">Portal</span>
            </div>
          )}
          <button
            onClick={toggleSidebar}
            className="rounded-md p-1.5 text-zinc-400 hover:text-white hover:bg-white/8 transition-colors"
            aria-label="Toggle sidebar"
          >
            <Menu size={16} />
          </button>
        </div>

        {/* Nav items */}
        <nav className="flex-1 overflow-y-auto py-3 px-2 space-y-0.5">
          {visibleItems.map((item) => {
            const active = location.pathname.startsWith(item.path)
            return (
              <Link
                key={item.path}
                to={item.path}
                className={`flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-all duration-150 ${
                  active
                    ? 'text-amber-300 shadow-sm'
                    : 'text-zinc-400 hover:text-zinc-200 hover:bg-white/5'
                }`}
                style={active ? {
                  background: 'linear-gradient(135deg, rgba(245,158,11,0.15) 0%, rgba(217,119,6,0.08) 100%)',
                  boxShadow: 'inset 0 0 0 1px rgba(245,158,11,0.2)',
                } : undefined}
                title={!sidebarOpen ? item.label : undefined}
              >
                <span className="flex-shrink-0">{item.icon}</span>
                {sidebarOpen && <span className="truncate">{item.label}</span>}
              </Link>
            )
          })}
        </nav>

        {/* User footer */}
        <div className="p-3" style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}>
          {sidebarOpen && user && (
            <div className="mb-2.5 flex items-center gap-2">
              <div className="h-7 w-7 rounded-full flex items-center justify-center text-[10px] font-bold text-white" style={{ background: 'linear-gradient(135deg, #6366f1 0%, #8b5cf6 100%)' }}>
                {(user.displayName || 'U')[0].toUpperCase()}
              </div>
              <div className="min-w-0 flex-1">
                <p className="text-xs font-medium text-zinc-200 truncate">{user.displayName}</p>
                <p className="text-[10px] text-zinc-500 truncate">{user.roles?.[0] || 'user'}</p>
              </div>
            </div>
          )}
          <button
            onClick={handleLogout}
            disabled={loggingOut}
            className="flex items-center gap-2 text-xs text-zinc-500 hover:text-red-400 transition-colors disabled:opacity-50"
            title="Sign out"
          >
            {loggingOut ? <Loader2 size={13} className="animate-spin" /> : <LogOut size={13} />}
            {sidebarOpen && (loggingOut ? 'Signing out...' : 'Sign out')}
          </button>
        </div>
      </aside>

      {/* Main content — slightly lighter than sidebar for contrast */}
      <main className="flex-1 overflow-y-auto" style={{ background: '#0f1219' }}>
        <Outlet />
      </main>
    </div>
  )
}
