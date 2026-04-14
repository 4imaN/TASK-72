// DisputesPage — file and view appeals against reviews.
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, PortalApiError } from '../../app/api/client'
import {
  Flag, Plus, Loader2, AlertTriangle, CheckCircle,
  Clock, X, FileText, Trash2,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

// EvidenceItem is used when submitting a new appeal (input shape).
// Matches the backend EvidenceInput: filename, content_type, and base64-encoded
// file data. Without `data` the backend has nothing to persist to disk.
interface EvidenceItem {
  filename: string
  content_type: string
  data: string // base64-encoded file content (no data: prefix)
}

// Evidence matches the backend Evidence struct returned in Appeal.evidence
interface Evidence {
  id: string
  appeal_id: string
  original_name: string
  content_type: string
  size_bytes: number
  uploaded_at: string
}

// Appeal matches the backend Appeal struct from appeals_store.go
interface Appeal {
  id: string
  review_id: string
  review_excerpt?: string       // UI-only enrichment, may be absent
  appealed_by: string           // backend field: UUID of appellant
  appeal_reason: string         // backend field name (not "reason")
  status: 'pending' | 'under_review' | 'decided'
  outcome?: 'hide' | 'show_with_disclaimer' | 'restore'
  notes?: string
  submitted_at: string          // backend field name (not "created_at")
  decided_at?: string
  evidence: Evidence[]
}

interface AppealsResponse {
  appeals: Appeal[]
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badge
// ─────────────────────────────────────────────────────────────────────────────

function StatusBadge({ status, outcome }: { status: Appeal['status']; outcome?: Appeal['outcome'] }) {
  if (status === 'pending' || status === 'under_review') {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/15 px-2.5 py-1 text-xs font-medium text-amber-400">
        <Clock size={10} />
        {status === 'under_review' ? 'Under Review' : 'Pending'}
      </span>
    )
  }

  const outcomeConfig: Record<NonNullable<Appeal['outcome']>, { label: string; classes: string }> = {
    hide:               { label: 'Review Hidden',     classes: 'bg-red-500/15 text-red-400' },
    show_with_disclaimer: { label: 'With Disclaimer', classes: 'bg-blue-500/15 text-blue-400' },
    restore:            { label: 'Review Restored',   classes: 'bg-emerald-500/15 text-emerald-400' },
  }

  const cfg = outcome ? outcomeConfig[outcome] : { label: 'Decided', classes: 'bg-zinc-500/15 text-zinc-400' }

  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${cfg.classes}`}>
      <CheckCircle size={10} />
      {cfg.label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// File New Appeal Modal
// ─────────────────────────────────────────────────────────────────────────────

function NewAppealModal({
  onClose,
  onSubmit,
  isPending,
  error,
}: {
  onClose: () => void
  onSubmit: (data: { review_id: string; reason: string; evidence: EvidenceItem[] }) => void
  isPending: boolean
  error: string | null
}) {
  const [reviewId, setReviewId]   = useState('')
  const [reason, setReason]       = useState('')
  const [evidence, setEvidence]   = useState<EvidenceItem[]>([])
  const [fileError, setFileError] = useState<string | null>(null)

  // Supported by the backend magic-byte validator (see internal/platform/storage).
  const ALLOWED = ['application/pdf', 'image/jpeg', 'image/png']
  const MAX_BYTES = 5 * 1024 * 1024 // 5 MB per evidence file

  // Read a File as base64 (strips the `data:<mime>;base64,` prefix).
  const readAsBase64 = (file: File) =>
    new Promise<string>((resolve, reject) => {
      const r = new FileReader()
      r.onerror = () => reject(r.error ?? new Error('read failed'))
      r.onload = () => {
        const result = typeof r.result === 'string' ? r.result : ''
        const comma = result.indexOf(',')
        resolve(comma >= 0 ? result.slice(comma + 1) : result)
      }
      r.readAsDataURL(file)
    })

  const handleFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return
    setFileError(null)
    const additions: EvidenceItem[] = []
    for (const file of Array.from(files)) {
      if (!ALLOWED.includes(file.type)) {
        setFileError(`${file.name}: unsupported type (${file.type || 'unknown'}). Allowed: PDF, JPEG, PNG.`)
        continue
      }
      if (file.size > MAX_BYTES) {
        setFileError(`${file.name}: too large (max ${MAX_BYTES / 1024 / 1024} MB).`)
        continue
      }
      try {
        const data = await readAsBase64(file)
        additions.push({ filename: file.name, content_type: file.type, data })
      } catch {
        setFileError(`${file.name}: could not read file`)
      }
    }
    if (additions.length > 0) {
      setEvidence((prev) => [...prev, ...additions])
    }
  }

  const removeEvidence = (i: number) => setEvidence((prev) => prev.filter((_, idx) => idx !== i))

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!reviewId.trim() || !reason.trim()) return
    onSubmit({ review_id: reviewId.trim(), reason: reason.trim(), evidence })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-lg rounded-xl border bg-card shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div className="flex items-center gap-2">
            <Flag size={16} className="text-primary" />
            <h2 className="font-semibold text-sm">File New Appeal</h2>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <X size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          {error && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <div>
            <label className="block text-xs font-medium mb-1.5">Review ID</label>
            <input
              type="text"
              required
              value={reviewId}
              onChange={(e) => setReviewId(e.target.value)}
              placeholder="e.g. rev_abc123"
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
            <p className="mt-1 text-xs text-muted-foreground">
              The ID of the review you are appealing.
            </p>
          </div>

          <div>
            <label className="block text-xs font-medium mb-1.5">Reason for Appeal</label>
            <textarea
              required
              rows={4}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Explain why this review should be reconsidered..."
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring resize-none"
            />
          </div>

          {/* Evidence */}
          <div>
            <label className="block text-xs font-medium mb-1.5">Evidence Files (optional)</label>
            <div className="space-y-2">
              {evidence.map((ev, i) => (
                <div key={i} className="flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2">
                  <FileText size={13} className="text-muted-foreground shrink-0" />
                  <span className="flex-1 text-xs truncate">{ev.filename}</span>
                  <span className="text-xs text-muted-foreground">{ev.content_type}</span>
                  <button
                    type="button"
                    onClick={() => removeEvidence(i)}
                    className="text-muted-foreground hover:text-destructive"
                    aria-label={`Remove ${ev.filename}`}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              ))}

              {fileError && (
                <p className="text-xs text-destructive">{fileError}</p>
              )}

              <div>
                <input
                  type="file"
                  multiple
                  accept="application/pdf,image/jpeg,image/png"
                  onChange={(e) => {
                    handleFiles(e.target.files)
                    e.target.value = '' // allow re-selecting the same file
                  }}
                  className="block w-full text-xs text-muted-foreground file:mr-3 file:rounded-md file:border file:bg-background file:px-3 file:py-1.5 file:text-xs file:font-medium hover:file:bg-accent"
                />
                <p className="mt-1 text-xs text-muted-foreground">
                  PDF, JPEG, or PNG; up to {MAX_BYTES / 1024 / 1024} MB each.
                </p>
              </div>
            </div>
          </div>

          <div className="flex gap-3 justify-end pt-1">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border px-4 py-2 text-sm font-medium hover:bg-accent"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isPending || !reviewId.trim() || !reason.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed"
            >
              {isPending ? (
                <>
                  <Loader2 size={13} className="animate-spin" />
                  Submitting...
                </>
              ) : (
                'Submit Appeal'
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main DisputesPage
// ─────────────────────────────────────────────────────────────────────────────

export function DisputesPage() {
  const qc = useQueryClient()
  const [showModal, setShowModal] = useState(false)
  const [modalError, setModalError] = useState<string | null>(null)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['appeals', 'mine'],
    queryFn: () => api.get<AppealsResponse>('/appeals?my=true'),
  })

  const submitMut = useMutation({
    mutationFn: (payload: { review_id: string; reason: string; evidence: EvidenceItem[] }) =>
      api.post<unknown>('/appeals', payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appeals', 'mine'] })
      setShowModal(false)
      setModalError(null)
    },
    onError: (err) => {
      setModalError(
        err instanceof PortalApiError ? err.error.message : 'Failed to submit appeal.'
      )
    },
  })

  const appeals = data?.appeals ?? []

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {showModal && (
        <NewAppealModal
          onClose={() => { setShowModal(false); setModalError(null) }}
          onSubmit={(data) => submitMut.mutate(data)}
          isPending={submitMut.isPending}
          error={modalError}
        />
      )}

      {/* Page header */}
      <div className="border-b bg-card/50 px-6 py-5">
        <div className="max-w-4xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-primary/10 p-2">
              <Flag size={20} className="text-primary" />
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">Disputes</h1>
              <p className="text-xs text-muted-foreground mt-0.5">
                Appeals you have filed against reviews
              </p>
            </div>
          </div>
          <button
            onClick={() => setShowModal(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
          >
            <Plus size={15} />
            File New Appeal
          </button>
        </div>
      </div>

      <div className="max-w-4xl mx-auto px-6 py-6">
        {isLoading && (
          <div className="flex items-center justify-center h-48 text-muted-foreground">
            <Loader2 size={20} className="animate-spin mr-2" />
            <span className="text-sm">Loading appeals...</span>
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
              <Flag size={24} className="text-muted-foreground" />
            </div>
            <p className="font-medium text-sm">No appeals filed</p>
            <p className="text-xs text-muted-foreground mt-1 max-w-xs">
              If you believe a review is unfair or incorrect, you can file an appeal using the button above.
            </p>
          </div>
        )}

        {!isLoading && !isError && appeals.length > 0 && (
          <div className="space-y-3">
            {appeals.map((appeal) => (
              <div key={appeal.id} className="rounded-xl border bg-card p-5 space-y-3">
                <div className="flex items-start justify-between gap-4">
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
                      <span>Review: <span className="font-mono">{appeal.review_id}</span></span>
                      <span>&middot;</span>
                      <span>Filed {new Date(appeal.submitted_at).toLocaleDateString()}</span>
                      {appeal.decided_at && (
                        <>
                          <span>&middot;</span>
                          <span>Decided {new Date(appeal.decided_at).toLocaleDateString()}</span>
                        </>
                      )}
                    </div>
                  </div>
                  <StatusBadge status={appeal.status} outcome={appeal.outcome} />
                </div>

                {appeal.notes && (
                  <div className="rounded-lg bg-muted/50 px-3 py-2">
                    <p className="text-xs text-muted-foreground font-medium mb-0.5">Arbitration notes</p>
                    <p className="text-xs">{appeal.notes}</p>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
