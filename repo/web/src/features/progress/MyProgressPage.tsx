// MyProgressPage — learner resume state and learning activity.
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, PortalApiError } from '../../app/api/client'
import {
  GraduationCap,
  PlayCircle,
  Loader2,
  AlertTriangle,
  BookOpen,
  Map,
  ArrowRight,
  Clock,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────────────────────

interface InProgressItem {
  resource_id: string
  title?: string
  content_type?: string
  status: string
  progress_pct?: number
  last_position_seconds?: number
  last_active_at?: string
}

// ResumeStateResponse matches handler.GetResumeState which returns { in_progress: [...] }
interface ResumeStateResponse {
  in_progress: InProgressItem[]
}

interface Enrollment {
  enrollment_id: string
  path_id: string
  title: string
  status: 'active' | 'completed' | string
  enrolled_at: string
  completed_at?: string
  progress_pct: number
  item_count: number
}

interface EnrollmentsResponse {
  enrollments: Enrollment[]
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badge
// ─────────────────────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { label: string; className: string }> = {
    in_progress: {
      label: 'In Progress',
      className: 'bg-blue-500/10 text-blue-700 dark:text-blue-400',
    },
    completed: {
      label: 'Completed',
      className: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
    },
    not_started: {
      label: 'Not Started',
      className: 'bg-muted text-muted-foreground',
    },
  }

  const { label, className } = config[status] ?? {
    label: status,
    className: 'bg-muted text-muted-foreground',
  }

  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${className}`}>
      {label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Progress bar
// ─────────────────────────────────────────────────────────────────────────────

function ProgressBar({ pct }: { pct: number }) {
  const clamped = Math.max(0, Math.min(100, pct))
  return (
    <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
      <div
        className="h-full rounded-full bg-primary transition-all duration-300"
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Resume card
// ─────────────────────────────────────────────────────────────────────────────

function ResumeCard({ item, onContinue }: { item: InProgressItem; onContinue: () => void }) {
  const title = item.title ?? `Resource ${item.resource_id}`
  const hasPct = typeof item.progress_pct === 'number'

  return (
    <div className="group rounded-xl border bg-card p-5 transition-shadow hover:shadow-sm">
      <div className="flex items-start gap-4">
        <div className="shrink-0 mt-0.5 rounded-lg bg-primary/10 p-2.5">
          <BookOpen size={16} className="text-primary" />
        </div>
        <div className="flex-1 min-w-0">
          <h3 className="font-semibold text-sm leading-tight">{title}</h3>
          <div className="mt-1.5 flex items-center gap-2">
            <StatusBadge status={item.status} />
            {item.last_active_at && (
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <Clock size={10} />
                {formatTimeAgo(new Date(item.last_active_at))}
              </span>
            )}
          </div>
          {hasPct && (
            <div className="mt-2.5 space-y-1">
              <ProgressBar pct={item.progress_pct!} />
              <p className="text-xs text-muted-foreground">{Math.round(item.progress_pct!)}% complete</p>
            </div>
          )}
        </div>
        <button
          onClick={onContinue}
          className="shrink-0 inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <PlayCircle size={12} />
          Continue
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main page
// ─────────────────────────────────────────────────────────────────────────────

export function MyProgressPage() {
  const navigate = useNavigate()

  const resumeQuery = useQuery({
    queryKey: ['me', 'progress'],
    queryFn: () => api.get<ResumeStateResponse>('/me/progress'),
  })

  const enrollQuery = useQuery({
    queryKey: ['me', 'enrollments'],
    queryFn: () => api.get<EnrollmentsResponse>('/me/enrollments'),
  })

  const inProgressItems = (resumeQuery.data?.in_progress ?? []).slice(0, 3)
  const enrollments = enrollQuery.data?.enrollments ?? []

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {/* Page header */}
      <div className="border-b bg-card/50 px-6 py-5">
        <div className="max-w-3xl mx-auto flex items-center gap-3">
          <div className="rounded-lg bg-primary/10 p-2">
            <GraduationCap size={20} className="text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">My Progress</h1>
            <p className="text-xs text-muted-foreground mt-0.5">
              Track your learning activity and resume where you left off
            </p>
          </div>
        </div>
      </div>

      <div className="max-w-3xl mx-auto px-6 py-6 space-y-8">

        {/* Resume section */}
        <section>
          <div className="mb-4">
            <h2 className="text-base font-semibold">Continue where you left off</h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              Your most recently active resources
            </p>
          </div>

          {resumeQuery.isLoading && (
            <div className="flex items-center justify-center h-32 text-muted-foreground">
              <Loader2 size={18} className="animate-spin mr-2" />
              <span className="text-sm">Loading progress...</span>
            </div>
          )}

          {resumeQuery.isError && (
            <div className="flex items-center justify-center h-32 gap-2 text-destructive">
              <AlertTriangle size={15} />
              <span className="text-sm">
                {resumeQuery.error instanceof PortalApiError
                  ? resumeQuery.error.error.message
                  : 'Failed to load progress.'}
              </span>
            </div>
          )}

          {!resumeQuery.isLoading && !resumeQuery.isError && inProgressItems.length === 0 && (
            <div className="flex flex-col items-center justify-center py-12 text-center rounded-xl border bg-card/50">
              <div className="rounded-full bg-muted p-4 mb-3">
                <BookOpen size={20} className="text-muted-foreground" />
              </div>
              <p className="font-medium text-sm">Nothing in progress yet</p>
              <p className="text-xs text-muted-foreground mt-1 max-w-xs">
                Start exploring the library to begin your learning journey.
              </p>
              <button
                onClick={() => navigate('/library')}
                className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                <BookOpen size={12} />
                Browse library
              </button>
            </div>
          )}

          {inProgressItems.length > 0 && (
            <div className="space-y-3">
              {inProgressItems.map((item) => (
                <ResumeCard
                  key={item.resource_id}
                  item={item}
                  onContinue={() => navigate(`/library?resource=${item.resource_id}`)}
                />
              ))}
            </div>
          )}
        </section>

        {/* Enrolled paths section — shows ONLY paths the user has enrolled in,
          fetched from GET /me/enrollments (backed by learning_enrollments). */}
        <section>
          <div className="mb-4">
            <h2 className="text-base font-semibold">My Enrolled Paths</h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              Learning paths you have enrolled in
            </p>
          </div>

          {enrollQuery.isLoading && (
            <div className="flex items-center justify-center h-32 text-muted-foreground">
              <Loader2 size={18} className="animate-spin mr-2" />
              <span className="text-sm">Loading enrolled paths...</span>
            </div>
          )}

          {enrollQuery.isError && (
            <div className="flex items-center justify-center h-32 gap-2 text-destructive">
              <AlertTriangle size={15} />
              <span className="text-sm">Failed to load enrolled paths.</span>
            </div>
          )}

          {!enrollQuery.isLoading && !enrollQuery.isError && enrollments.length === 0 && (
            <div className="flex flex-col items-center justify-center py-12 text-center rounded-xl border bg-card/50">
              <div className="rounded-full bg-muted p-4 mb-3">
                <Map size={20} className="text-muted-foreground" />
              </div>
              <p className="font-medium text-sm">Not enrolled in any paths</p>
              <p className="text-xs text-muted-foreground mt-1 max-w-xs">
                Enroll in a learning path to start tracking your progress here.
              </p>
              <button
                onClick={() => navigate('/paths')}
                className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                <Map size={12} />
                Browse paths
              </button>
            </div>
          )}

          {enrollments.length > 0 && (
            <div className="space-y-3">
              {enrollments.map((e) => (
                <div key={e.enrollment_id} className="rounded-xl border bg-card p-5">
                  <div className="flex items-start gap-4">
                    <div className="shrink-0 rounded-lg bg-primary/10 p-2.5">
                      <Map size={16} className="text-primary" />
                    </div>
                    <div className="flex-1 min-w-0">
                      <h3 className="font-semibold text-sm leading-tight">{e.title}</h3>
                      <div className="mt-1.5 flex items-center gap-2">
                        <StatusBadge status={e.status} />
                        <span className="text-xs text-muted-foreground">{e.item_count} items</span>
                        <span className="text-xs text-muted-foreground">
                          Enrolled {new Date(e.enrolled_at).toLocaleDateString()}
                        </span>
                      </div>
                      <div className="mt-2.5 space-y-1">
                        <ProgressBar pct={e.progress_pct} />
                        <p className="text-xs text-muted-foreground">{Math.round(e.progress_pct)}% complete</p>
                      </div>
                    </div>
                    <button
                      onClick={() => navigate(`/paths/${e.path_id}`)}
                      className="shrink-0 inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors hover:bg-accent"
                    >
                      <ArrowRight size={12} />
                      View
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Utilities
// ─────────────────────────────────────────────────────────────────────────────

function formatTimeAgo(date: Date): string {
  const now = new Date()
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}
