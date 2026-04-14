// LearningPathsPage — list of paths, enrollment, progress display
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '../../app/api/client'
import { BookOpen, ChevronRight, Loader2, AlertTriangle, GraduationCap, Sparkles } from 'lucide-react'

interface PathRules {
  required_count: number
  elective_minimum: number
  completion_description?: string
}

interface LearningPath {
  id: string
  title: string
  description?: string
  is_published: boolean
  rules?: PathRules
}

interface PathsResponse {
  paths: LearningPath[]
}

export function LearningPathsPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['paths'],
    queryFn: () => api.get<PathsResponse>('/paths'),
  })

  const enroll = useMutation({
    mutationFn: (pathID: string) => api.post<unknown>(`/paths/${pathID}/enroll`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['paths'] }),
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 text-muted-foreground">
        <Loader2 size={20} className="animate-spin mr-2" />
        <span className="text-sm">Loading paths...</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-64 text-destructive">
        <AlertTriangle size={16} className="mr-2" />
        <span className="text-sm">Failed to load learning paths.</span>
      </div>
    )
  }

  const paths = data?.paths ?? []

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Page header */}
      <div className="border-b border-border px-6 py-5">
        <div className="max-w-4xl mx-auto flex items-center gap-3">
          <div className="rounded-lg bg-primary/10 p-2">
            <GraduationCap size={20} className="text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Learning Paths</h1>
            <p className="text-xs text-muted-foreground mt-0.5">
              Structured learning programs for your role and development goals
            </p>
          </div>
        </div>
      </div>

      <div className="max-w-4xl mx-auto px-6 py-6">
        {paths.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-20 text-center">
            <div className="rounded-full bg-muted p-4 mb-4">
              <BookOpen size={24} className="text-muted-foreground" />
            </div>
            <p className="font-medium text-sm">No learning paths available</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-xs">
              Learning paths will appear here once they are published by your administrator.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {paths.map((path) => (
              <PathCard
                key={path.id}
                path={path}
                isEnrolling={enroll.isPending && enroll.variables === path.id}
                onEnroll={() => enroll.mutate(path.id)}
                onView={() => navigate(`/paths/${path.id}`)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function PathCard({
  path,
  isEnrolling,
  onEnroll,
  onView,
}: {
  path: LearningPath
  isEnrolling: boolean
  onEnroll: () => void
  onView: () => void
}) {
  const hasRules = path.rules && (path.rules.required_count > 0 || path.rules.elective_minimum > 0)

  return (
    <div className="group rounded-xl border bg-card transition-shadow hover:shadow-sm">
      <div className="p-5">
        <div className="flex items-start gap-4">
          {/* Icon */}
          <div className="shrink-0 mt-0.5 rounded-lg bg-primary/8 p-2 group-hover:bg-primary/12 transition-colors">
            <BookOpen size={16} className="text-primary" />
          </div>

          {/* Content */}
          <div className="flex-1 min-w-0">
            <h2 className="font-semibold text-sm leading-tight">{path.title}</h2>
            {path.description && (
              <p className="mt-1.5 text-xs text-muted-foreground leading-relaxed line-clamp-2">
                {path.description}
              </p>
            )}

            {hasRules && path.rules && (
              <div className="mt-2.5 flex flex-wrap gap-2">
                {path.rules.required_count > 0 && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-blue-500/10 px-2 py-0.5 text-xs font-medium text-blue-600 dark:text-blue-400">
                    {path.rules.required_count} required
                  </span>
                )}
                {path.rules.elective_minimum > 0 && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-purple-500/10 px-2 py-0.5 text-xs font-medium text-purple-600 dark:text-purple-400">
                    {path.rules.elective_minimum}+ electives
                  </span>
                )}
                {path.rules.completion_description && (
                  <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                    <Sparkles size={10} />
                    {path.rules.completion_description}
                  </span>
                )}
              </div>
            )}
          </div>

          {/* Actions */}
          <div className="flex items-center gap-2 shrink-0">
            <button
              onClick={onView}
              className="inline-flex items-center gap-1 rounded-lg border bg-background px-3 py-1.5 text-xs font-medium transition-colors hover:bg-accent"
            >
              View
              <ChevronRight size={12} />
            </button>
            <button
              onClick={onEnroll}
              disabled={isEnrolling}
              className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed"
            >
              {isEnrolling ? (
                <>
                  <Loader2 size={11} className="animate-spin" />
                  Enrolling...
                </>
              ) : (
                'Enroll'
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
