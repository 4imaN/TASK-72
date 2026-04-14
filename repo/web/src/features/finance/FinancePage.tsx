// FinancePage — Reconciliation & Settlement console (Slice 8)
import { useState, useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../app/api/client'
import { useAuthStore } from '../../app/store'

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

interface BillingRule {
  id: number
  rule_set_name: string
  version_number: number
  description: string
  effective_from: string
  effective_to?: string
  rules: Record<string, unknown>
  created_at: string
}

interface ReconciliationRun {
  id: string
  period: string
  status: 'pending' | 'processing' | 'completed' | 'failed'
  initiated_by: string
  created_at: string
  completed_at?: string
  summary?: {
    orders_evaluated: number
    variance_count: number
    total_delta: number
    processed_at: string
  }
}

interface Variance {
  id: string
  run_id: string
  vendor_order_id: string
  expected_amount: number
  actual_amount: number
  delta: number
  variance_type: string
  suggestion: string
  // Backend state machine (see reconciliation/store.go):
  //   open → pending_finance_approval → finance_approved → applied
  // 'ignored' is a terminal escape hatch handled out-of-band.
  status: 'open' | 'pending_finance_approval' | 'finance_approved' | 'applied' | 'ignored'
}

interface SettlementBatch {
  id: string
  run_id: string
  recon_run_id?: string
  status: 'draft' | 'under_review' | 'approved' | 'exported' | 'settled' | 'voided' | 'exception'
  created_by: string
  finance_approved_by?: string
  created_at: string
  updated_at: string
  approved_at?: string
  exported_at?: string
  lines?: SettlementLine[]
}

interface SettlementLine {
  id: string
  batch_id: string
  vendor_order_id?: string
  amount: number
  direction: 'AR' | 'AP'
  cost_center_id: string
}

interface ExportJob {
  id: string
  type: string
  status: 'queued' | 'running' | 'completed' | 'failed'
  created_by: string
  created_at: string
  completed_at?: string
  file_path?: string
  error_msg?: string
}

// ─────────────────────────────────────────────────────────────────────────────
// API helpers
// ─────────────────────────────────────────────────────────────────────────────

const RECON_BASE = '/reconciliation'

function fetchRules() {
  return api.get<{ rules: BillingRule[] }>(`${RECON_BASE}/rules`)
}

function fetchRuns() {
  return api.get<{ runs: ReconciliationRun[]; total: number }>(`${RECON_BASE}/runs`)
}

function fetchVariances(runId: string) {
  return api.get<{ variances: Variance[] }>(`${RECON_BASE}/runs/${runId}/variances`)
}

function fetchBatches() {
  return api.get<{ batches: SettlementBatch[] }>(`${RECON_BASE}/batches`)
}

// ─────────────────────────────────────────────────────────────────────────────
// Formatting helpers
// ─────────────────────────────────────────────────────────────────────────────

function fmtAmount(minorUnits: number): string {
  const abs = Math.abs(minorUnits)
  const sign = minorUnits < 0 ? '-' : minorUnits > 0 ? '+' : ''
  return `${sign}$${(abs / 100).toFixed(2)}`
}

function fmtDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    year: 'numeric', month: 'short', day: '2-digit',
  })
}

function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: '2-digit',
    hour: '2-digit', minute: '2-digit',
  })
}

// ─────────────────────────────────────────────────────────────────────────────
// Status badges
// ─────────────────────────────────────────────────────────────────────────────

type BadgeVariant =
  | 'pending' | 'processing' | 'completed' | 'failed'
  | 'draft' | 'under_review' | 'approved' | 'exported' | 'settled'
  | 'voided' | 'exception'
  | 'open' | 'pending_finance_approval' | 'finance_approved' | 'applied' | 'ignored'

const BADGE_CLASSES: Record<BadgeVariant, string> = {
  pending:                  'bg-amber-500/15 text-amber-300 border border-amber-500/30',
  processing:               'bg-blue-500/15 text-blue-300 border border-blue-500/30',
  completed:                'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
  failed:                   'bg-red-500/15 text-red-300 border border-red-500/30',
  draft:                    'bg-zinc-500/20 text-zinc-400 border border-zinc-500/30',
  under_review:             'bg-violet-500/15 text-violet-300 border border-violet-500/30',
  approved:                 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
  exported:                 'bg-cyan-500/15 text-cyan-300 border border-cyan-500/30',
  settled:                  'bg-teal-500/15 text-teal-300 border border-teal-500/30',
  voided:                   'bg-zinc-500/15 text-zinc-400 border border-zinc-500/30',
  exception:                'bg-orange-500/15 text-orange-300 border border-orange-500/30',
  open:                     'bg-amber-500/15 text-amber-300 border border-amber-500/30',
  pending_finance_approval: 'bg-violet-500/15 text-violet-300 border border-violet-500/30',
  finance_approved:         'bg-sky-500/15 text-sky-300 border border-sky-500/30',
  applied:                  'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
  ignored:                  'bg-zinc-500/15 text-zinc-400 border border-zinc-500/30',
}

