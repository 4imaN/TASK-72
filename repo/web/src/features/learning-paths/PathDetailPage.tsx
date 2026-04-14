// PathDetailPage — shows path items, required/elective breakdown, and progress
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '../../app/api/client'
import {
  CheckCircle2,
  Circle,
  Download,
  ArrowLeft,
  Loader2,
  AlertTriangle,
  Trophy,
  BookOpen,
  Layers,
} from 'lucide-react'

interface ProgressSnapshot {
  status: string
  progress_pct: number
  last_position_seconds: number
  last_active_at?: string
}

interface ItemProgress {
  resource_id: string
  title: string
  content_type: string
  item_type: string
  sort_order: number
  progress?: ProgressSnapshot
}

interface PathProgressResponse {
  path: { id: string; title: string; description?: string }
  enrollment: { status: string; enrolled_at: string; completed_at?: string }
  required_items: ItemProgress[]
  elective_items: ItemProgress[]
  completion_ready: boolean
  required_done: number
  elective_done: number
}

export function PathDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['path-progress', id],
    queryFn: () => api.get<PathProgressResponse>(`/paths/${id}/progress`),
    retry: false,
  })

  const downloadCSV = useMutation({
    mutationFn: async () => {
      const res = await fetch('/api/v1/me/exports/csv', { credentials: 'include' })
      if (!res.ok) throw new Error('Export failed')
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `learning-record-${new Date().toISOString().slice(0, 10)}.csv`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        <Loader2 size={20} className="animate-spin mr-2" />
        <span className="text-sm">Loading path details...</span>
      </div>
    )
  }

  if (isError || !data) {
    return (
      <div className="p-6 max-w-3xl mx-auto">
        <button
          onClick={() => navigate('/paths')}
          className="mb-4 inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft size={14} />
          Back to paths
        </button>
        <div className="rounded-xl border border-destructive/30 bg-destructive/5 p-6 text-center">
          <AlertTriangle size={20} className="mx-auto mb-2 text-destructive" />
          <p className="text-sm font-medium">Not enrolled in this path</p>
          <p className="text-xs text-muted-foreground mt-1">
            Enroll in this learning path from the paths list to see your progress.
          </p>
          <button
            onClick={() => navigate('/paths')}
            className="mt-4 inline-flex items-center gap-1 rounded-lg bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90"
          >
            Browse Paths
          </button>
        </div>
      </div>
    )
  }

  const totalRequired = data.required_items.length
  const totalElective = data.elective_items.length
  const requiredPct = totalRequired > 0 ? Math.round((data.required_done / totalRequired) * 100) : 0
  const electivePct = totalElective > 0 ? Math.round((data.elective_done / totalElective) * 100) : 0

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Header */}
      <div className="border-b border-border px-6 py-5">
        <div className="max-w-3xl mx-auto">
          <button
            onClick={() => navigate('/paths')}
            className="mb-3 inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            <ArrowLeft size={14} />
            Back to paths
          </button>
          <div className="flex items-start justify-between gap-4">
            <div>
              <h1 className="text-xl font-semibold tracking-tight">{data.path.title}</h1>
              {data.path.description && (
                <p className="mt-1 text-xs text-muted-foreground leading-relaxed max-w-xl">
                  {data.path.description}
                </p>
              )}
            </div>
            <button
              onClick={() => downloadCSV.mutate()}
              disabled={downloadCSV.isPending}
              className="shrink-0 inline-flex items-center gap-1.5 rounded-lg border bg-background px-3 py-1.5 text-xs font-medium transition-colors hover:bg-accent disabled:opacity-60"
            >
              {downloadCSV.isPending ? (
                <Loader2 size={12} className="animate-spin" />
              ) : (
                <Download size={12} />
              )}
              Export CSV
            </button>
          </div>
        </div>
      </div>

      <div className="max-w-3xl mx-auto px-6 py-6 space-y-6">
        {/* Completion banner */}
        {data.completion_ready && (
          <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/8 px-5 py-4 flex items-center gap-3">
            <Trophy size={18} className="text-emerald-500 shrink-0" />
            <div>
              <p className="text-sm font-semibold text-emerald-700 dark:text-emerald-400">
                Path requirements met
              </p>
              <p className="text-xs text-emerald-600/80 dark:text-emerald-500/80 mt-0.5">
                You have completed all required items and met the elective minimum.
              </p>
            </div>
          </div>
        )}

        {/* Stats grid */}
        <div className="grid grid-cols-3 gap-3">
          <StatCard
            label="Required done"
            value={`${data.required_done} / ${totalRequired}`}
            pct={requiredPct}
            color="blue"
          />
          <StatCard
            label="Electives done"
            value={`${data.elective_done} / ${totalElective}`}
            pct={electivePct}
            color="purple"
          />
          <div className="rounded-xl border bg-card p-4 text-center">
            <p className="text-xs text-muted-foreground mb-1">Enrollment status</p>
            <p className="text-sm font-semibold capitalize">{data.enrollment.status}</p>
            <p className="text-xs text-muted-foreground mt-1">
              Since {new Date(data.enrollment.enrolled_at).toLocaleDateString()}
            </p>
          </div>
        </div>

        {/* Required items */}
        {data.required_items.length > 0 && (
          <section>
            <div className="flex items-center gap-2 mb-3">
              <BookOpen size={14} className="text-muted-foreground" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Required ({data.required_done}/{totalRequired})
              </h2>
            </div>
            <ul className="space-y-2">
              {data.required_items.map((item) => (
                <ItemRow key={item.resource_id} item={item} />
              ))}
            </ul>
          </section>
        )}

        {/* Elective items */}
        {data.elective_items.length > 0 && (
          <section>
            <div className="flex items-center gap-2 mb-3">
              <Layers size={14} className="text-muted-foreground" />
              <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Electives ({data.elective_done}/{totalElective})
              </h2>
            </div>
            <ul className="space-y-2">
              {data.elective_items.map((item) => (
                <ItemRow key={item.resource_id} item={item} />
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  )
}

function StatCard({
  label,
  value,
  pct,
  color,
}: {
  label: string
  value: string
  pct: number
  color: 'blue' | 'purple'
}) {
  const trackColor = color === 'blue' ? 'bg-blue-500/20' : 'bg-purple-500/20'
  const barColor = color === 'blue' ? 'bg-blue-500' : 'bg-purple-500'

  return (
    <div className="rounded-xl border bg-card p-4">
      <p className="text-xs text-muted-foreground mb-1">{label}</p>
      <p className="text-lg font-semibold tabular-nums">{value}</p>
      <div className={`mt-2 h-1.5 rounded-full ${trackColor} overflow-hidden`}>
        <div
          className={`h-full rounded-full ${barColor} transition-all duration-500`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  )
}

function ItemRow({ item }: { item: ItemProgress }) {
  const done = item.progress?.status === 'completed'
  const inProgress = item.progress?.status === 'in_progress'
  const pct = item.progress?.progress_pct ?? 0

  return (
    <li className="flex items-center gap-3 rounded-xl border bg-card px-4 py-3 text-sm transition-colors hover:bg-accent/30">
      <span className="shrink-0">
        {done ? (
          <CheckCircle2 size={16} className="text-emerald-500" />
        ) : (
          <Circle size={16} className="text-muted-foreground/40" />
        )}
      </span>

      <div className="flex-1 min-w-0">
        <p className={`text-sm font-medium truncate ${done ? 'text-muted-foreground line-through decoration-muted-foreground/40' : ''}`}>
          {item.title}
        </p>
        {inProgress && item.progress && (
          <div className="mt-1.5 flex items-center gap-2">
            <div className="h-1 flex-1 max-w-24 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full rounded-full bg-primary/70 transition-all"
                style={{ width: `${pct}%` }}
              />
            </div>
            <span className="text-xs text-muted-foreground tabular-nums">
              {Math.round(pct)}%
              {item.progress.last_position_seconds > 0 &&
                ` · ${Math.floor(item.progress.last_position_seconds / 60)}m in`}
            </span>
          </div>
        )}
      </div>

      <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground capitalize">
        {item.content_type}
      </span>
    </li>
  )
}
