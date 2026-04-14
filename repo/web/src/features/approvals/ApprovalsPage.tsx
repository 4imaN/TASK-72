// ApprovalsPage — arbitrate pending appeals (appeals:decide permission required).
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, PortalApiError } from '../../app/api/client'
import {
  Shield, Loader2, AlertTriangle, EyeOff, AlertOctagon,
  RotateCcw, X, ChevronRight,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

// Appeal matches the backend Appeal struct from appeals_store.go
interface Appeal {
  id: string
  review_id: string
  review_excerpt?: string      // UI-only enrichment, may be absent
  appealed_by: string          // backend field: UUID of appellant
  appeal_reason: string        // backend field name (not "reason")
  status: 'pending' | 'under_review' | 'decided'
  submitted_at: string         // backend field name (not "created_at")
  decided_at?: string
}

interface AppealsResponse {
  appeals: Appeal[]
}

type ArbitrateOutcome = 'hide' | 'show_with_disclaimer' | 'restore'

interface ArbitratePayload {
  outcome: ArbitrateOutcome
  notes: string
  disclaimer_text?: string
}

// ─────────────────────────────────────────────────────────────────────────────
// Arbitrate Modal
// ─────────────────────────────────────────────────────────────────────────────

const OUTCOME_CONFIG: Record<ArbitrateOutcome, { label: string; icon: React.ReactNode; classes: string; activeClasses: string }> = {
  hide: {
    label: 'Hide Review',
    icon: <EyeOff size={14} />,
    classes: 'border-red-200 text-red-700 hover:bg-red-50',
    activeClasses: 'border-red-400 bg-red-50 text-red-700 ring-1 ring-red-400',
  },
  show_with_disclaimer: {
    label: 'Show with Disclaimer',
    icon: <AlertOctagon size={14} />,
    classes: 'border-amber-200 text-amber-700 hover:bg-amber-50',
    activeClasses: 'border-amber-400 bg-amber-50 text-amber-700 ring-1 ring-amber-400',
  },
  restore: {
    label: 'Restore Review',
    icon: <RotateCcw size={14} />,
    classes: 'border-emerald-200 text-emerald-700 hover:bg-emerald-50',
    activeClasses: 'border-emerald-400 bg-emerald-50 text-emerald-700 ring-1 ring-emerald-400',
  },
}

function ArbitrateModal({
  appeal,
  onClose,
  onSubmit,
  isPending,
  error,
}: {
  appeal: Appeal
  onClose: () => void
  onSubmit: (id: string, payload: ArbitratePayload) => void
  isPending: boolean
  error: string | null
}) {
  const [outcome, setOutcome]               = useState<ArbitrateOutcome | null>(null)
  const [notes, setNotes]                   = useState('')
  const [disclaimerText, setDisclaimerText] = useState('')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!outcome) return
    onSubmit(appeal.id, {
      outcome,
      notes: notes.trim(),
      disclaimer_text: outcome === 'show_with_disclaimer' ? disclaimerText.trim() : undefined,
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-lg rounded-xl border bg-card shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div className="flex items-center gap-2">
            <Shield size={16} className="text-primary" />
            <h2 className="font-semibold text-sm">Arbitrate Appeal</h2>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <X size={16} />
          </button>
        </div>

        <div className="p-5 space-y-4">
          {/* Appeal summary */}
          <div className="rounded-lg bg-muted/50 p-4 space-y-2">
            {appeal.review_excerpt && (
              <blockquote className="border-l-2 border-muted pl-3">
                <p className="text-xs text-muted-foreground italic line-clamp-2">
                  "{appeal.review_excerpt}"
                </p>
              </blockquote>
            )}
            <p className="text-sm font-medium">{appeal.appeal_reason}</p>
            <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
              {appeal.appealed_by && (
                <span>Filed by: <span className="font-mono text-xs">{appeal.appealed_by.slice(0, 8)}…</span></span>
              )}
              <span>Filed {new Date(appeal.submitted_at).toLocaleDateString()}</span>
            </div>
          </div>

          {error && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            {/* Outcome selection */}
            <div>
              <label className="block text-xs font-medium mb-2">Decision</label>
              <div className="flex gap-2">
                {(Object.entries(OUTCOME_CONFIG) as [ArbitrateOutcome, typeof OUTCOME_CONFIG[ArbitrateOutcome]][]).map(([key, cfg]) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setOutcome(key)}
                    className={`flex-1 inline-flex items-center justify-center gap-1.5 rounded-lg border px-2 py-2 text-xs font-medium transition-colors ${
                      outcome === key ? cfg.activeClasses : cfg.classes
                    }`}
                  >
                    {cfg.icon}
                    <span className="hidden sm:inline">{cfg.label}</span>
                  </button>
                ))}
              </div>
              {outcome && (
                <p className="mt-1.5 text-xs text-muted-foreground">
                  Selected: <span className="font-medium">{OUTCOME_CONFIG[outcome].label}</span>
                </p>
              )}
            </div>

            {/* Disclaimer text — only if show_with_disclaimer */}
            {outcome === 'show_with_disclaimer' && (
              <div>
                <label className="block text-xs font-medium mb-1.5">Disclaimer Text</label>
                <input
                  type="text"
                  value={disclaimerText}
                  onChange={(e) => setDisclaimerText(e.target.value)}
                  placeholder="This review has been flagged for review..."
                  className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
            )}

            {/* Notes */}
            <div>
              <label className="block text-xs font-medium mb-1.5">Arbitration Notes</label>
              <textarea
                rows={3}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Add internal notes about this decision..."
                className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring resize-none"
              />
            </div>

            <div className="flex gap-3 justify-end">
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg border px-4 py-2 text-sm font-medium hover:bg-accent"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isPending || !outcome}
                className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed"
              >
                {isPending ? (
                  <>
                    <Loader2 size={13} className="animate-spin" />
                    Saving...
                  </>
                ) : (
                  <>
                    Submit Decision
                    <ChevronRight size={13} />
                  </>
                )}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main ApprovalsPage
// ─────────────────────────────────────────────────────────────────────────────

export function ApprovalsPage() {
  const qc = useQueryClient()
  const [selectedAppeal, setSelectedAppeal] = useState<Appeal | null>(null)
  const [modalError, setModalError]         = useState<string | null>(null)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['appeals', 'pending'],
    queryFn: () => api.get<AppealsResponse>('/appeals?status=pending'),
  })

  const arbitrateMut = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: ArbitratePayload }) =>
      api.post<unknown>(`/appeals/${id}/arbitrate`, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appeals', 'pending'] })
      setSelectedAppeal(null)
      setModalError(null)
    },
    onError: (err) => {
      setModalError(
        err instanceof PortalApiError ? err.error.message : 'Failed to submit decision.'
      )
    },
  })

  const appeals = data?.appeals ?? []

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {selectedAppeal && (
        <ArbitrateModal
          appeal={selectedAppeal}
          onClose={() => { setSelectedAppeal(null); setModalError(null) }}
          onSubmit={(id, payload) => arbitrateMut.mutate({ id, payload })}
          isPending={arbitrateMut.isPending}
          error={modalError}
        />
      )}

      {/* Page header */}
      <div className="border-b bg-card/50 px-6 py-5">
        <div className="max-w-4xl mx-auto flex items-center gap-3">
          <div className="rounded-lg bg-primary/10 p-2">
            <Shield size={20} className="text-primary" />
          </div>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Approvals</h1>
            <p className="text-xs text-muted-foreground mt-0.5">
              Review and decide on pending appeal requests
            </p>
          </div>
        </div>
      </div>

      <div className="max-w-4xl mx-auto px-6 py-6">
        {isLoading && (
          <div className="flex items-center justify-center h-48 text-muted-foreground">
            <Loader2 size={20} className="animate-spin mr-2" />
            <span className="text-sm">Loading pending appeals...</span>
          </div>
        )}

        {isError && (
          <div className="flex items-center justify-center h-48 text-destructive">
            <AlertTriangle size={16} className="mr-2" />
            <span className="text-sm">
              {error instanceof PortalApiError ? error.error.message : 'Failed to load appeals.'}
            </span>
          </div>
        )}

        {!isLoading && !isError && appeals.length === 0 && (
          <div className="flex flex-col items-center justify-center py-20 text-center">
            <div className="rounded-full bg-muted p-4 mb-4">
              <Shield size={24} className="text-muted-foreground" />
            </div>
            <p className="font-medium text-sm">No pending appeals</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-xs">
              All appeal requests have been decided. New appeals will appear here.
            </p>
          </div>
        )}

        {!isLoading && !isError && appeals.length > 0 && (
          <div className="space-y-3">
            <p className="text-xs text-muted-foreground mb-3">
              {appeals.length} pending appeal{appeals.length !== 1 ? 's' : ''} awaiting decision
            </p>
            {appeals.map((appeal) => (
              <div
                key={appeal.id}
                className="rounded-xl border bg-card p-5"
              >
                <div className="flex items-start gap-4">
                  {/* Icon */}
                  <div className="shrink-0 mt-0.5 rounded-lg bg-amber-500/10 p-2">
                    <AlertOctagon size={15} className="text-amber-600" />
                  </div>

                  {/* Content */}
                  <div className="flex-1 min-w-0">
                    {appeal.review_excerpt && (
                      <blockquote className="border-l-2 border-muted pl-3 mb-2">
                        <p className="text-xs text-muted-foreground italic line-clamp-2">
                          "{appeal.review_excerpt}"
                        </p>
                      </blockquote>
                    )}
                    <p className="text-sm font-medium leading-snug">{appeal.appeal_reason}</p>
                    <div className="mt-1.5 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      {appeal.appealed_by && (
                        <span>By: <span className="font-mono text-xs">{appeal.appealed_by.slice(0, 8)}…</span></span>
                      )}
                      <span>&middot;</span>
                      <span>Filed {new Date(appeal.submitted_at).toLocaleDateString()}</span>
                      <span>&middot;</span>
                      <span className="font-mono">Review: {appeal.review_id}</span>
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="shrink-0 flex flex-col gap-2">
                    <button
                      onClick={() => { setSelectedAppeal(appeal); setModalError(null) }}
                      className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                    >
                      <Shield size={12} />
                      Decide
                    </button>
                  </div>
                </div>

                {/* Quick action row */}
                <div className="mt-4 pt-3 border-t border-border flex items-center gap-2">
                  <span className="text-xs text-muted-foreground mr-1">Quick action:</span>
                  {(Object.entries(OUTCOME_CONFIG) as [ArbitrateOutcome, typeof OUTCOME_CONFIG[ArbitrateOutcome]][]).map(([key, cfg]) => (
                    <button
                      key={key}
                      onClick={() => {
                        setSelectedAppeal(appeal)
                        setModalError(null)
                      }}
                      title={cfg.label}
                      className={`inline-flex items-center gap-1 rounded-md border px-2.5 py-1 text-xs font-medium transition-colors ${cfg.classes}`}
                    >
                      {cfg.icon}
                      <span className="hidden sm:inline">{cfg.label}</span>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