function StatusBadge({ status }: { status: string }) {
  const cls = BADGE_CLASSES[status as BadgeVariant] ?? 'bg-zinc-500/15 text-zinc-300 border border-zinc-500/30'
  const label = status.replace('_', ' ')
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium tracking-wide uppercase ${cls}`}>
      {label}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab bar
// ─────────────────────────────────────────────────────────────────────────────

type Tab = 'imports' | 'runs' | 'batches' | 'exports' | 'rules'

function TabBar({ active, onChange }: { active: Tab; onChange: (t: Tab) => void }) {
  const canExport = useAuthStore((s) => s.hasPermission('exports:write'))
  const tabs: { id: Tab; label: string; hidden?: boolean }[] = [
    { id: 'imports', label: 'Statement Imports' },
    { id: 'runs',    label: 'Reconciliation Runs' },
    { id: 'batches', label: 'Settlement Batches' },
    { id: 'exports', label: 'Export Jobs', hidden: !canExport },
    { id: 'rules',   label: 'Billing Rules' },
  ]
  return (
    <div className="flex gap-1 border-b border-white/8 mb-6">
      {tabs.filter(t => !t.hidden).map(t => (
        <button
          key={t.id}
          onClick={() => onChange(t.id)}
          className={[
            'px-4 py-2.5 text-sm font-medium tracking-wide transition-colors',
            active === t.id
              ? 'text-emerald-300 border-b-2 border-emerald-400 -mb-px'
              : 'text-zinc-400 hover:text-zinc-200',
          ].join(' ')}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Create Run modal
// ─────────────────────────────────────────────────────────────────────────────

function CreateRunModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [period, setPeriod] = useState('')
  const [error, setError] = useState('')

  const mutation = useMutation({
    mutationFn: (p: string) => api.post<ReconciliationRun>(`${RECON_BASE}/runs`, { period: p }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['recon-runs'] })
      onClose()
    },
    onError: (e: Error) => setError(e.message),
  })

  function submit() {
    setError('')
    if (!period.match(/^\d{4}-\d{2}$/)) {
      setError('Period must be in YYYY-MM format')
      return
    }
    mutation.mutate(period)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-zinc-900 border border-white/12 rounded-xl p-6 w-full max-w-sm shadow-2xl">
        <h3 className="text-base font-semibold text-white mb-4">New Reconciliation Run</h3>
        <label className="block text-xs text-zinc-400 mb-1.5 font-medium tracking-wide uppercase">
          Billing Period
        </label>
        <input
          type="text"
          placeholder="2026-04"
          value={period}
          onChange={e => setPeriod(e.target.value)}
          className="w-full bg-zinc-800 border border-white/12 rounded-lg px-3 py-2 text-sm text-white placeholder:text-zinc-600 focus:outline-none focus:ring-1 focus:ring-emerald-500/60 mb-3 font-mono"
        />
        {error && <p className="text-red-400 text-xs mb-3">{error}</p>}
        <div className="flex gap-2 justify-end">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={submit}
            disabled={mutation.isPending}
            className="px-4 py-1.5 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {mutation.isPending ? 'Creating...' : 'Create Run'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Variance inline panel
// ─────────────────────────────────────────────────────────────────────────────

function VariancePanel({ runId, onClose }: { runId: string; onClose: () => void }) {
  const qc = useQueryClient()
  const canApprove = useAuthStore((s) => s.hasPermission('writeoffs:approve'))
  const canWriteVariance = useAuthStore((s) => s.hasPermission('reconciliation:write'))

  const { data, isLoading } = useQuery({
    queryKey: ['variances', runId],
    queryFn: () => fetchVariances(runId),
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ['variances', runId] })

  // Step 1: open → pending_finance_approval. Anyone with reconciliation:write
  // (the variance handler permission) can submit a writeoff for finance review.
  const submitApprovalMutation = useMutation({
    mutationFn: (varId: string) => api.post(`${RECON_BASE}/variances/${varId}/submit-approval`),
    onSuccess: invalidate,
  })

  // Step 2: pending_finance_approval → finance_approved. Gated server-side by
  // writeoffs:approve; we mirror that gate in the UI to keep the button hidden
  // for non-finance users.
  const approveMutation = useMutation({
    mutationFn: (varId: string) => api.post(`${RECON_BASE}/variances/${varId}/approve`),
    onSuccess: invalidate,
  })

  // Step 3: finance_approved → applied. Backend (ApplySuggestion) delegates to
  // ApplyApprovedVariance, so it will reject anything not yet approved.
  const applyMutation = useMutation({
    mutationFn: (varId: string) => api.post(`${RECON_BASE}/variances/${varId}/apply`),
    onSuccess: invalidate,
  })

  const variances = data?.variances ?? []

  return (
    <tr>
      <td colSpan={6} className="px-0">
        <div className="mx-4 mb-4 border border-white/10 rounded-xl bg-zinc-900/60 overflow-hidden">
          {/* Panel header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-white/8 bg-zinc-800/40">
            <p className="text-xs font-semibold text-zinc-300 tracking-wider uppercase">
              Variances — Run {runId.slice(0, 8)}...
            </p>
            <button
              onClick={onClose}
              className="text-zinc-500 hover:text-zinc-200 text-xs px-2 py-0.5 rounded hover:bg-white/8 transition-colors"
            >
              Close
            </button>
          </div>

          {isLoading && (
            <div className="px-4 py-6 text-center text-zinc-500 text-sm">Loading variances...</div>
          )}

          {!isLoading && variances.length === 0 && (
            <div className="px-4 py-6 text-center text-zinc-500 text-sm">
              No variances found for this run.
            </div>
          )}

          {!isLoading && variances.length > 0 && (
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-white/8">
                  <th className="text-left px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase">Order ID</th>
                  <th className="text-right px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase">Expected</th>
                  <th className="text-right px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase">Actual</th>
                  <th className="text-right px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase">Delta</th>
                  <th className="text-left px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase">Suggestion</th>
                  <th className="px-4 py-2.5 text-zinc-500 font-medium tracking-wider uppercase text-center">Status</th>
                  <th className="px-4 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {variances.map(v => (
                  <tr key={v.id} className="border-b border-white/5 hover:bg-white/3 transition-colors">
                    <td className="px-4 py-2.5 font-mono text-zinc-300">{v.vendor_order_id.slice(0, 12)}…</td>
                    <td className="px-4 py-2.5 text-right font-mono text-zinc-300">{fmtAmount(v.expected_amount)}</td>
                    <td className="px-4 py-2.5 text-right font-mono text-zinc-300">{fmtAmount(v.actual_amount)}</td>
                    <td className={`px-4 py-2.5 text-right font-mono font-semibold ${v.delta > 0 ? 'text-red-400' : 'text-emerald-400'}`}>
                      {fmtAmount(v.delta)}
                    </td>
                    <td className="px-4 py-2.5 text-zinc-400 max-w-xs truncate" title={v.suggestion}>{v.suggestion || '—'}</td>
                    <td className="px-4 py-2.5 text-center"><StatusBadge status={v.status} /></td>
                    <td className="px-4 py-2.5 text-right">
                      <div className="inline-flex items-center gap-1.5">
                        {v.status === 'open' && canWriteVariance && (
                          <button
                            onClick={() => submitApprovalMutation.mutate(v.id)}
                            disabled={submitApprovalMutation.isPending}
                            className="px-2.5 py-1 text-xs font-medium bg-violet-700/40 text-violet-200 border border-violet-600/30 rounded hover:bg-violet-700/60 transition-colors disabled:opacity-50"
                            title="Submit this writeoff suggestion for finance approval"
                          >
                            Submit for Approval
                          </button>
                        )}
                        {v.status === 'pending_finance_approval' && canApprove && (
                          <button
                            onClick={() => approveMutation.mutate(v.id)}
                            disabled={approveMutation.isPending}
                            className="px-2.5 py-1 text-xs font-medium bg-sky-700/40 text-sky-200 border border-sky-600/30 rounded hover:bg-sky-700/60 transition-colors disabled:opacity-50"
                            title="Approve writeoff (writeoffs:approve)"
                          >
                            Approve
                          </button>
                        )}
                        {v.status === 'pending_finance_approval' && !canApprove && (
                          <span className="text-[11px] text-zinc-500 italic">Awaiting finance</span>
                        )}
                        {v.status === 'finance_approved' && canWriteVariance && (
                          <button
                            onClick={() => applyMutation.mutate(v.id)}
                            disabled={applyMutation.isPending}
                            className="px-2.5 py-1 text-xs font-medium bg-emerald-700/40 text-emerald-300 border border-emerald-600/30 rounded hover:bg-emerald-700/60 transition-colors disabled:opacity-50"
                            title="Apply approved writeoff to the order"
                          >
                            Apply
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </td>
    </tr>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Runs tab
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Statement Imports tab
// ─────────────────────────────────────────────────────────────────────────────

interface ImportBatch {
  id: string
  imported_by: string
  source_file: string
  row_count: number
  status: string
  imported_at: string
}

interface StatementRow {
  order_id: string
  line_description: string
  statement_amount: number
  currency: string
  transaction_date: string
}

function StatementImportsTab() {
  const qc = useQueryClient()
  const canWrite = useAuthStore((s) => s.hasPermission('reconciliation:write'))
  const [showForm, setShowForm] = useState(false)
  const [sourceFile, setSourceFile] = useState('')
  const [rows, setRows] = useState<StatementRow[]>([
    { order_id: '', line_description: '', statement_amount: 0, currency: 'USD', transaction_date: '' },
  ])

  const { data, isLoading } = useQuery({
    queryKey: ['statement-imports'],
    queryFn: () => api.get<{ batches: ImportBatch[] }>(`${RECON_BASE}/statements`),
  })

  const importMut = useMutation({
    mutationFn: (body: { source_file: string; checksum: string; rows: StatementRow[] }) =>
      api.post(`${RECON_BASE}/statements`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['statement-imports'] })
      setShowForm(false)
      setSourceFile('')
      setRows([{ order_id: '', line_description: '', statement_amount: 0, currency: 'USD', transaction_date: '' }])
    },
  })

  const batches = data?.batches ?? []

  const addRow = () => setRows((prev) => [
    ...prev,
    { order_id: '', line_description: '', statement_amount: 0, currency: 'USD', transaction_date: '' },
  ])

  const updateRow = (i: number, field: keyof StatementRow, value: string | number) =>
    setRows((prev) => prev.map((r, idx) => idx === i ? { ...r, [field]: value } : r))

  const removeRow = (i: number) => setRows((prev) => prev.filter((_, idx) => idx !== i))

  const submit = () => {
    const valid = rows.filter((r) => r.line_description && r.transaction_date)
    if (valid.length === 0 || !sourceFile) return
    importMut.mutate({ source_file: sourceFile, checksum: '', rows: valid })
  }

  return (
    <>
      <div className="flex items-center justify-between mb-5">
        <p className="text-sm text-zinc-400">
          Import vendor statement data before running reconciliation. Each import creates a batch of rows
          that ProcessRun compares against vendor orders.
        </p>
        {canWrite && (
          <button
            onClick={() => setShowForm(!showForm)}
            className="flex items-center gap-1.5 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {showForm ? 'Cancel' : '+ Import Statements'}
          </button>
        )}
      </div>

      {showForm && (
        <div className="mb-6 border border-white/10 rounded-xl p-5 bg-zinc-900/50 space-y-4">
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Source File Name</label>
            <input
              value={sourceFile}
              onChange={(e) => setSourceFile(e.target.value)}
              placeholder="vendor_statement_2026-04.csv"
              className="w-full rounded-lg bg-zinc-800 border border-white/10 px-3 py-2 text-sm text-zinc-200 focus:outline-none focus:border-emerald-500/50"
            />
          </div>

          <div>
            <label className="block text-xs text-zinc-400 mb-2">Statement Rows</label>
            {rows.map((r, i) => (
              <div key={i} className="grid grid-cols-6 gap-2 mb-2">
                <input value={r.order_id} onChange={(e) => updateRow(i, 'order_id', e.target.value)}
                  placeholder="Order UUID (opt)" className="col-span-1 rounded border border-white/10 bg-zinc-800 px-2 py-1 text-xs text-zinc-200" />
                <input value={r.line_description} onChange={(e) => updateRow(i, 'line_description', e.target.value)}
                  placeholder="Description" className="col-span-2 rounded border border-white/10 bg-zinc-800 px-2 py-1 text-xs text-zinc-200" />
                <input type="number" value={r.statement_amount || ''} onChange={(e) => updateRow(i, 'statement_amount', Number(e.target.value))}
                  placeholder="Amount (cents)" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-xs text-zinc-200" />
                <input type="date" value={r.transaction_date} onChange={(e) => updateRow(i, 'transaction_date', e.target.value)}
                  className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-xs text-zinc-200" />
                <button onClick={() => removeRow(i)} className="text-red-400 hover:text-red-300 text-xs">Remove</button>
              </div>
            ))}
            <button onClick={addRow} className="text-emerald-400 hover:text-emerald-300 text-xs font-medium">+ Add Row</button>
          </div>

          <div className="flex justify-end">
            <button
              onClick={submit}
              disabled={importMut.isPending || !sourceFile || rows.every((r) => !r.line_description)}
              className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium rounded-lg disabled:opacity-50"
            >
              {importMut.isPending ? 'Importing...' : 'Import'}
            </button>
          </div>
        </div>
      )}

      {isLoading && <div className="py-12 text-center text-zinc-500 text-sm">Loading imports...</div>}

      {!isLoading && batches.length === 0 && !showForm && (
        <div className="py-16 text-center border border-dashed border-white/10 rounded-xl">
          <p className="text-zinc-400 text-sm mb-2">No statement imports yet</p>
          {canWrite ? (
            <button onClick={() => setShowForm(true)} className="text-emerald-400 text-sm font-medium">Import your first statement →</button>
          ) : (
            <p className="text-zinc-600 text-xs">You have read-only access.</p>
          )}
        </div>
      )}

      {batches.length > 0 && (
        <div className="border border-white/8 rounded-xl overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/8 bg-zinc-800/40 text-xs text-zinc-500 uppercase tracking-wider">
                <th className="text-left px-5 py-3">Source File</th>
                <th className="text-right px-5 py-3">Rows</th>
                <th className="px-5 py-3">Status</th>
                <th className="text-right px-5 py-3">Imported</th>
              </tr>
            </thead>
            <tbody>
              {batches.map((b) => (
                <tr key={b.id} className="border-b border-white/5 hover:bg-white/3 transition-colors">
                  <td className="px-5 py-3 text-sm text-zinc-300 font-mono">{b.source_file}</td>
                  <td className="px-5 py-3 text-right text-sm text-zinc-400 tabular-nums">{b.row_count}</td>
                  <td className="px-5 py-3"><StatusBadge status={b.status} /></td>
                  <td className="px-5 py-3 text-right text-sm text-zinc-400">{fmtDateTime(b.imported_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Runs tab
// ─────────────────────────────────────────────────────────────────────────────

function RunsTab() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null)
  // Gate write actions behind the permissions the routes actually enforce.
  const canWrite = useAuthStore((s) => s.hasPermission('reconciliation:write'))

  const { data, isLoading, error } = useQuery({
    queryKey: ['recon-runs'],
    queryFn: fetchRuns,
  })

  const processMutation = useMutation({
    mutationFn: (runId: string) => api.post<ReconciliationRun>(`${RECON_BASE}/runs/${runId}/process`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['recon-runs'] }),
  })

  const runs = data?.runs ?? []

  return (
    <>
      {showCreate && <CreateRunModal onClose={() => setShowCreate(false)} />}

      <div className="flex items-center justify-between mb-5">
        <div>
          <p className="text-sm text-zinc-400">
            Automated period-based reconciliation runs compare vendor statements against recorded orders.
          </p>
        </div>
        {canWrite && (
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium rounded-lg transition-colors shadow-lg shadow-emerald-900/40"
          >
            <span className="text-base leading-none">+</span>
            New Run
          </button>
        )}
      </div>

      {isLoading && (
        <div className="flex items-center justify-center py-16 text-zinc-500 text-sm">
          Loading reconciliation runs...
        </div>
      )}

      {error && (
        <div className="bg-red-900/20 border border-red-700/30 rounded-xl p-4 text-red-400 text-sm">
          Failed to load runs. Check your permissions.
        </div>
      )}

      {!isLoading && !error && runs.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 border border-dashed border-white/10 rounded-xl">
          <p className="text-zinc-400 text-sm mb-3">No reconciliation runs yet</p>
          {canWrite ? (
            <button
              onClick={() => setShowCreate(true)}
              className="text-emerald-400 hover:text-emerald-300 text-sm font-medium"
            >
              Create your first run →
            </button>
          ) : (
            <p className="text-zinc-600 text-xs">You have read-only access to this page.</p>
          )}
        </div>
      )}

      {!isLoading && runs.length > 0 && (
        <div className="border border-white/8 rounded-xl overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/8 bg-zinc-800/40">
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Period</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Status</th>
                <th className="text-right px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Variances</th>
                <th className="text-right px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Net Delta</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Created</th>
                <th className="px-5 py-3.5 text-right text-xs font-medium text-zinc-500 tracking-wider uppercase">Actions</th>
              </tr>
            </thead>
            <tbody>
              {runs.map(run => (
                <>
                  <tr
                    key={run.id}
                    className="border-b border-white/5 hover:bg-white/3 transition-colors"
                  >
                    <td className="px-5 py-4 font-mono font-semibold text-white">{run.period}</td>
                    <td className="px-5 py-4">
                      <StatusBadge status={run.status} />
                    </td>
                    <td className="px-5 py-4 text-right font-mono text-zinc-300">
                      {run.summary?.variance_count ?? '—'}
                    </td>
                    <td className="px-5 py-4 text-right font-mono">
                      {run.summary?.total_delta != null ? (
                        <span className={run.summary.total_delta > 0 ? 'text-red-400' : 'text-emerald-400'}>
                          {fmtAmount(run.summary.total_delta)}
                        </span>
                      ) : '—'}
                    </td>
                    <td className="px-5 py-4 text-sm text-zinc-400">{fmtDateTime(run.created_at)}</td>
                    <td className="px-5 py-4">
                      <div className="flex items-center gap-2 justify-end">
                        {run.status === 'pending' && canWrite && (
                          <button
                            onClick={() => processMutation.mutate(run.id)}
                            disabled={processMutation.isPending}
                            className="px-3 py-1.5 text-xs font-medium bg-blue-700/40 text-blue-300 border border-blue-600/30 rounded-md hover:bg-blue-700/60 transition-colors disabled:opacity-50"
                          >
                            {processMutation.isPending ? 'Processing...' : 'Process'}
                          </button>
                        )}
                        {run.status === 'completed' && (
                          <button
                            onClick={() => setExpandedRunId(expandedRunId === run.id ? null : run.id)}
                            className={[
                              'px-3 py-1.5 text-xs font-medium border rounded-md transition-colors',
                              expandedRunId === run.id
                                ? 'bg-violet-700/40 text-violet-300 border-violet-600/30'
                                : 'bg-white/5 text-zinc-300 border-white/10 hover:bg-white/10',
                            ].join(' ')}
                          >
                            {expandedRunId === run.id ? 'Hide Variances' : 'View Variances'}
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                  {expandedRunId === run.id && (
                    <VariancePanel
                      key={`var-${run.id}`}
                      runId={run.id}
                      onClose={() => setExpandedRunId(null)}
                    />
                  )}
                </>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Batches tab
// ─────────────────────────────────────────────────────────────────────────────

function VoidModal({ batchId, onClose }: { batchId: string; onClose: () => void }) {
  const qc = useQueryClient()
  const [reason, setReason] = useState('')

  const mutation = useMutation({
    mutationFn: () => api.post<SettlementBatch>(`${RECON_BASE}/batches/${batchId}/void`, { reason }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settlement-batches'] })
      onClose()
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-zinc-900 border border-white/12 rounded-xl p-6 w-full max-w-sm shadow-2xl">
        <h3 className="text-base font-semibold text-white mb-1">Void Batch</h3>
        <p className="text-xs text-zinc-500 mb-4">
          Batch <span className="font-mono text-zinc-300">{batchId.slice(0, 12)}…</span> will be permanently voided.
        </p>
        <label className="block text-xs text-zinc-400 mb-1.5 font-medium tracking-wide uppercase">
          Reason (optional)
        </label>
        <input
          type="text"
          placeholder="Duplicate entry"
          value={reason}
          onChange={e => setReason(e.target.value)}
          className="w-full bg-zinc-800 border border-white/12 rounded-lg px-3 py-2 text-sm text-white placeholder:text-zinc-600 focus:outline-none focus:ring-1 focus:ring-red-500/60 mb-4"
        />
        <div className="flex gap-2 justify-end">
          <button onClick={onClose} className="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">
            Cancel
          </button>
          <button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="px-4 py-1.5 bg-red-700 hover:bg-red-600 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {mutation.isPending ? 'Voiding...' : 'Void Batch'}
          </button>
        </div>
      </div>
    </div>
  )
}

function BatchActions({ batch }: { batch: SettlementBatch }) {
  const qc = useQueryClient()
  const [voidModal, setVoidModal] = useState(false)
  // Gate every batch-mutation button behind the real route permissions.
  const canSettle = useAuthStore((s) => s.hasPermission('settlements:write'))

  const submit = useMutation({
    mutationFn: () => api.post<SettlementBatch>(`${RECON_BASE}/batches/${batch.id}/submit`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settlement-batches'] }),
  })

  const approve = useMutation({
    mutationFn: () => api.post<SettlementBatch>(`${RECON_BASE}/batches/${batch.id}/approve`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settlement-batches'] }),
  })

  const settle = useMutation({
    mutationFn: () => api.post<SettlementBatch>(`${RECON_BASE}/batches/${batch.id}/settle`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settlement-batches'] }),
  })

  function exportBatch() {
    // Download CSV via a fetch + blob URL
    fetch(`/api/v1${RECON_BASE}/batches/${batch.id}/export`, {
      method: 'POST',
      credentials: 'include',
    }).then(async res => {
      if (!res.ok) return
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `settlement-batch-${batch.id}.csv`
      a.click()
      URL.revokeObjectURL(url)
      qc.invalidateQueries({ queryKey: ['settlement-batches'] })
    })
  }

  const btnBase = 'px-2.5 py-1 text-xs font-medium rounded border transition-colors disabled:opacity-50 whitespace-nowrap'

  return (
    <>
      {voidModal && <VoidModal batchId={batch.id} onClose={() => setVoidModal(false)} />}
      <div className="flex items-center gap-1.5 justify-end flex-wrap">
        {batch.status === 'draft' && canSettle && (
          <>
            <button
              onClick={() => submit.mutate()}
              disabled={submit.isPending}
              className={`${btnBase} bg-violet-700/30 text-violet-300 border-violet-600/30 hover:bg-violet-700/50`}
            >
              Submit
            </button>
            <button
              onClick={() => setVoidModal(true)}
              className={`${btnBase} bg-red-900/20 text-red-400 border-red-700/20 hover:bg-red-900/30`}
            >
              Void
            </button>
          </>
        )}
        {batch.status === 'under_review' && canSettle && (
          <>
            <button
              onClick={() => approve.mutate()}
              disabled={approve.isPending}
              className={`${btnBase} bg-emerald-700/30 text-emerald-300 border-emerald-600/30 hover:bg-emerald-700/50`}
            >
              Approve
            </button>
            <button
              onClick={() => setVoidModal(true)}
              className={`${btnBase} bg-red-900/20 text-red-400 border-red-700/20 hover:bg-red-900/30`}
            >
              Void
            </button>
          </>
        )}
        {batch.status === 'approved' && canSettle && (
          <button
            onClick={exportBatch}
            className={`${btnBase} bg-cyan-700/30 text-cyan-300 border-cyan-600/30 hover:bg-cyan-700/50`}
          >
            Export CSV
          </button>
        )}
        {batch.status === 'exported' && canSettle && (
          <button
            onClick={() => settle.mutate()}
            disabled={settle.isPending}
            className={`${btnBase} bg-teal-700/30 text-teal-300 border-teal-600/30 hover:bg-teal-700/50`}
          >
            Mark Settled
          </button>
        )}
        {!canSettle && !['settled','voided'].includes(batch.status) && (
          <span className="text-xs text-zinc-600 italic">View only</span>
        )}
        {(batch.status === 'settled' || batch.status === 'voided') && (
          <span className="text-xs text-zinc-600 italic">No actions</span>
        )}
      </div>
    </>
  )
}

function BatchesTab() {
  const qc = useQueryClient()
  const canSettle = useAuthStore((s) => s.hasPermission('settlements:write'))
  const [showCreateBatch, setShowCreateBatch] = useState(false)
  const [cbRunId, setCbRunId]     = useState('')
  const [cbLines, setCbLines]     = useState<Array<{
    vendor_order_id: string; amount: string; direction: string;
    cost_center_id: string; department_code: string; cost_center: string
  }>>([{ vendor_order_id: '', amount: '', direction: 'AP', cost_center_id: '', department_code: '', cost_center: '' }])

  const { data, isLoading, error } = useQuery({
    queryKey: ['settlement-batches'],
    queryFn: fetchBatches,
  })

  const createBatchMut = useMutation({
    mutationFn: (body: { run_id: string; lines: Array<{
      vendor_order_id?: string; amount: number; direction: string;
      cost_center_id: string; allocations?: Array<{ department_code: string; cost_center: string; amount: number }>
    }> }) => api.post(`${RECON_BASE}/batches`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settlement-batches'] })
      setShowCreateBatch(false)
      setCbRunId('')
      setCbLines([{ vendor_order_id: '', amount: '', direction: 'AP', cost_center_id: '', department_code: '', cost_center: '' }])
    },
  })

  const submitBatch = () => {
    if (!cbRunId) return
    const lines = cbLines
      .filter((l) => l.amount && l.direction)
      .map((l) => ({
        vendor_order_id: l.vendor_order_id || undefined,
        amount: Number(l.amount),
        direction: l.direction,
        cost_center_id: l.cost_center_id,
        allocations: (l.department_code || l.cost_center)
          ? [{ department_code: l.department_code, cost_center: l.cost_center, amount: Number(l.amount) }]
          : undefined,
      }))
    if (lines.length === 0) return
    createBatchMut.mutate({ run_id: cbRunId, lines })
  }

  const batches = data?.batches ?? []

  return (
    <>
      <div className="flex items-center justify-between mb-5">
        <p className="text-sm text-zinc-400">
          Settlement batches consolidate reconciled variance adjustments into AR/AP journal entries for finance approval.
        </p>
        {canSettle && (
          <button
            onClick={() => setShowCreateBatch(!showCreateBatch)}
            className="flex items-center gap-1.5 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {showCreateBatch ? 'Cancel' : '+ Create Batch'}
          </button>
        )}
      </div>

      {showCreateBatch && (
        <div className="mb-6 border border-white/10 rounded-xl p-5 bg-zinc-900/50 space-y-4">
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Reconciliation Run ID</label>
            <input value={cbRunId} onChange={(e) => setCbRunId(e.target.value)}
              placeholder="UUID of the completed recon run"
              className="w-full rounded-lg bg-zinc-800 border border-white/10 px-3 py-2 text-sm text-zinc-200 focus:outline-none focus:border-emerald-500/50" />
          </div>
          <div>
            <label className="block text-xs text-zinc-400 mb-2">Lines</label>
            {cbLines.map((l, i) => (
              <div key={i} className="grid grid-cols-7 gap-2 mb-2 text-xs">
                <input value={l.vendor_order_id} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, vendor_order_id: e.target.value }; setCbLines(n) }}
                  placeholder="Order UUID (opt)" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200" />
                <input type="number" value={l.amount} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, amount: e.target.value }; setCbLines(n) }}
                  placeholder="Amount (cents)" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200" />
                <select value={l.direction} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, direction: e.target.value }; setCbLines(n) }}
                  className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200">
                  <option value="AP">AP</option><option value="AR">AR</option>
                </select>
                <input value={l.cost_center_id} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, cost_center_id: e.target.value }; setCbLines(n) }}
                  placeholder="Cost Center ID" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200" />
                <input value={l.department_code} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, department_code: e.target.value }; setCbLines(n) }}
                  placeholder="Dept Code" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200" />
                <input value={l.cost_center} onChange={(e) => { const n = [...cbLines]; n[i] = { ...l, cost_center: e.target.value }; setCbLines(n) }}
                  placeholder="Alloc Center" className="rounded border border-white/10 bg-zinc-800 px-2 py-1 text-zinc-200" />
                <button onClick={() => setCbLines((p) => p.filter((_, idx) => idx !== i))} className="text-red-400 hover:text-red-300">Remove</button>
              </div>
            ))}
            <button onClick={() => setCbLines((p) => [...p, { vendor_order_id: '', amount: '', direction: 'AP', cost_center_id: '', department_code: '', cost_center: '' }])}
              className="text-emerald-400 hover:text-emerald-300 text-xs font-medium">+ Add Line</button>
          </div>
          <div className="flex justify-end">
            <button onClick={submitBatch} disabled={createBatchMut.isPending || !cbRunId}
              className="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium rounded-lg disabled:opacity-50">
              {createBatchMut.isPending ? 'Creating...' : 'Create Batch'}
            </button>
          </div>
        </div>
      )}

      {isLoading && (
        <div className="flex items-center justify-center py-16 text-zinc-500 text-sm">
          Loading settlement batches...
        </div>
      )}

      {error && (
        <div className="bg-red-900/20 border border-red-700/30 rounded-xl p-4 text-red-400 text-sm">
          Failed to load batches.
        </div>
      )}

      {!isLoading && !error && batches.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 border border-dashed border-white/10 rounded-xl">
          <p className="text-zinc-400 text-sm">No settlement batches yet</p>
          <p className="text-zinc-600 text-xs mt-1">Batches are created via the API after processing a reconciliation run.</p>
        </div>
      )}

      {!isLoading && batches.length > 0 && (
        <div className="border border-white/8 rounded-xl overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/8 bg-zinc-800/40">
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Batch ID</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Status</th>
                <th className="text-right px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Lines</th>
                <th className="text-right px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Total AP</th>
                <th className="text-right px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Total AR</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Approved By</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Created</th>
                <th className="px-5 py-3.5 text-right text-xs font-medium text-zinc-500 tracking-wider uppercase">Actions</th>
              </tr>
            </thead>
            <tbody>
              {batches.map(batch => {
                const lines = batch.lines ?? []
                const apTotal = lines.filter(l => l.direction === 'AP').reduce((s, l) => s + l.amount, 0)
                const arTotal = lines.filter(l => l.direction === 'AR').reduce((s, l) => s + l.amount, 0)
                return (
                  <tr key={batch.id} className="border-b border-white/5 hover:bg-white/3 transition-colors">
                    <td className="px-5 py-4 font-mono text-zinc-300 text-xs">
                      {batch.id.slice(0, 12)}…
                    </td>
                    <td className="px-5 py-4">
                      <StatusBadge status={batch.status} />
                    </td>
                    <td className="px-5 py-4 text-right font-mono text-zinc-400 text-xs">
                      {lines.length}
                    </td>
                    <td className="px-5 py-4 text-right font-mono text-xs text-zinc-300">
                      {apTotal > 0 ? fmtAmount(apTotal) : '—'}
                    </td>
                    <td className="px-5 py-4 text-right font-mono text-xs text-zinc-300">
                      {arTotal > 0 ? fmtAmount(arTotal) : '—'}
                    </td>
                    <td className="px-5 py-4 text-xs text-zinc-400 font-mono">
                      {batch.finance_approved_by ? batch.finance_approved_by.slice(0, 10) + '…' : '—'}
                    </td>
                    <td className="px-5 py-4 text-xs text-zinc-400">{fmtDate(batch.created_at)}</td>
                    <td className="px-5 py-4">
                      <BatchActions batch={batch} />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Billing rules tab
// ─────────────────────────────────────────────────────────────────────────────

function RulesTab() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['billing-rules'],
    queryFn: fetchRules,
  })

  const rules = data?.rules ?? []

  return (
    <>
      <div className="mb-5">
        <p className="text-sm text-zinc-400">
          Versioned billing rule configurations governing payment terms, late fees, variance thresholds, and write-off limits.
        </p>
      </div>

      {isLoading && (
        <div className="flex items-center justify-center py-16 text-zinc-500 text-sm">Loading...</div>
      )}
      {error && (
        <div className="bg-red-900/20 border border-red-700/30 rounded-xl p-4 text-red-400 text-sm">
          Failed to load billing rules.
        </div>
      )}

      {!isLoading && rules.length > 0 && (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rules.map(rule => (
            <div key={rule.id} className="border border-white/8 rounded-xl bg-zinc-800/30 p-4">
              <div className="flex items-start justify-between mb-3">
                <div>
                  <p className="text-sm font-semibold text-white capitalize">
                    {rule.rule_set_name.replace(/_/g, ' ')}
                  </p>
                  <p className="text-xs text-zinc-500 mt-0.5">
                    Version {rule.version_number} · Effective {fmtDate(rule.effective_from)}
                    {rule.effective_to && ` → ${fmtDate(rule.effective_to)}`}
                  </p>
                </div>
                <span className="text-xs font-mono bg-white/8 text-zinc-300 px-2 py-0.5 rounded border border-white/10">
                  v{rule.version_number}
                </span>
              </div>
              {rule.description && (
                <p className="text-xs text-zinc-500 mb-3">{rule.description}</p>
              )}
              <div className="space-y-1">
                {Object.entries(rule.rules as Record<string, unknown>).map(([k, v]) => (
                  <div key={k} className="flex items-center justify-between text-xs">
                    <span className="text-zinc-500 font-mono">{k.replace(/_/g, ' ')}</span>
                    <span className="text-zinc-300 font-mono">
                      {typeof v === 'object' ? JSON.stringify(v).slice(0, 30) + '…' : String(v)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Export Jobs tab (reconciliation exports — finance-facing)
// ─────────────────────────────────────────────────────────────────────────────

function ExportJobsTab() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['finance-export-jobs'],
    queryFn: () => api.get<{ jobs: ExportJob[] }>('/exports/jobs'),
    refetchInterval: (query) => {
      const jobs = query.state.data?.jobs ?? []
      const hasActive = jobs.some((j) => j.status === 'queued' || j.status === 'running')
      return hasActive ? 3000 : false
    },
  })

  const createMut = useMutation({
    mutationFn: () =>
      api.post('/exports/jobs', { type: 'reconciliation_export' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['finance-export-jobs'] })
      setCreating(false)
    },
  })

  // Only show reconciliation_export jobs (filter out learning_progress_csv if backend returns all for admins)
  const jobs = (data?.jobs ?? []).filter((j) => j.type === 'reconciliation_export')

  return (
    <>
      <div className="flex items-center justify-between mb-5">
        <p className="text-sm text-zinc-400">
          Reconciliation export jobs generate CSV files from completed reconciliation runs for offline processing.
        </p>
        <button
          onClick={() => {
            setCreating(true)
            createMut.mutate()
          }}
          disabled={creating || createMut.isPending}
          className="flex items-center gap-1.5 px-4 py-2 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
        >
          {createMut.isPending ? 'Creating…' : '+ New Reconciliation Export'}
        </button>
      </div>

      {isLoading && (
        <div className="flex items-center justify-center py-16 text-zinc-500 text-sm">
          Loading export jobs...
        </div>
      )}

      {error && (
        <div className="bg-red-900/20 border border-red-700/30 rounded-xl p-4 text-red-400 text-sm">
          Failed to load export jobs.
        </div>
      )}

      {!isLoading && !error && jobs.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 border border-dashed border-white/10 rounded-xl">
          <p className="text-zinc-400 text-sm">No reconciliation export jobs yet</p>
          <p className="text-zinc-600 text-xs mt-1">Click "+ New Reconciliation Export" to generate a CSV from your reconciliation data.</p>
        </div>
      )}

      {!isLoading && jobs.length > 0 && (
        <div className="border border-white/8 rounded-xl overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-white/8 bg-zinc-800/40">
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Job ID</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Status</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Created</th>
                <th className="text-left px-5 py-3.5 text-xs font-medium text-zinc-500 tracking-wider uppercase">Completed</th>
                <th className="px-5 py-3.5 text-right text-xs font-medium text-zinc-500 tracking-wider uppercase">Download</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.id} className="border-b border-white/5 hover:bg-white/3 transition-colors">
                  <td className="px-5 py-4 font-mono text-zinc-300 text-xs">
                    {job.id.slice(0, 12)}…
                  </td>
                  <td className="px-5 py-4">
                    <StatusBadge status={job.status} />
                  </td>
                  <td className="px-5 py-4 text-xs text-zinc-400">
                    {fmtDateTime(job.created_at)}
                  </td>
                  <td className="px-5 py-4 text-xs text-zinc-400">
                    {job.completed_at ? fmtDateTime(job.completed_at) : '—'}
                  </td>
                  <td className="px-5 py-4 text-right">
                    {job.status === 'completed' ? (
                      <a
                        href={`/api/v1/exports/jobs/${job.id}/download`}
                        download
                        className="inline-flex items-center gap-1 rounded-lg bg-emerald-700/30 text-emerald-300 border border-emerald-600/30 px-2.5 py-1 text-xs font-medium hover:bg-emerald-700/50 transition-colors"
                      >
                        Download CSV
                      </a>
                    ) : job.status === 'failed' ? (
                      <span className="text-xs text-red-400" title={job.error_msg}>
                        Failed
                      </span>
                    ) : (
                      <span className="text-xs text-zinc-600">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main page
// ─────────────────────────────────────────────────────────────────────────────

// Map URL path segments to tabs so sidebar links open the right one.
const FINANCE_PATH_TO_TAB: Record<string, Tab> = {
  'reconciliation': 'runs',
  'settlements':    'batches',
}

function financeTabFromPath(pathname: string): Tab | null {
  const last = pathname.split('/').filter(Boolean).pop() ?? ''
  return FINANCE_PATH_TO_TAB[last] ?? null
}

export function FinancePage() {
  const location = useLocation()
  const navigate = useNavigate()

  const [tab, setTab] = useState<Tab>(() => financeTabFromPath(location.pathname) ?? 'imports')

  // Sync tab when sidebar navigation changes the URL
  useEffect(() => {
    const fromUrl = financeTabFromPath(location.pathname)
    if (fromUrl && fromUrl !== tab) {
      setTab(fromUrl)
    }
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // Update URL when user clicks a tab inside the page
  function switchTab(t: Tab) {
    setTab(t)
    if (t === 'batches') navigate('/finance/settlements', { replace: true })
    else navigate('/finance/reconciliation', { replace: true })
  }

  return (
    <div
      className="min-h-screen p-6 lg:p-8"
      style={{
        background: 'linear-gradient(160deg, #0d1117 0%, #111827 50%, #0d1117 100%)',
      }}
    >
      {/* Page header */}
      <div className="mb-8">
        <div className="flex items-center gap-3 mb-1.5">
          {/* Ledger icon */}
          <div className="w-8 h-8 rounded-lg bg-emerald-500/15 border border-emerald-500/30 flex items-center justify-center">
            <svg
              viewBox="0 0 20 20"
              fill="none"
              className="w-4 h-4 text-emerald-400"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <rect x="3" y="3" width="14" height="14" rx="2" />
              <path d="M7 7h6M7 10h6M7 13h4" />
            </svg>
          </div>
          <h1
            className="text-xl font-semibold tracking-tight"
            style={{
              fontFamily: '"Source Serif 4", "Georgia", serif',
              color: '#f4f4f5',
              letterSpacing: '-0.01em',
            }}
          >
            Finance &amp; Reconciliation
          </h1>
        </div>
        <p className="text-sm text-zinc-500 ml-11">
          Reconcile vendor statements, manage settlement batches, and track billing rules.
        </p>
      </div>

      {/* Tab navigation */}
      <TabBar active={tab} onChange={switchTab} />

      {/* Tab content */}
      {tab === 'imports' && <StatementImportsTab />}
      {tab === 'runs'    && <RunsTab />}
      {tab === 'batches' && <BatchesTab />}
      {tab === 'exports' && <ExportJobsTab />}
      {tab === 'rules'   && <RulesTab />}
    </div>
  )
}
