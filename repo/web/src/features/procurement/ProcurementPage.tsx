// ProcurementPage — vendor order governance: all orders, my requests, create/approve/reject.
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, PortalApiError } from '../../app/api/client'
import { useAuthStore } from '../../app/store'
import {
  ShoppingCart, Plus, Loader2, AlertTriangle, Package,
  CheckCircle, Clock, XCircle, X, ChevronRight,
  Star, MessageSquare,
} from 'lucide-react'

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

interface VendorOrder {
  id: string
  vendor_name: string
  order_number: string
  order_date: string
  status: string // pending, received, disputed, closed
  total_amount: number // minor units (cents)
  currency: string
  created_by?: string
  created_at: string
  updated_at: string
}

interface OrdersResponse {
  orders: VendorOrder[]
  total: number
  limit: number
  offset: number
}

interface Review {
  id: string
  reviewer_id: string
  rating: number
  review_text?: string
  visibility: string
  created_at: string
}

interface ReviewsResponse {
  reviews: Review[]
}

// AttachmentItem matches the backend AttachmentInput shape for review uploads:
// filename + content_type + base64-encoded data (no data: prefix).
interface AttachmentItem {
  filename:     string
  content_type: string
  data:         string
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function formatAmount(minorUnits: number, currency: string) {
  const major = minorUnits / 100
  try {
    return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(major)
  } catch {
    return `${currency} ${major.toFixed(2)}`
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badge
// ─────────────────────────────────────────────────────────────────────────────

const STATUS_STYLES: Record<string, { label: string; classes: string; icon: React.ReactNode }> = {
  pending:   { label: 'Pending',   classes: 'bg-amber-500/15 text-amber-400',   icon: <Clock size={10} /> },
  received:  { label: 'Approved',  classes: 'bg-emerald-500/15 text-emerald-400', icon: <CheckCircle size={10} /> },
  disputed:  { label: 'Disputed',  classes: 'bg-blue-500/15 text-blue-400',      icon: <AlertTriangle size={10} /> },
  closed:    { label: 'Closed',    classes: 'bg-zinc-500/15 text-zinc-400', icon: <XCircle size={10} /> },
}

function StatusBadge({ status }: { status: string }) {
  const cfg = STATUS_STYLES[status] ?? { label: status, classes: 'bg-muted text-muted-foreground', icon: <Clock size={10} /> }
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${cfg.classes}`}>
      {cfg.icon}
      {cfg.label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Create Order Modal
// ─────────────────────────────────────────────────────────────────────────────

function CreateOrderModal({
  onClose,
  onSubmit,
  isPending,
  error,
}: {
  onClose: () => void
  onSubmit: (data: { vendor_name: string; description: string; total_amount: number }) => void
  isPending: boolean
  error: string | null
}) {
  const [vendorName, setVendorName] = useState('')
  const [description, setDescription] = useState('')
  const [totalAmount, setTotalAmount] = useState('')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!vendorName.trim()) return
    onSubmit({
      vendor_name: vendorName.trim(),
      description: description.trim(),
      total_amount: parseFloat(totalAmount) || 0,
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-lg rounded-xl border bg-card shadow-xl">
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div className="flex items-center gap-2">
            <Plus size={16} className="text-primary" />
            <h2 className="font-semibold text-sm">Create Vendor Order</h2>
          </div>
          <button onClick={onClose} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
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
            <label className="block text-xs font-medium mb-1.5">Vendor Name <span className="text-destructive">*</span></label>
            <input
              type="text"
              required
              value={vendorName}
              onChange={(e) => setVendorName(e.target.value)}
              placeholder="e.g. Acme Supplies Inc."
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div>
            <label className="block text-xs font-medium mb-1.5">Description</label>
            <textarea
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Describe the purpose of this order..."
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring resize-none"
            />
          </div>

          <div>
            <label className="block text-xs font-medium mb-1.5">Total Amount (USD)</label>
            <input
              type="number"
              min="0"
              step="0.01"
              value={totalAmount}
              onChange={(e) => setTotalAmount(e.target.value)}
              placeholder="0.00"
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div className="flex gap-3 justify-end pt-1">
            <button type="button" onClick={onClose} className="rounded-lg border px-4 py-2 text-sm font-medium hover:bg-accent">
              Cancel
            </button>
            <button
              type="submit"
              disabled={isPending || !vendorName.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed"
            >
              {isPending ? (
                <><Loader2 size={13} className="animate-spin" />Creating...</>
              ) : (
                <><Plus size={13} />Create Order</>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Reject Modal
// ─────────────────────────────────────────────────────────────────────────────

function RejectModal({
  orderId,
  onClose,
  onSubmit,
  isPending,
}: {
  orderId: string
  onClose: () => void
  onSubmit: (id: string, reason: string) => void
  isPending: boolean
}) {
  const [reason, setReason] = useState('')
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-md rounded-xl border bg-card shadow-xl">
        <div className="flex items-center justify-between border-b px-5 py-4">
          <h2 className="font-semibold text-sm">Reject Order</h2>
          <button onClick={onClose} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
            <X size={16} />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-xs font-medium mb-1.5">Reason for rejection <span className="text-destructive">*</span></label>
            <textarea
              required
              rows={3}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Explain why this order is being rejected..."
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring resize-none"
            />
          </div>
          <div className="flex gap-3 justify-end">
            <button type="button" onClick={onClose} className="rounded-lg border px-4 py-2 text-sm font-medium hover:bg-accent">
              Cancel
            </button>
            <button
              onClick={() => { if (reason.trim()) onSubmit(orderId, reason.trim()) }}
              disabled={isPending || !reason.trim()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-destructive px-4 py-2 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-60 disabled:cursor-not-allowed"
            >
              {isPending ? <><Loader2 size={13} className="animate-spin" />Rejecting...</> : 'Reject Order'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Order Detail Panel
// ─────────────────────────────────────────────────────────────────────────────

function OrderDetailPanel({ order, onClose }: { order: VendorOrder; onClose: () => void }) {
  const qc = useQueryClient()
  const canReview        = useAuthStore((s) => s.hasPermission('reviews:write'))
  const canMerchantReply = useAuthStore((s) => s.hasPermission('merchant_replies:write'))

  const { data: reviewsData, isLoading } = useQuery({
    queryKey: ['order-reviews', order.id],
    queryFn: () => api.get<ReviewsResponse>(`/orders/${order.id}/reviews`),
    retry: false,
  })

  const reviews = reviewsData?.reviews ?? []
  const [showReviewForm, setShowReviewForm] = useState(false)
  const [replyTarget, setReplyTarget]       = useState<string | null>(null)
  const [createError, setCreateError]       = useState<string | null>(null)

  const invalidateReviews = () => qc.invalidateQueries({ queryKey: ['order-reviews', order.id] })

  const createReviewMut = useMutation({
    mutationFn: (body: { order_id: string; rating: number; body: string; attachments: AttachmentItem[] }) =>
      api.post('/reviews', body),
    onSuccess: () => {
      invalidateReviews()
      setShowReviewForm(false)
      setCreateError(null)
    },
    onError: (err) => {
      setCreateError(err instanceof PortalApiError ? err.error.message : 'Failed to submit review.')
    },
  })

  const replyMut = useMutation({
    mutationFn: (vars: { reviewID: string; reply_text: string }) =>
      api.post(`/reviews/${vars.reviewID}/reply`, { reply_text: vars.reply_text }),
    onSuccess: () => {
      invalidateReviews()
      setReplyTarget(null)
    },
  })

  return (
    <div className="fixed inset-0 z-40 flex items-start justify-end bg-black/30 p-4">
      <div className="w-full max-w-md rounded-xl border bg-card shadow-xl h-full max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between border-b px-5 py-4 sticky top-0 bg-card z-10">
          <div className="flex items-center gap-2">
            <Package size={16} className="text-primary" />
            <h2 className="font-semibold text-sm">Order Detail</h2>
          </div>
          <button onClick={onClose} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
            <X size={16} />
          </button>
        </div>

        <div className="p-5 space-y-4">
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Order #</span>
              <span className="font-mono text-xs">{order.order_number}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Vendor</span>
              <span className="text-sm font-medium text-right max-w-[60%] truncate">{order.vendor_name}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Status</span>
              <StatusBadge status={order.status} />
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Amount</span>
              <span className="text-sm font-semibold tabular-nums">{formatAmount(order.total_amount, order.currency)}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Order Date</span>
              <span className="text-xs">{order.order_date}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">Created</span>
              <span className="text-xs">{new Date(order.created_at).toLocaleDateString()}</span>
            </div>
          </div>

          <div className="border-t pt-4">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Linked Reviews</h3>
              {canReview && !showReviewForm && (
                <button
                  onClick={() => { setShowReviewForm(true); setCreateError(null) }}
                  className="inline-flex items-center gap-1 rounded border px-2 py-0.5 text-[11px] font-medium hover:bg-accent"
                >
                  <Star size={11} /> Add Review
                </button>
              )}
            </div>

            {showReviewForm && (
              <ReviewForm
                orderID={order.id}
                error={createError}
                submitting={createReviewMut.isPending}
                onCancel={() => { setShowReviewForm(false); setCreateError(null) }}
                onSubmit={(body) => createReviewMut.mutate(body)}
              />
            )}

            {isLoading && (
              <div className="flex items-center gap-2 text-muted-foreground text-sm">
                <Loader2 size={14} className="animate-spin" />
                Loading reviews...
              </div>
            )}
            {!isLoading && reviews.length === 0 && !showReviewForm && (
              <p className="text-xs text-muted-foreground">No reviews linked to this order.</p>
            )}
            {reviews.map((rev) => (
              <div key={rev.id} className="rounded-lg border bg-muted/30 p-3 mb-2">
                <div className="flex items-center gap-2 mb-1">
                  <span className="text-xs font-medium">Rating: {rev.rating}/5</span>
                  <span className={`text-xs px-1.5 py-0.5 rounded-full ${
                    rev.visibility === 'visible' ? 'bg-emerald-500/10 text-emerald-700' :
                    rev.visibility === 'hidden' ? 'bg-red-500/10 text-red-700' :
                    'bg-blue-500/10 text-blue-700'
                  }`}>{rev.visibility}</span>
                  {canMerchantReply && (
                    <button
                      onClick={() => setReplyTarget(replyTarget === rev.id ? null : rev.id)}
                      className="ml-auto inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] hover:bg-accent"
                    >
                      <MessageSquare size={10} /> Reply
                    </button>
                  )}
                </div>
                {rev.review_text && (
                  <p className="text-xs text-muted-foreground line-clamp-2">{rev.review_text}</p>
                )}
                <p className="text-xs text-muted-foreground mt-1">{new Date(rev.created_at).toLocaleDateString()}</p>

                {replyTarget === rev.id && (
                  <ReplyComposer
                    submitting={replyMut.isPending}
                    onCancel={() => setReplyTarget(null)}
                    onSubmit={(text) => replyMut.mutate({ reviewID: rev.id, reply_text: text })}
                  />
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// ReviewForm — submit a review with optional image attachments
// ─────────────────────────────────────────────────────────────────────────────

function ReviewForm({
  orderID,
  error,
  submitting,
  onCancel,
  onSubmit,
}: {
  orderID:    string
  error:      string | null
  submitting: boolean
  onCancel:   () => void
  onSubmit:   (body: { order_id: string; rating: number; body: string; attachments: AttachmentItem[] }) => void
}) {
  const [rating, setRating]           = useState(5)
  const [body, setBody]               = useState('')
  const [attachments, setAttachments] = useState<AttachmentItem[]>([])
  const [fileError, setFileError]     = useState<string | null>(null)

  const ALLOWED = ['image/jpeg', 'image/png']
  const MAX_BYTES = 5 * 1024 * 1024
  const MAX_FILES = 5

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
    const additions: AttachmentItem[] = []
    for (const file of Array.from(files)) {
      if (attachments.length + additions.length >= MAX_FILES) {
        setFileError(`Maximum ${MAX_FILES} attachments`)
        break
      }
      if (!ALLOWED.includes(file.type)) {
        setFileError(`${file.name}: only JPEG and PNG accepted`)
        continue
      }
      if (file.size > MAX_BYTES) {
        setFileError(`${file.name}: exceeds ${MAX_BYTES / 1024 / 1024} MB`)
        continue
      }
      try {
        additions.push({
          filename:     file.name,
          content_type: file.type,
          data:         await readAsBase64(file),
        })
      } catch {
        setFileError(`${file.name}: could not read file`)
      }
    }
    if (additions.length > 0) setAttachments((prev) => [...prev, ...additions])
  }

  const removeAttachment = (i: number) =>
    setAttachments((prev) => prev.filter((_, idx) => idx !== i))

  const submit = () => {
    if (rating < 1 || rating > 5) return
    onSubmit({ order_id: orderID, rating, body: body.trim(), attachments })
  }

  return (
    <div className="mb-3 rounded-lg border bg-muted/30 p-3 space-y-2">
      <p className="text-xs font-semibold text-muted-foreground">New review</p>

      {error && <p className="text-xs text-destructive">{error}</p>}

      <div className="flex items-center gap-1.5">
        <span className="text-[11px] text-muted-foreground">Rating</span>
        {[1, 2, 3, 4, 5].map((n) => (
          <button
            key={n}
            type="button"
            onClick={() => setRating(n)}
            aria-label={`Set rating to ${n}`}
            className={`text-[14px] ${n <= rating ? 'text-amber-500' : 'text-muted-foreground'}`}
          >
            ★
          </button>
        ))}
        <span className="text-[10px] text-muted-foreground tabular-nums">{rating}/5</span>
      </div>

      <textarea
        rows={3}
        value={body}
        maxLength={2000}
        onChange={(e) => setBody(e.target.value)}
        placeholder="Optional review body…"
        className="w-full rounded border bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-1 focus:ring-ring resize-none"
      />

      <div>
        <input
          type="file"
          multiple
          accept="image/jpeg,image/png"
          onChange={(e) => { handleFiles(e.target.files); e.target.value = '' }}
          className="block w-full text-[11px] text-muted-foreground file:mr-2 file:rounded file:border file:bg-background file:px-2 file:py-1 file:text-[11px] file:font-medium hover:file:bg-accent"
        />
        <p className="mt-1 text-[10px] text-muted-foreground">JPEG or PNG, up to {MAX_BYTES / 1024 / 1024} MB each, max {MAX_FILES} files.</p>
        {fileError && <p className="text-[11px] text-destructive">{fileError}</p>}
        {attachments.length > 0 && (
          <ul className="mt-1 space-y-1">
            {attachments.map((a, i) => (
              <li key={i} className="flex items-center gap-2 text-[11px]">
                <span className="flex-1 truncate">{a.filename}</span>
                <button
                  type="button"
                  onClick={() => removeAttachment(i)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label={`Remove ${a.filename}`}
                >
                  <X size={11} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="flex justify-end gap-2 pt-1">
        <button
          onClick={onCancel}
          type="button"
          className="rounded border px-2 py-1 text-[11px] hover:bg-accent"
        >
          Cancel
        </button>
        <button
          onClick={submit}
          disabled={submitting}
          className="inline-flex items-center gap-1 rounded bg-primary px-2 py-1 text-[11px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60"
        >
          {submitting ? <Loader2 size={11} className="animate-spin" /> : <Star size={11} />}
          Submit Review
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// ReplyComposer — merchant reply input shown inline under each review
// ─────────────────────────────────────────────────────────────────────────────

function ReplyComposer({
  submitting,
  onCancel,
  onSubmit,
}: {
  submitting: boolean
  onCancel:   () => void
  onSubmit:   (text: string) => void
}) {
  const [text, setText] = useState('')
  return (
    <div className="mt-2 space-y-1.5">
      <textarea
        rows={2}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Merchant reply…"
        className="w-full rounded border bg-background px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-ring resize-none"
      />
      <div className="flex justify-end gap-2">
        <button
          onClick={onCancel}
          type="button"
          className="rounded border px-2 py-0.5 text-[10px] hover:bg-accent"
        >
          Cancel
        </button>
        <button
          onClick={() => text.trim() && onSubmit(text.trim())}
          disabled={submitting || !text.trim()}
          className="inline-flex items-center gap-1 rounded bg-primary px-2 py-0.5 text-[10px] font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60"
        >
          {submitting ? <Loader2 size={10} className="animate-spin" /> : <MessageSquare size={10} />}
          Send Reply
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Orders Table (shared by both tabs)
// ─────────────────────────────────────────────────────────────────────────────

function OrdersTable({
  orders,
  canManage,
  onApprove,
  onReject,
  onViewDetail,
  isApproving,
  isRejecting,
  approvingId,
}: {
  orders: VendorOrder[]
  canManage: boolean
  onApprove: (id: string) => void
  onReject: (id: string) => void
  onViewDetail: (order: VendorOrder) => void
  isApproving: boolean
  isRejecting: boolean
  approvingId: string | null
}) {
  if (orders.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <div className="rounded-full bg-muted p-4 mb-4">
          <ShoppingCart size={24} className="text-muted-foreground" />
        </div>
        <p className="font-medium text-sm">No orders found</p>
        <p className="text-xs text-muted-foreground mt-1 max-w-xs">
          Orders will appear here once created.
        </p>
      </div>
    )
  }

  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/40">
            <th className="px-4 py-3 text-left text-xs font-semibold text-muted-foreground">Vendor</th>
            <th className="px-4 py-3 text-left text-xs font-semibold text-muted-foreground hidden sm:table-cell">Order #</th>
            <th className="px-4 py-3 text-right text-xs font-semibold text-muted-foreground">Amount</th>
            <th className="px-4 py-3 text-center text-xs font-semibold text-muted-foreground">Status</th>
            <th className="px-4 py-3 text-left text-xs font-semibold text-muted-foreground hidden md:table-cell">Created</th>
            <th className="px-4 py-3 text-right text-xs font-semibold text-muted-foreground">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {orders.map((order) => (
            <tr
              key={order.id}
              className="hover:bg-muted/20 transition-colors cursor-pointer"
              onClick={() => onViewDetail(order)}
            >
              <td className="px-4 py-3">
                <p className="font-medium leading-tight max-w-[200px] truncate">{order.vendor_name}</p>
              </td>
              <td className="px-4 py-3 hidden sm:table-cell">
                <span className="font-mono text-xs text-muted-foreground">{order.order_number}</span>
              </td>
              <td className="px-4 py-3 text-right tabular-nums font-semibold">
                {formatAmount(order.total_amount, order.currency)}
              </td>
              <td className="px-4 py-3 text-center">
                <StatusBadge status={order.status} />
              </td>
              <td className="px-4 py-3 text-xs text-muted-foreground hidden md:table-cell">
                {new Date(order.created_at).toLocaleDateString()}
              </td>
              <td className="px-4 py-3">
                <div
                  className="flex items-center justify-end gap-1.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  {canManage && order.status === 'pending' && (
                    <>
                      <button
                        onClick={() => onApprove(order.id)}
                        disabled={isApproving && approvingId === order.id}
                        className="inline-flex items-center gap-1 rounded-md bg-emerald-500/10 px-2 py-1 text-xs font-medium text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/20 transition-colors disabled:opacity-60"
                      >
                        {isApproving && approvingId === order.id ? (
                          <Loader2 size={10} className="animate-spin" />
                        ) : (
                          <CheckCircle size={10} />
                        )}
                        Approve
                      </button>
                      <button
                        onClick={() => onReject(order.id)}
                        disabled={isRejecting && approvingId === order.id}
                        className="inline-flex items-center gap-1 rounded-md bg-red-500/10 px-2 py-1 text-xs font-medium text-red-700 dark:text-red-400 hover:bg-red-500/20 transition-colors disabled:opacity-60"
                      >
                        <XCircle size={10} />
                        Reject
                      </button>
                    </>
                  )}
                  <button
                    onClick={() => onViewDetail(order)}
                    className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs font-medium text-muted-foreground hover:bg-accent transition-colors"
                  >
                    <ChevronRight size={10} />
                    View
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Toast
// ─────────────────────────────────────────────────────────────────────────────

interface Toast {
  id: string
  message: string
  type: 'success' | 'error'
}

// ─────────────────────────────────────────────────────────────────────────────
// Main ProcurementPage
// ─────────────────────────────────────────────────────────────────────────────

export function ProcurementPage() {
  const qc = useQueryClient()
  const { hasPermission } = useAuthStore()
  const canManage = hasPermission('orders:write')

  const [activeTab, setActiveTab] = useState<'all' | 'mine'>('all')
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [rejectOrderId, setRejectOrderId] = useState<string | null>(null)
  const [detailOrder, setDetailOrder] = useState<VendorOrder | null>(null)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [statusFilter, setStatusFilter] = useState('')
  const [approvingId, setApprovingId] = useState<string | null>(null)

  const pushToast = (message: string, type: Toast['type'] = 'success') => {
    const id = Math.random().toString(36).slice(2)
    setToasts((prev) => [...prev, { id, message, type }])
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 4000)
  }

  // All orders query
  const allOrdersQuery = useQuery({
    queryKey: ['procurement-orders', 'all', statusFilter],
    queryFn: () => {
      const qs = statusFilter ? `?status=${encodeURIComponent(statusFilter)}` : ''
      return api.get<OrdersResponse>(`/procurement/orders${qs}`)
    },
    enabled: activeTab === 'all',
  })

  // My requests query
  const myOrdersQuery = useQuery({
    queryKey: ['procurement-orders', 'mine', statusFilter],
    queryFn: () => {
      const base = `/procurement/orders?requested_by=me`
      const qs = statusFilter ? `&status=${encodeURIComponent(statusFilter)}` : ''
      return api.get<OrdersResponse>(`${base}${qs}`)
    },
    enabled: activeTab === 'mine',
  })

  const activeQuery = activeTab === 'all' ? allOrdersQuery : myOrdersQuery
  const orders = activeQuery.data?.orders ?? []

  const createMut = useMutation({
    mutationFn: (data: { vendor_name: string; description: string; total_amount: number }) =>
      api.post<VendorOrder>('/procurement/orders', data),
    onSuccess: () => {
      pushToast('Order created successfully.')
      qc.invalidateQueries({ queryKey: ['procurement-orders'] })
      setShowCreateModal(false)
      setCreateError(null)
    },
    onError: (err) => {
      setCreateError(
        err instanceof PortalApiError ? err.error.message : 'Failed to create order.'
      )
    },
  })

  const approveMut = useMutation({
    mutationFn: (id: string) => api.post<unknown>(`/procurement/orders/${id}/approve`, {}),
    onSuccess: () => {
      pushToast('Order approved.')
      qc.invalidateQueries({ queryKey: ['procurement-orders'] })
      setApprovingId(null)
    },
    onError: (err) => {
      pushToast(
        err instanceof PortalApiError ? err.error.message : 'Failed to approve order.',
        'error'
      )
      setApprovingId(null)
    },
  })

  const rejectMut = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) =>
      api.post<unknown>(`/procurement/orders/${id}/reject`, { reason }),
    onSuccess: () => {
      pushToast('Order rejected.')
      qc.invalidateQueries({ queryKey: ['procurement-orders'] })
      setRejectOrderId(null)
    },
    onError: (err) => {
      pushToast(
        err instanceof PortalApiError ? err.error.message : 'Failed to reject order.',
        'error'
      )
    },
  })

  const handleApprove = (id: string) => {
    setApprovingId(id)
    approveMut.mutate(id)
  }

  const handleRejectSubmit = (id: string, reason: string) => {
    rejectMut.mutate({ id, reason })
  }

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {/* Toast notifications */}
      <div className="fixed top-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={`rounded-lg border px-4 py-2.5 text-sm shadow-md pointer-events-auto animate-in slide-in-from-right-2 duration-200 ${
              t.type === 'success'
                ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
                : 'border-red-200 bg-red-50 text-red-800'
            }`}
          >
            {t.message}
          </div>
        ))}
      </div>

      {/* Modals */}
      {showCreateModal && (
        <CreateOrderModal
          onClose={() => { setShowCreateModal(false); setCreateError(null) }}
          onSubmit={(data) => createMut.mutate(data)}
          isPending={createMut.isPending}
          error={createError}
        />
      )}
      {rejectOrderId && (
        <RejectModal
          orderId={rejectOrderId}
          onClose={() => setRejectOrderId(null)}
          onSubmit={handleRejectSubmit}
          isPending={rejectMut.isPending}
        />
      )}
      {detailOrder && (
        <OrderDetailPanel
          order={detailOrder}
          onClose={() => setDetailOrder(null)}
        />
      )}

      {/* Page header */}
      <div className="border-b bg-card/50 px-6 py-5">
        <div className="max-w-5xl mx-auto flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-primary/10 p-2">
              <ShoppingCart size={20} className="text-primary" />
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">Procurement</h1>
              <p className="text-xs text-muted-foreground mt-0.5">
                Manage vendor orders and procurement requests
              </p>
            </div>
          </div>
          <button
            onClick={() => setShowCreateModal(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
          >
            <Plus size={15} />
            Create Order
          </button>
        </div>
      </div>

      <div className="max-w-5xl mx-auto px-6 py-6">
        {/* Tab bar + status filter */}
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-6">
          <div className="flex border-b border-border">
            {([
              { id: 'all',  label: 'All Orders' },
              { id: 'mine', label: 'My Requests' },
            ] as const).map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`relative -mb-px px-4 py-2.5 text-sm font-medium transition-colors ${
                  activeTab === tab.id
                    ? 'border-b-2 border-primary text-primary'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          >
            <option value="">All statuses</option>
            <option value="pending">Pending</option>
            <option value="received">Approved</option>
            <option value="disputed">Disputed</option>
            <option value="closed">Closed</option>
          </select>
        </div>

        {/* Loading */}
        {activeQuery.isLoading && (
          <div className="flex items-center justify-center h-48 text-muted-foreground">
            <Loader2 size={20} className="animate-spin mr-2" />
            <span className="text-sm">Loading orders...</span>
          </div>
        )}

        {/* Error */}
        {activeQuery.isError && (
          <div className="flex items-center justify-center h-48 text-destructive">
            <AlertTriangle size={16} className="mr-2" />
            <span className="text-sm">
              {activeQuery.error instanceof PortalApiError
                ? activeQuery.error.error.message
                : 'Failed to load orders.'}
            </span>
          </div>
        )}

        {/* Orders table */}
        {!activeQuery.isLoading && !activeQuery.isError && (
          <>
            <p className="text-xs text-muted-foreground mb-3">
              {orders.length} order{orders.length !== 1 ? 's' : ''}
              {statusFilter ? ` with status "${statusFilter}"` : ''}
            </p>
            <OrdersTable
              orders={orders}
              canManage={canManage}
              onApprove={handleApprove}
              onReject={(id) => setRejectOrderId(id)}
              onViewDetail={(order) => setDetailOrder(order)}
              isApproving={approveMut.isPending}
              isRejecting={rejectMut.isPending}
              approvingId={approvingId}
            />
          </>
        )}
      </div>
    </div>
  )
}
