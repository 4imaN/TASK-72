import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../app/api/client'
import { useIsReadOnly } from '../../app/guards'
import {
  ShieldAlert,
  ShieldCheck,
  ClipboardList,
  CheckCircle2,
  XCircle,
  ArrowUpCircle,
  Loader2,
  AlertTriangle,
  Clock,
  User,
  MessageSquare,
  ChevronRight,
  Filter,
} from 'lucide-react'

// ── Types ─────────────────────────────────────────────────────────────────────

interface ModerationItem {
  id: string
  review_id: string
  reason: string
  flagged_by: string
  status: string
  moderator_id?: string
  decision_notes?: string
  flagged_at: string
  decided_at?: string
}

interface QueueResponse {
  items: ModerationItem[]
  total: number
  limit: number
  offset: number
}

type DecisionType = 'approve' | 'reject' | 'escalate'

// ── Decision confirmation modal ───────────────────────────────────────────────

interface DecisionModalProps {
  item: ModerationItem
  decision: DecisionType
  onConfirm: (notes: string) => void
  onCancel: () => void
  isPending: boolean
}

function DecisionModal({ item, decision, onConfirm, onCancel, isPending }: DecisionModalProps) {
  const [notes, setNotes] = useState('')

  const config = {
    approve: {
      label: 'Approve',
      description: 'Mark this content as reviewed and acceptable. It will remain visible.',
      icon: <CheckCircle2 size={18} className="text-emerald-500" />,
      buttonClass: 'bg-emerald-600 hover:bg-emerald-700 text-white',
      borderClass: 'border-emerald-200',
    },
    reject: {
      label: 'Reject',
      description: 'Reject this content. The associated review may be hidden or flagged.',
      icon: <XCircle size={18} className="text-red-500" />,
      buttonClass: 'bg-red-600 hover:bg-red-700 text-white',
      borderClass: 'border-red-200',
    },
    escalate: {
      label: 'Escalate',
      description: 'Escalate this item for senior review. It will remain in pending state.',
      icon: <ArrowUpCircle size={18} className="text-amber-500" />,
      buttonClass: 'bg-amber-600 hover:bg-amber-700 text-white',
      borderClass: 'border-amber-200',
    },
  }[decision]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className={`w-full max-w-md rounded-xl border bg-card shadow-xl ${config.borderClass}`}>
        <div className="p-5 border-b flex items-center gap-3">
          {config.icon}
          <div>
            <p className="font-semibold text-sm">{config.label} Item</p>
            <p className="text-xs text-muted-foreground mt-0.5">{config.description}</p>
          </div>
        </div>

        <div className="p-5 space-y-4">
          {/* Review excerpt */}
          <div className="rounded-lg bg-muted/50 p-3 border">
            <p className="text-xs font-medium text-muted-foreground mb-1">Flagged reason</p>
            <p className="text-sm leading-relaxed">{item.reason}</p>
          </div>

          {/* Notes textarea */}
          <div>
            <label className="block text-xs font-medium mb-1.5">
              Decision notes
              <span className="text-muted-foreground font-normal ml-1">(optional)</span>
            </label>
            <textarea
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="Add context for this decision..."
              rows={3}
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm resize-none
                         focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring
                         placeholder:text-muted-foreground/50"
            />
          </div>
        </div>

        <div className="p-4 border-t flex items-center justify-end gap-2">
          <button
            onClick={onCancel}
            disabled={isPending}
            className="rounded-lg border px-4 py-1.5 text-sm font-medium
                       hover:bg-accent transition-colors disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={() => onConfirm(notes)}
            disabled={isPending}
            className={`inline-flex items-center gap-2 rounded-lg px-4 py-1.5 text-sm font-medium
                        transition-colors disabled:opacity-60 disabled:cursor-not-allowed
                        ${config.buttonClass}`}
          >
            {isPending ? (
              <>
                <Loader2 size={13} className="animate-spin" />
                Processing...
              </>
            ) : (
              config.label
            )}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Status badge ──────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { label: string; className: string }> = {
    pending: {
      label: 'Pending',
      className: 'bg-amber-500/10 text-amber-700 dark:text-amber-400',
    },
    approve: {
      label: 'Approved',
      className: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
    },
    reject: {
      label: 'Rejected',
      className: 'bg-red-500/10 text-red-700 dark:text-red-400',
    },
    escalate: {
      label: 'Escalated',
      className: 'bg-blue-500/10 text-blue-700 dark:text-blue-400',
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

// ── Queue row ─────────────────────────────────────────────────────────────────

interface QueueRowProps {
  item: ModerationItem
  onDecide: (item: ModerationItem, decision: DecisionType) => void
  isReadOnly: boolean
}

function QueueRow({ item, onDecide, isReadOnly }: QueueRowProps) {
  const flaggedDate = new Date(item.flagged_at)
  const timeAgo = formatTimeAgo(flaggedDate)

  return (
    <tr className="group border-b last:border-b-0 transition-colors hover:bg-muted/30">
      {/* Review ID */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <div className="h-6 w-6 rounded bg-primary/10 flex items-center justify-center shrink-0">
            <MessageSquare size={11} className="text-primary" />
          </div>
          <span className="text-xs font-mono text-muted-foreground truncate max-w-[100px]" title={item.review_id}>
            {item.review_id.slice(0, 8)}…
          </span>
        </div>
      </td>

      {/* Reason */}
      <td className="px-4 py-3 max-w-xs">
        <p className="text-sm line-clamp-2 leading-relaxed">{item.reason}</p>
      </td>

      {/* Flagged by */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-1.5">
          <User size={12} className="text-muted-foreground shrink-0" />
          <span className="text-xs text-muted-foreground font-mono truncate max-w-[80px]" title={item.flagged_by}>
            {item.flagged_by.slice(0, 8)}…
          </span>
        </div>
      </td>

      {/* Time */}
      <td className="px-4 py-3 whitespace-nowrap">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Clock size={11} />
          <span>{timeAgo}</span>
        </div>
      </td>

      {/* Status */}
      <td className="px-4 py-3">
        <StatusBadge status={item.status} />
      </td>

      {/* Actions */}
      <td className="px-4 py-3">
        {!isReadOnly && item.status === 'pending' ? (
          <div className="flex items-center gap-1.5">
            <button
              onClick={() => onDecide(item, 'approve')}
              title="Approve"
              className="inline-flex items-center gap-1 rounded-md bg-emerald-50 border border-emerald-200
                         px-2 py-1 text-xs font-medium text-emerald-700
                         hover:bg-emerald-100 transition-colors dark:bg-emerald-900/20
                         dark:border-emerald-800 dark:text-emerald-400"
            >
              <CheckCircle2 size={11} />
              Approve
            </button>
            <button
              onClick={() => onDecide(item, 'reject')}
              title="Reject"
              className="inline-flex items-center gap-1 rounded-md bg-red-50 border border-red-200
                         px-2 py-1 text-xs font-medium text-red-700
                         hover:bg-red-100 transition-colors dark:bg-red-900/20
                         dark:border-red-800 dark:text-red-400"
            >
              <XCircle size={11} />
              Reject
            </button>
            <button
              onClick={() => onDecide(item, 'escalate')}
              title="Escalate"
              className="inline-flex items-center gap-1 rounded-md bg-amber-50 border border-amber-200
                         px-2 py-1 text-xs font-medium text-amber-700
                         hover:bg-amber-100 transition-colors dark:bg-amber-900/20
                         dark:border-amber-800 dark:text-amber-400"
            >
              <ArrowUpCircle size={11} />
              Escalate
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            {item.decided_at && (
              <>
                <ChevronRight size={11} />
                <span>{formatTimeAgo(new Date(item.decided_at))}</span>
              </>
            )}
          </div>
        )}
      </td>
    </tr>
  )
}

// ── Decided row ───────────────────────────────────────────────────────────────

function DecidedRow({ item }: { item: ModerationItem }) {
  return (
    <tr className="border-b last:border-b-0">
      <td className="px-4 py-3">
        <span className="text-xs font-mono text-muted-foreground">{item.review_id.slice(0, 8)}…</span>
      </td>
      <td className="px-4 py-3 max-w-xs">
        <p className="text-sm line-clamp-2 text-muted-foreground">{item.reason}</p>
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={item.status} />
      </td>
      <td className="px-4 py-3">
        <p className="text-xs text-muted-foreground line-clamp-1">
          {item.decision_notes ?? '—'}
        </p>
      </td>
      <td className="px-4 py-3 whitespace-nowrap">
        <span className="text-xs text-muted-foreground">
          {item.decided_at ? formatTimeAgo(new Date(item.decided_at)) : '—'}
        </span>
      </td>
    </tr>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export function ModerationPage() {
  const [activeTab, setActiveTab] = useState<'queue' | 'decided'>('queue')
  const [pendingDecision, setPendingDecision] = useState<{
    item: ModerationItem
    decision: DecisionType
  } | null>(null)

  const isReadOnly = useIsReadOnly()
  const qc = useQueryClient()

  // Queue tab: pending items
  const queueQuery = useQuery({
    queryKey: ['moderation', 'queue', 'pending'],
    queryFn: () => api.get<QueueResponse>('/moderation/queue?status=pending&limit=50'),
    enabled: activeTab === 'queue',
    refetchInterval: 30_000,
  })

  // Decided tab: non-pending items
  const decidedQuery = useQuery({
    queryKey: ['moderation', 'queue', 'decided'],
    queryFn: () => api.get<QueueResponse>('/moderation/queue?limit=50'),
    enabled: activeTab === 'decided',
  })

  const decideMutation = useMutation({
    mutationFn: ({ itemID, decision, notes }: { itemID: string; decision: string; notes: string }) =>
      api.post<void>(`/moderation/queue/${itemID}/decide`, { decision, notes }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['moderation'] })
      setPendingDecision(null)
    },
  })

  const handleDecide = (item: ModerationItem, decision: DecisionType) => {
    setPendingDecision({ item, decision })
  }

  const handleConfirm = (notes: string) => {
    if (!pendingDecision) return
    decideMutation.mutate({
      itemID: pendingDecision.item.id,
      decision: pendingDecision.decision,
      notes,
    })
  }

  // Derive decided items from full list
  const allItems = decidedQuery.data?.items ?? []
  const decidedItems = allItems.filter((i) => i.status !== 'pending')
  const queueItems = queueQuery.data?.items ?? []
  const pendingCount = queueQuery.data?.total ?? 0

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {/* Page header */}
      <div className="border-b bg-card/50 px-6 py-5">
        <div className="max-w-5xl mx-auto flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-primary/10 p-2">
              <ShieldAlert size={20} className="text-primary" />
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">Review Moderation</h1>
              <p className="text-xs text-muted-foreground mt-0.5">
                Manage flagged review content and visibility decisions
              </p>
            </div>
          </div>

          {pendingCount > 0 && (
            <div className="flex items-center gap-2 rounded-full bg-amber-500/10 border border-amber-200/50 px-3 py-1.5">
              <AlertTriangle size={13} className="text-amber-600" />
              <span className="text-xs font-medium text-amber-700 dark:text-amber-400">
                {pendingCount} awaiting review
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b bg-card/30">
        <div className="max-w-5xl mx-auto px-6">
          <nav className="flex gap-1 pt-2" role="tablist">
            <TabButton
              active={activeTab === 'queue'}
              onClick={() => setActiveTab('queue')}
              icon={<ClipboardList size={14} />}
              label="Queue"
              badge={pendingCount > 0 ? String(pendingCount) : undefined}
            />
            <TabButton
              active={activeTab === 'decided'}
              onClick={() => setActiveTab('decided')}
              icon={<ShieldCheck size={14} />}
              label="Decided"
            />
          </nav>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-5xl mx-auto px-6 py-6">
        {activeTab === 'queue' && (
          <QueueTabContent
            items={queueItems}
            isLoading={queueQuery.isLoading}
            isError={queueQuery.isError}
            onDecide={handleDecide}
            isReadOnly={isReadOnly}
          />
        )}
        {activeTab === 'decided' && (
          <DecidedTabContent
            items={decidedItems}
            isLoading={decidedQuery.isLoading}
            isError={decidedQuery.isError}
          />
        )}
      </div>

      {/* Decision modal */}
      {pendingDecision && (
        <DecisionModal
          item={pendingDecision.item}
          decision={pendingDecision.decision}
          onConfirm={handleConfirm}
          onCancel={() => setPendingDecision(null)}
          isPending={decideMutation.isPending}
        />
      )}
    </div>
  )
}

// ── Tab button ────────────────────────────────────────────────────────────────

function TabButton({
  active,
  onClick,
  icon,
  label,
  badge,
}: {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
  badge?: string
}) {
  return (
    <button
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={`inline-flex items-center gap-2 px-3 py-2 text-sm font-medium rounded-t-lg
                  border-b-2 transition-colors -mb-px
                  ${active
                    ? 'border-primary text-primary'
                    : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'
                  }`}
    >
      {icon}
      {label}
      {badge && (
        <span className="ml-0.5 rounded-full bg-amber-500 text-white text-[10px] font-bold px-1.5 py-0.5 leading-none">
          {badge}
        </span>
      )}
    </button>
  )
}

// ── Queue tab content ─────────────────────────────────────────────────────────

function QueueTabContent({
  items,
  isLoading,
  isError,
  onDecide,
  isReadOnly,
}: {
  items: ModerationItem[]
  isLoading: boolean
  isError: boolean
  onDecide: (item: ModerationItem, decision: DecisionType) => void
  isReadOnly: boolean
}) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground">
        <Loader2 size={18} className="animate-spin mr-2" />
        <span className="text-sm">Loading queue...</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-48 gap-2 text-destructive">
        <AlertTriangle size={16} />
        <span className="text-sm">Failed to load moderation queue.</span>
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <div className="rounded-full bg-emerald-500/10 p-4 mb-4">
          <ShieldCheck size={24} className="text-emerald-600" />
        </div>
        <p className="font-medium text-sm">Queue is clear</p>
        <p className="text-xs text-muted-foreground mt-1 max-w-xs">
          No items are currently pending moderation review.
        </p>
      </div>
    )
  }

  return (
    <div className="rounded-xl border bg-card overflow-hidden shadow-sm">
      <div className="px-4 py-3 border-b bg-muted/20 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Filter size={13} className="text-muted-foreground" />
          <span className="text-xs font-medium text-muted-foreground">
            {items.length} item{items.length !== 1 ? 's' : ''} pending review
          </span>
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/10">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Review</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Reason flagged</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Flagged by</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Time</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Status</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <QueueRow key={item.id} item={item} onDecide={onDecide} isReadOnly={isReadOnly} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ── Decided tab content ───────────────────────────────────────────────────────

function DecidedTabContent({
  items,
  isLoading,
  isError,
}: {
  items: ModerationItem[]
  isLoading: boolean
  isError: boolean
}) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground">
        <Loader2 size={18} className="animate-spin mr-2" />
        <span className="text-sm">Loading history...</span>
      </div>
    )
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-48 gap-2 text-destructive">
        <AlertTriangle size={16} />
        <span className="text-sm">Failed to load decided items.</span>
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <div className="rounded-full bg-muted p-4 mb-4">
          <ClipboardList size={24} className="text-muted-foreground" />
        </div>
        <p className="font-medium text-sm">No decided items yet</p>
        <p className="text-xs text-muted-foreground mt-1">
          Decisions made on queue items will appear here.
        </p>
      </div>
    )
  }

  return (
    <div className="rounded-xl border bg-card overflow-hidden shadow-sm">
      <div className="px-4 py-3 border-b bg-muted/20">
        <span className="text-xs font-medium text-muted-foreground">
          {items.length} decided item{items.length !== 1 ? 's' : ''}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/10">
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Review</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Reason</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Decision</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Notes</th>
              <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground">Decided</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <DecidedRow key={item.id} item={item} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ── Utilities ─────────────────────────────────────────────────────────────────

function formatTimeAgo(date: Date): string {
  const now = new Date()
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000)

  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}
