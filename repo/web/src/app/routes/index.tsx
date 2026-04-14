// Route configuration — maps paths to features with permission guards.
import { Routes, Route, Navigate } from 'react-router-dom'
import { Suspense, lazy } from 'react'
import { AppLayout } from '../layout/AppLayout'
import { RequireAuth } from '../guards'
import { LoginPage } from '../../features/auth/LoginPage'
import { PasswordChangePage } from '../../features/auth/PasswordChangePage'
import { MFASetupPage } from '../../features/auth/MFASetupPage'

// Lazy-load feature pages — will be implemented in subsequent slices.
const LibraryPage          = lazy(() => import('../../features/library/LibraryPage').then(m => ({ default: m.LibraryPage })))
const LearningPathsPage    = lazy(() => import('../../features/learning-paths/LearningPathsPage').then(m => ({ default: m.LearningPathsPage })))
const PathDetailPage       = lazy(() => import('../../features/learning-paths/PathDetailPage').then(m => ({ default: m.PathDetailPage })))
const ProcurementPage      = lazy(() => import('../../features/procurement/ProcurementPage').then(m => ({ default: m.ProcurementPage })))
const DisputesPage         = lazy(() => import('../../features/disputes/DisputesPage').then(m => ({ default: m.DisputesPage })))
const ApprovalsPage        = lazy(() => import('../../features/approvals/ApprovalsPage').then(m => ({ default: m.ApprovalsPage })))
const FinancePage          = lazy(() => import('../../features/finance/FinancePage').then(m => ({ default: m.FinancePage })))
const ModerationPage       = lazy(() => import('../../features/moderation/ModerationPage').then(m => ({ default: m.ModerationPage })))
const AdminPage            = lazy(() => import('../../features/admin/AdminPage').then(m => ({ default: m.AdminPage })))
const MyProgressPage       = lazy(() => import('../../features/progress/MyProgressPage').then(m => ({ default: m.MyProgressPage })))
const ArchivePage          = lazy(() => import('../../features/archive/ArchivePage').then(m => ({ default: m.ArchivePage })))

function PageSkeleton() {
  return (
    <div className="flex h-64 items-center justify-center text-muted-foreground text-sm">
      Loading...
    </div>
  )
}

function NotFound() {
  return (
    <div className="flex h-64 items-center justify-center flex-col gap-2">
      <p className="text-lg font-medium">404 — Page not found</p>
      <Navigate to="/" replace />
    </div>
  )
}

function Forbidden() {
  return (
    <div className="flex h-64 items-center justify-center flex-col gap-2">
      <p className="text-lg font-medium">403 — Access denied</p>
      <p className="text-sm text-muted-foreground">You do not have permission to view this page.</p>
    </div>
  )
}

function VersionBlocked() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/40 p-4">
      <div className="max-w-sm text-center">
        <p className="text-lg font-semibold text-destructive">Unsupported client version</p>
        <p className="mt-2 text-sm text-muted-foreground">
          This version of the portal is no longer supported. Please refresh or contact your administrator.
        </p>
      </div>
    </div>
  )
}

export function AppRoutes() {
  return (
    <Suspense fallback={<PageSkeleton />}>
      <Routes>
        {/* Public */}
        <Route path="/login"           element={<LoginPage />} />
        <Route path="/version-blocked" element={<VersionBlocked />} />
        <Route path="/forbidden"       element={<Forbidden />} />

        {/* Protected — wrapped in AppLayout */}
        <Route element={<RequireAuth><AppLayout /></RequireAuth>}>
          <Route index element={<Navigate to="/library" replace />} />

          {/* Learner routes */}
          <Route path="/library/*" element={
            <RequireAuth requiredPermission="catalog:read">
              <LibraryPage />
            </RequireAuth>
          } />
          <Route path="/archive" element={
            <RequireAuth requiredPermission="catalog:read">
              <ArchivePage />
            </RequireAuth>
          } />
          <Route path="/paths" element={
            <RequireAuth requiredPermission="learning:enroll">
              <LearningPathsPage />
            </RequireAuth>
          } />
          <Route path="/paths/:id" element={
            <RequireAuth requiredPermission="learning:enroll">
              <PathDetailPage />
            </RequireAuth>
          } />
          <Route path="/me/*" element={
            <RequireAuth requiredPermission="learning:progress">
              <MyProgressPage />
            </RequireAuth>
          } />

          {/* Procurement routes */}
          <Route path="/procurement/*" element={
            <RequireAuth requiredPermission="orders:read">
              <ProcurementPage />
            </RequireAuth>
          } />
          <Route path="/disputes/*" element={
            <RequireAuth requiredAnyPermission={['appeals:write', 'appeals:decide']}>
              <DisputesPage />
            </RequireAuth>
          } />

          {/* Approval */}
          <Route path="/approvals/*" element={
            <RequireAuth requiredPermission="appeals:decide">
              <ApprovalsPage />
            </RequireAuth>
          } />

          {/* Moderation routes */}
          <Route path="/moderation/*" element={
            <RequireAuth requiredPermission="moderation:write">
              <ModerationPage />
            </RequireAuth>
          } />

          {/* Finance routes */}
          <Route path="/finance/*" element={
            <RequireAuth requiredPermission="reconciliation:read">
              <FinancePage />
            </RequireAuth>
          } />

          {/* Admin routes */}
          <Route path="/admin/*" element={
            <RequireAuth requiredRole="admin">
              <AdminPage />
            </RequireAuth>
          } />

          {/* Account — password change (bootstrap rotation + voluntary) */}
          <Route path="/account/security" element={
            <RequireAuth>
              <PasswordChangePage />
            </RequireAuth>
          } />
          <Route path="/account/mfa/setup" element={
            <RequireAuth>
              <MFASetupPage />
            </RequireAuth>
          } />
          <Route path="/account/*" element={
            <RequireAuth>
              <Navigate to="/account/security" replace />
            </RequireAuth>
          } />

          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </Suspense>
  )
}
