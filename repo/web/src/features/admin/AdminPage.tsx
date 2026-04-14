// AdminPage — Configuration Center (Slice 9)
// Tabbed interface: Config Flags | Parameters | Version Rules | Export Jobs | Webhooks | Users | Audit Log
import { useState, useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, PortalApiError } from '../../app/api/client'
import { useAuthStore } from '../../app/store'

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

interface ConfigFlag {
  key: string
  enabled: boolean
  description?: string
  rollout_percentage: number
  target_roles: string[]
  updated_at: string
}

interface ConfigParam {
  key: string
  value: string
  description?: string
  updated_at: string
}

interface VersionRule {
  min_version: string
  max_version?: string
  action: 'block' | 'warn' | 'read_only'
  message?: string
  grace_until?: string
  created_at: string
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

interface WebhookEndpoint {
  id: string
  url: string
  events: string[]
  is_active: boolean
  created_by: string
  created_at: string
}

interface WebhookDelivery {
  id: string
  endpoint_id: string
  event_type: string
  status: 'pending' | 'delivered' | 'failed'
  attempts: number
  last_attempt_at?: string
  response_status?: number
  created_at: string
}

// ─────────────────────────────────────────────────────────────────────────────
// Small design-system primitives
// ─────────────────────────────────────────────────────────────────────────────

type BadgeVariant = 'green' | 'red' | 'yellow' | 'blue' | 'slate' | 'orange'

function Badge({ label, variant }: { label: string; variant: BadgeVariant }) {
  const styles: Record<BadgeVariant, string> = {
    green:  'bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/30',
    red:    'bg-red-500/15 text-red-400 ring-1 ring-red-500/30',
    yellow: 'bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30',
    blue:   'bg-sky-500/15 text-sky-400 ring-1 ring-sky-500/30',
    slate:  'bg-zinc-500/15 text-zinc-400 ring-1 ring-zinc-500/30',
    orange: 'bg-orange-500/15 text-orange-400 ring-1 ring-orange-500/30',
  }
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium tabular-nums ${styles[variant]}`}>
      {label}
    </span>
  )
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; variant: BadgeVariant }> = {
    queued:    { label: 'QUEUED',    variant: 'slate'  },
    running:   { label: 'RUNNING',   variant: 'blue'   },
    completed: { label: 'DONE',      variant: 'green'  },
    failed:    { label: 'FAILED',    variant: 'red'    },
    pending:   { label: 'PENDING',   variant: 'yellow' },
    delivered: { label: 'DELIVERED', variant: 'green'  },
    active:    { label: 'ACTIVE',    variant: 'green'  },
    inactive:  { label: 'INACTIVE',  variant: 'slate'  },
  }
  const cfg = map[status] ?? { label: status.toUpperCase(), variant: 'slate' as BadgeVariant }
  return <Badge label={cfg.label} variant={cfg.variant} />
}

function ActionBadge({ action }: { action: string }) {
  const map: Record<string, { label: string; variant: BadgeVariant }> = {
    block:     { label: 'BLOCK',     variant: 'red'    },
    warn:      { label: 'WARN',      variant: 'yellow' },
    read_only: { label: 'READ-ONLY', variant: 'blue'   },
  }
  const cfg = map[action] ?? { label: action.toUpperCase(), variant: 'slate' as BadgeVariant }
  return <Badge label={cfg.label} variant={cfg.variant} />
}

function Toggle({ enabled, onChange, disabled }: { enabled: boolean; onChange: () => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onChange}
      disabled={disabled}
      aria-pressed={enabled}
      className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors duration-150 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/40 disabled:opacity-40 disabled:cursor-not-allowed ${enabled ? 'bg-amber-600' : 'bg-zinc-600'}`}
    >
      <span className={`pointer-events-none inline-block h-3.5 w-3.5 transform rounded-full bg-zinc-100 shadow transition duration-150 ${enabled ? 'translate-x-4' : 'translate-x-0.5'}`} />
    </button>
  )
}

function Spinner() {
  return (
    <div className="flex items-center justify-center py-10">
      <div className="h-5 w-5 animate-spin rounded-full border-2 border-zinc-600 border-t-amber-400" />
    </div>
  )
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-14 text-zinc-500">
      <div className="mb-2 h-10 w-10 rounded-full border-2 border-dashed border-zinc-700 flex items-center justify-center">
        <span className="text-lg text-zinc-600">—</span>
      </div>
      <p className="text-sm">{message}</p>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Table shell
// ─────────────────────────────────────────────────────────────────────────────

function Table({ headers, children }: { headers: string[]; children: React.ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-zinc-700/50">
      <table className="min-w-full divide-y divide-zinc-700/30 text-sm">
        <thead className="bg-zinc-800/40">
          <tr>
            {headers.map((h) => (
              <th
                key={h}
                className="whitespace-nowrap px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-widest text-zinc-400"
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-700/30 bg-[#161922]">{children}</tbody>
      </table>
    </div>
  )
}

function TD({ children, mono }: { children: React.ReactNode; mono?: boolean }) {
  return (
    <td className={`px-4 py-2.5 text-zinc-300 ${mono ? 'font-mono text-xs' : ''}`}>
      {children}
    </td>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 1: Config Flags
// ─────────────────────────────────────────────────────────────────────────────

function ConfigFlagsTab() {
  const queryClient = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'config', 'flags'],
    queryFn: () => api.get<{ flags: ConfigFlag[] }>('/admin/config/flags'),
  })

  const updateMut = useMutation({
    mutationFn: ({ key, flag }: { key: string; flag: Partial<ConfigFlag> }) =>
      api.put(`/admin/config/flags/${key}`, flag),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'config', 'flags'] }),
  })

  const flags = data?.flags ?? []

  const toggleFlag = (flag: ConfigFlag) => {
    updateMut.mutate({
      key: flag.key,
      flag: {
        enabled: !flag.enabled,
        rollout_percentage: flag.rollout_percentage,
        target_roles: flag.target_roles,
      },
    })
  }

  if (isLoading) return <Spinner />
  if (error) return <div className="py-6 text-sm text-red-600">Failed to load flags.</div>
  if (flags.length === 0) return <EmptyState message="No config flags found." />

  return (
    <Table headers={['Flag Key', 'Enabled', 'Rollout %', 'Target Roles', 'Updated']}>
      {flags.map((flag) => (
        <tr key={flag.key} className="group hover:bg-zinc-800/40 transition-colors">
          <TD mono>{flag.key}</TD>
          <TD>
            <Toggle
              enabled={flag.enabled}
              onChange={() => toggleFlag(flag)}
              disabled={updateMut.isPending}
            />
          </TD>
          <TD>
            <span className="tabular-nums">{flag.rollout_percentage}%</span>
          </TD>
          <TD>
            <div className="flex flex-wrap gap-1">
              {flag.target_roles.length === 0 ? (
                <span className="text-zinc-500 text-xs">all</span>
              ) : (
                flag.target_roles.map((r) => (
                  <Badge key={r} label={r} variant="slate" />
                ))
              )}
            </div>
          </TD>
          <TD>
            <span className="text-xs text-zinc-500 tabular-nums">
              {new Date(flag.updated_at).toLocaleDateString()}
            </span>
          </TD>
        </tr>
      ))}
    </Table>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 2: Parameters
// ─────────────────────────────────────────────────────────────────────────────

function ParametersTab() {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'config', 'params'],
    queryFn: () => api.get<{ params: ConfigParam[] }>('/admin/config/params'),
  })

  const updateMut = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) =>
      api.put(`/admin/config/params/${key}`, { value }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'config', 'params'] })
      setEditing(null)
    },
  })

  const params = data?.params ?? []
  const isAdmin = useAuthStore((s) => s.isAdmin())

  const startEdit = (p: ConfigParam) => {
    setEditing(p.key)
    setDraft(p.value)
  }

  const saveEdit = (key: string) => {
    updateMut.mutate({ key, value: draft })
  }

  if (isLoading) return <Spinner />
  if (error) return <div className="py-6 text-sm text-red-600">Failed to load parameters.</div>
  if (params.length === 0) return <EmptyState message="No parameters found." />

  return (
    <Table headers={['Parameter Key', 'Value', 'Description', 'Updated']}>
      {params.map((p) => (
        <tr key={p.key} className="group hover:bg-zinc-800/40 transition-colors">
          <TD mono>{p.key}</TD>
          <td className="px-4 py-2 text-zinc-300">
            {editing === p.key ? (
              <div className="flex items-center gap-2">
                <input
                  autoFocus
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') saveEdit(p.key)
                    if (e.key === 'Escape') setEditing(null)
                  }}
                  className="w-48 rounded border border-zinc-600/50 px-2 py-1 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
                />
                <button
                  onClick={() => saveEdit(p.key)}
                  disabled={updateMut.isPending}
                  className="rounded bg-amber-600 px-2 py-1 text-xs text-white hover:bg-amber-500 disabled:opacity-50"
                >
                  Save
                </button>
                <button
                  onClick={() => setEditing(null)}
                  className="rounded px-2 py-1 text-xs text-zinc-400 hover:text-zinc-300"
                >
                  Cancel
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <span className="font-mono text-xs">{p.value}</span>
                {isAdmin && (
                  <button
                    onClick={() => startEdit(p)}
                    className="invisible group-hover:visible rounded px-1.5 py-0.5 text-xs text-zinc-500 hover:bg-zinc-800/50 hover:text-zinc-300"
                  >
                    Edit
                  </button>
                )}
              </div>
            )}
          </td>
          <TD>
            <span className="text-xs text-zinc-400">{p.description ?? '—'}</span>
          </TD>
          <TD>
            <span className="text-xs text-zinc-500 tabular-nums">
              {new Date(p.updated_at).toLocaleDateString()}
            </span>
          </TD>
        </tr>
      ))}
    </Table>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 3: Version Rules
// ─────────────────────────────────────────────────────────────────────────────

function VersionRulesTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({
    min_version: '',
    max_version: '',
    action: 'block',
    message: '',
    grace_period_days: '14', // matches the documented default grace window
  })
  const isAdmin = useAuthStore((s) => s.isAdmin())

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'config', 'version-rules'],
    queryFn: () => api.get<{ rules: VersionRule[] }>('/admin/config/version-rules'),
  })

  const createMut = useMutation({
    mutationFn: (rule: typeof form) => {
      // Transform the form into the backend payload. An empty string for
      // grace_period_days clears the grace window; otherwise parse to int.
      const payload: Record<string, unknown> = {
        min_version: rule.min_version,
        max_version: rule.max_version,
        action: rule.action,
        message: rule.message,
      }
      const days = rule.grace_period_days.trim() === '' ? 0 : Number(rule.grace_period_days)
      if (Number.isFinite(days) && days > 0) {
        payload.grace_period_days = days
      }
      return api.put('/admin/config/version-rules', payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'config', 'version-rules'] })
      setShowForm(false)
      setForm({ min_version: '', max_version: '', action: 'block', message: '', grace_period_days: '14' })
    },
  })

  const rules = data?.rules ?? []

  if (isLoading) return <Spinner />
  if (error) return <div className="py-6 text-sm text-red-600">Failed to load version rules.</div>

  return (
    <div className="space-y-4">
      {isAdmin && (
        <div className="flex justify-end">
          <button
            onClick={() => setShowForm(!showForm)}
            className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 transition-colors"
          >
            {showForm ? 'Cancel' : '+ Add Rule'}
          </button>
        </div>
      )}

      {showForm && (
        <div className="rounded-lg border border-zinc-700/50 bg-zinc-800/40 p-4 space-y-3">
          <p className="text-xs font-semibold uppercase tracking-widest text-zinc-400">New Version Rule</p>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Min Version</label>
              <input
                value={form.min_version}
                onChange={(e) => setForm({ ...form, min_version: e.target.value })}
                placeholder="1.0.0"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Max Version</label>
              <input
                value={form.max_version}
                onChange={(e) => setForm({ ...form, max_version: e.target.value })}
                placeholder="1.9.9 (optional)"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Action</label>
              <select
                value={form.action}
                onChange={(e) => setForm({ ...form, action: e.target.value })}
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40 bg-[#161922]"
              >
                <option value="block">Block</option>
                <option value="warn">Warn</option>
                <option value="read_only">Read-only</option>
              </select>
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Message</label>
              <input
                value={form.message}
                onChange={(e) => setForm({ ...form, message: e.target.value })}
                placeholder="Optional message"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Grace Period (days)</label>
              <input
                type="number"
                min="0"
                max="14"
                value={form.grace_period_days}
                onChange={(e) => setForm({ ...form, grace_period_days: e.target.value })}
                placeholder="14"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
              <p className="mt-1 text-[10px] text-zinc-500">
                Max 14 days. During grace, `block` is downgraded to `read_only`. 0 = no grace.
              </p>
            </div>
          </div>
          <div className="flex justify-end">
            <button
              onClick={() => createMut.mutate(form)}
              disabled={createMut.isPending || !form.min_version}
              className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50"
            >
              {createMut.isPending ? 'Saving…' : 'Save Rule'}
            </button>
          </div>
        </div>
      )}

      {rules.length === 0 ? (
        <EmptyState message="No version rules configured." />
      ) : (
        <Table headers={['Min Version', 'Max Version', 'Action', 'Message', 'Grace Until', 'Created']}>
          {rules.map((r, i) => (
            <tr key={i} className="hover:bg-zinc-800/40 transition-colors">
              <TD mono>{r.min_version}</TD>
              <TD mono>{r.max_version || '—'}</TD>
              <TD><ActionBadge action={r.action} /></TD>
              <TD>
                <span className="text-xs text-zinc-400">{r.message || '—'}</span>
              </TD>
              <TD>
                <span className="text-xs text-zinc-400 tabular-nums">
                  {r.grace_until ? new Date(r.grace_until).toLocaleDateString() : '—'}
                </span>
              </TD>
              <TD>
                <span className="text-xs text-zinc-500 tabular-nums">
                  {new Date(r.created_at).toLocaleDateString()}
                </span>
              </TD>
            </tr>
          ))}
        </Table>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// ─────────────────────────────────────────────────────────────────────────────
// Tab: Taxonomy (skill tags, synonyms, conflicts)
// ─────────────────────────────────────────────────────────────────────────────

interface Tag {
  id: number
  code: string
  canonical_name: string
}

interface Conflict {
  id: number
  synonym_text: string
  tag_id_a: number
  tag_id_b: number
  detected_at: string
  resolved_at?: string
}

function TaxonomyTab() {
  const qc = useQueryClient()

  const tagsQuery = useQuery({
    queryKey: ['admin', 'taxonomy', 'tags'],
    queryFn: () => api.get<{ tags: Tag[] }>('/taxonomy/tags'),
  })

  const conflictsQuery = useQuery({
    queryKey: ['admin', 'taxonomy', 'conflicts'],
    queryFn: () => api.get<{ conflicts: Conflict[] }>('/taxonomy/conflicts'),
  })

  const resolveMut = useMutation({
    mutationFn: (vars: { id: number; resolution: string }) =>
      api.post(`/taxonomy/conflicts/${vars.id}/resolve`, { resolution: vars.resolution }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'taxonomy'] }),
  })

  const addSynonymMut = useMutation({
    mutationFn: (vars: { tagId: number; text: string; type: string }) =>
      api.post(`/taxonomy/tags/${vars.tagId}/synonyms`, { text: vars.text, type: vars.type }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'taxonomy'] }),
  })

  const [synForm, setSynForm] = useState({ tagId: '', text: '', type: 'alias' })

  const tags = tagsQuery.data?.tags ?? []
  const conflicts = conflictsQuery.data?.conflicts ?? []

  if (tagsQuery.isLoading) return <Spinner />

  return (
    <div className="space-y-8">
      {/* Skill Tags */}
      <section>
        <h2 className="text-sm font-semibold text-zinc-200 mb-3">Skill Tags ({tags.length})</h2>
        {tags.length === 0 ? (
          <EmptyState message="No tags defined." />
        ) : (
          <Table headers={['ID', 'Code', 'Canonical Name']}>
            {tags.map((t) => (
              <tr key={t.id} className="hover:bg-zinc-800/30 transition-colors">
                <TD mono>{t.id}</TD>
                <TD mono>{t.code}</TD>
                <TD>{t.canonical_name}</TD>
              </tr>
            ))}
          </Table>
        )}
      </section>

      {/* Add Synonym */}
      <section>
        <h2 className="text-sm font-semibold text-zinc-200 mb-3">Add Synonym</h2>
        <div className="flex items-end gap-3 flex-wrap">
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Tag ID</label>
            <input value={synForm.tagId} onChange={(e) => setSynForm({ ...synForm, tagId: e.target.value })}
              placeholder="e.g. 1" className="w-20 rounded border border-zinc-600/50 bg-[#161922] px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-amber-500/40" />
          </div>
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Synonym Text</label>
            <input value={synForm.text} onChange={(e) => setSynForm({ ...synForm, text: e.target.value })}
              placeholder="e.g. python3" className="w-40 rounded border border-zinc-600/50 bg-[#161922] px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-amber-500/40" />
          </div>
          <div>
            <label className="block text-xs text-zinc-400 mb-1">Type</label>
            <select value={synForm.type} onChange={(e) => setSynForm({ ...synForm, type: e.target.value })}
              className="rounded border border-zinc-600/50 bg-[#161922] px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-amber-500/40">
              <option value="alias">Alias</option>
              <option value="pinyin">Pinyin</option>
            </select>
          </div>
          <button
            onClick={() => {
              if (!synForm.tagId || !synForm.text) return
              addSynonymMut.mutate({ tagId: Number(synForm.tagId), text: synForm.text, type: synForm.type })
              setSynForm({ tagId: '', text: '', type: 'alias' })
            }}
            disabled={addSynonymMut.isPending || !synForm.tagId || !synForm.text}
            className="rounded bg-amber-600 px-3 py-1 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50"
          >
            {addSynonymMut.isPending ? 'Adding...' : 'Add'}
          </button>
        </div>
        {addSynonymMut.isError && (
          <p className="mt-2 text-xs text-red-400">
            {addSynonymMut.error instanceof PortalApiError ? addSynonymMut.error.error.message : 'Failed'}
          </p>
        )}
      </section>

      {/* Conflicts */}
      <section>
        <h2 className="text-sm font-semibold text-zinc-200 mb-3">
          Open Conflicts ({conflicts.length})
        </h2>
        {conflicts.length === 0 ? (
          <EmptyState message="No unresolved conflicts." />
        ) : (
          <Table headers={['ID', 'Synonym', 'Tag A', 'Tag B', 'Detected', 'Resolve']}>
            {conflicts.map((c) => (
              <tr key={c.id} className="hover:bg-zinc-800/30 transition-colors">
                <TD mono>{c.id}</TD>
                <TD>{c.synonym_text}</TD>
                <TD mono>{c.tag_id_a}</TD>
                <TD mono>{c.tag_id_b}</TD>
                <TD><span className="text-xs text-zinc-500 tabular-nums">{new Date(c.detected_at).toLocaleDateString()}</span></TD>
                <TD>
                  <div className="flex gap-1">
                    {['deactivated_a', 'deactivated_b', 'merged'].map((r) => (
                      <button
                        key={r}
                        onClick={() => resolveMut.mutate({ id: c.id, resolution: r })}
                        disabled={resolveMut.isPending}
                        className="rounded border border-zinc-600/50 px-1.5 py-0.5 text-[10px] text-zinc-400 hover:bg-zinc-700/40 disabled:opacity-50"
                      >
                        {r.replace('_', ' ')}
                      </button>
                    ))}
                  </div>
                </TD>
              </tr>
            ))}
          </Table>
        )}
      </section>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab: Catalog (resource library CRUD)
// ─────────────────────────────────────────────────────────────────────────────

interface CatalogResource {
  id: string
  title: string
  description?: string
  content_type: string
  category: string
  publish_date?: string
  is_published: boolean
  is_archived: boolean
  view_count: number
  tags?: string[]
}

const CONTENT_TYPES = ['article', 'video', 'course', 'document'] as const
const CATEGORIES   = [
  'leadership', 'procurement', 'data', 'finance', 'communication',
  'project_mgmt', 'compliance', 'engineering',
] as const

function CatalogTab() {
  const qc = useQueryClient()
  const canWrite   = useAuthStore((s) => s.hasPermission('catalog:write'))
  const canPublish = useAuthStore((s) => s.hasPermission('catalog:publish'))

  const [editing, setEditing] = useState<CatalogResource | null>(null)
  const [creating, setCreating] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['catalog', 'admin-list'],
    // 100 covers the seeded fixture set; the table can grow with pagination later.
    queryFn: () => api.get<{ resources: CatalogResource[]; total: number }>(
      '/catalog/resources?limit=100',
    ),
  })

  const createMut = useMutation({
    mutationFn: (body: Partial<CatalogResource>) =>
      api.post('/catalog/resources', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['catalog', 'admin-list'] })
      setCreating(false)
    },
  })
  const updateMut = useMutation({
    mutationFn: (body: { id: string } & Partial<CatalogResource>) =>
      api.put(`/catalog/resources/${body.id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['catalog', 'admin-list'] })
      setEditing(null)
    },
  })
  const archiveMut = useMutation({
    mutationFn: (id: string) => api.post(`/catalog/resources/${id}/archive`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['catalog', 'admin-list'] }),
  })
  const restoreMut = useMutation({
    mutationFn: (id: string) => api.post(`/catalog/resources/${id}/restore`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['catalog', 'admin-list'] }),
  })

  const resources = data?.resources ?? []

  if (isLoading) return <Spinner />
  if (error) return <div className="py-6 text-sm text-red-600">Failed to load catalog.</div>

  return (
    <div className="space-y-4">
      {canWrite && !creating && !editing && (
        <div className="flex justify-end">
          <button
            onClick={() => setCreating(true)}
            className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 transition-colors"
          >
            + Add Resource
          </button>
        </div>
      )}

      {(creating || editing) && (
        <ResourceForm
          initial={editing ?? undefined}
          submitting={createMut.isPending || updateMut.isPending}
          onCancel={() => { setCreating(false); setEditing(null) }}
          onSubmit={(body) => {
            if (editing) {
              updateMut.mutate({ id: editing.id, ...body })
            } else {
              createMut.mutate(body)
            }
          }}
        />
      )}

      {resources.length === 0 ? (
        <EmptyState message="No resources in the library." />
      ) : (
        <Table headers={['Title', 'Type', 'Category', 'Status', 'Views', 'Actions']}>
          {resources.map((r) => (
            <tr key={r.id} className="hover:bg-zinc-800/40 transition-colors">
              <TD>
                <div className="font-medium text-zinc-200">{r.title}</div>
                {r.publish_date && (
                  <div className="text-[10px] text-zinc-500 tabular-nums">{r.publish_date}</div>
                )}
              </TD>
              <TD><span className="text-xs uppercase tracking-wide text-zinc-400">{r.content_type}</span></TD>
              <TD><span className="text-xs text-zinc-400">{r.category}</span></TD>
              <TD>
                {r.is_archived ? (
                  <span className="text-xs text-zinc-500 italic">archived</span>
                ) : r.is_published ? (
                  <span className="text-xs text-emerald-700">published</span>
                ) : (
                  <span className="text-xs text-amber-700">draft</span>
                )}
              </TD>
              <TD mono>{r.view_count.toLocaleString()}</TD>
              <TD>
                <div className="flex items-center gap-1.5">
                  {canWrite && !r.is_archived && (
                    <button
                      onClick={() => setEditing(r)}
                      className="rounded border border-zinc-600/50 px-2 py-0.5 text-[11px] hover:bg-zinc-800/50"
                    >
                      Edit
                    </button>
                  )}
                  {canPublish && !r.is_archived && (
                    <button
                      onClick={() => archiveMut.mutate(r.id)}
                      disabled={archiveMut.isPending}
                      className="rounded border border-red-300 px-2 py-0.5 text-[11px] text-red-700 hover:bg-red-50 disabled:opacity-50"
                    >
                      Archive
                    </button>
                  )}
                  {canPublish && r.is_archived && (
                    <button
                      onClick={() => restoreMut.mutate(r.id)}
                      disabled={restoreMut.isPending}
                      className="rounded border border-emerald-300 px-2 py-0.5 text-[11px] text-emerald-700 hover:bg-emerald-50 disabled:opacity-50"
                    >
                      Restore
                    </button>
                  )}
                </div>
              </TD>
            </tr>
          ))}
        </Table>
      )}
    </div>
  )
}

function ResourceForm({
  initial,
  onSubmit,
  onCancel,
  submitting,
}: {
  initial?: CatalogResource
  onSubmit: (body: Partial<CatalogResource>) => void
  onCancel: () => void
  submitting: boolean
}) {
  const [form, setForm] = useState({
    title:        initial?.title ?? '',
    description:  initial?.description ?? '',
    content_type: initial?.content_type ?? 'article',
    category:     initial?.category ?? 'leadership',
    publish_date: initial?.publish_date ?? '',
    is_published: initial?.is_published ?? true,
  })

  const isValid = form.title.trim() !== '' && form.content_type && form.category

  return (
    <div className="rounded-lg border border-zinc-700/50 bg-zinc-800/40 p-4 space-y-3">
      <p className="text-xs font-semibold uppercase tracking-widest text-zinc-400">
        {initial ? 'Edit Resource' : 'New Resource'}
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <label className="block text-xs text-zinc-400 mb-1">Title</label>
          <input
            value={form.title}
            onChange={(e) => setForm({ ...form, title: e.target.value })}
            className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
          />
        </div>
        <div className="sm:col-span-2">
          <label className="block text-xs text-zinc-400 mb-1">Description</label>
          <textarea
            rows={2}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
            className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
          />
        </div>
        <div>
          <label className="block text-xs text-zinc-400 mb-1">Content Type</label>
          <select
            value={form.content_type}
            onChange={(e) => setForm({ ...form, content_type: e.target.value })}
            className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40 bg-[#161922]"
          >
            {CONTENT_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </div>
        <div>
          <label className="block text-xs text-zinc-400 mb-1">Category</label>
          <select
            value={form.category}
            onChange={(e) => setForm({ ...form, category: e.target.value })}
            className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40 bg-[#161922]"
          >
            {CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
        <div>
          <label className="block text-xs text-zinc-400 mb-1">Publish Date</label>
          <input
            type="date"
            value={form.publish_date}
            onChange={(e) => setForm({ ...form, publish_date: e.target.value })}
            className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
          />
        </div>
        <div className="flex items-end gap-2">
          <label className="flex items-center gap-1.5 text-xs text-zinc-300 cursor-pointer">
            <input
              type="checkbox"
              checked={form.is_published}
              onChange={(e) => setForm({ ...form, is_published: e.target.checked })}
            />
            Published
          </label>
        </div>
      </div>
      <div className="flex justify-end gap-2">
        <button
          onClick={onCancel}
          className="rounded border border-zinc-600/50 px-3 py-1.5 text-xs hover:bg-zinc-800/50"
        >
          Cancel
        </button>
        <button
          onClick={() => onSubmit(form)}
          disabled={!isValid || submitting}
          className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50"
        >
          {submitting ? 'Saving…' : initial ? 'Save Changes' : 'Create Resource'}
        </button>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 5: Export Jobs
// ─────────────────────────────────────────────────────────────────────────────

function ExportJobsTab() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [jobType, setJobType] = useState('learning_progress_csv')

  const { data, isLoading, error } = useQuery({
    queryKey: ['exports', 'jobs'],
    queryFn: () => api.get<{ jobs: ExportJob[] }>('/exports/jobs'),
    refetchInterval: (query) => {
      const jobs = query.state.data?.jobs ?? []
      const hasActive = jobs.some((j) => j.status === 'queued' || j.status === 'running')
      return hasActive ? 3000 : false
    },
  })

  const createMut = useMutation({
    mutationFn: (type: string) =>
      api.post('/exports/jobs', { type }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['exports', 'jobs'] })
      setCreating(false)
    },
  })

  const jobs = data?.jobs ?? []

  if (isLoading) return <Spinner />
  if (error) return <div className="py-6 text-sm text-red-600">Failed to load export jobs.</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <select
          value={jobType}
          onChange={(e) => setJobType(e.target.value)}
          className="rounded border border-zinc-600/50 px-2 py-1.5 text-xs bg-[#161922] focus:outline-none focus:ring-1 focus:ring-amber-500/40"
        >
          <option value="learning_progress_csv">Learning Progress CSV</option>
          <option value="reconciliation_export">Reconciliation Export</option>
        </select>
        <button
          onClick={() => {
            setCreating(true)
            createMut.mutate(jobType)
          }}
          disabled={creating || createMut.isPending}
          className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50 transition-colors"
        >
          {createMut.isPending ? 'Creating…' : '+ New Export'}
        </button>
      </div>

      {jobs.length === 0 ? (
        <EmptyState message="No export jobs yet." />
      ) : (
        <Table headers={['Job ID', 'Type', 'Status', 'Created By', 'Created', 'Completed', 'Download']}>
          {jobs.map((job) => (
            <tr key={job.id} className="hover:bg-zinc-800/40 transition-colors">
              <TD mono>{job.id.slice(0, 8)}…</TD>
              <TD>
                <span className="text-xs text-zinc-400">
                  {job.type === 'learning_progress_csv' ? 'Learning CSV' : 'Reconciliation'}
                </span>
              </TD>
              <TD><StatusBadge status={job.status} /></TD>
              <TD>
                <span className="text-xs text-zinc-400">{job.created_by}</span>
              </TD>
              <TD>
                <span className="text-xs text-zinc-500 tabular-nums">
                  {new Date(job.created_at).toLocaleString()}
                </span>
              </TD>
              <TD>
                <span className="text-xs text-zinc-500 tabular-nums">
                  {job.completed_at ? new Date(job.completed_at).toLocaleString() : '—'}
                </span>
              </TD>
              <td className="px-4 py-2.5">
                {job.status === 'completed' ? (
                  <a
                    href={`/api/v1/exports/jobs/${job.id}/download`}
                    download
                    className="inline-flex items-center gap-1 rounded bg-zinc-800/50 px-2 py-1 text-xs font-medium text-zinc-300 hover:bg-zinc-700/50 transition-colors"
                  >
                    Download
                  </a>
                ) : job.status === 'failed' ? (
                  <span className="text-xs text-red-500" title={job.error_msg}>Error</span>
                ) : (
                  <span className="text-xs text-zinc-500">—</span>
                )}
              </td>
            </tr>
          ))}
        </Table>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 5: Webhooks
// ─────────────────────────────────────────────────────────────────────────────

function WebhooksTab() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [activeView, setActiveView] = useState<'endpoints' | 'deliveries'>('endpoints')
  const [form, setForm] = useState({ url: '', events: '', secret: '' })

  const endpointsQuery = useQuery({
    queryKey: ['admin', 'webhooks', 'endpoints'],
    queryFn: () => api.get<{ endpoints: WebhookEndpoint[] }>('/admin/webhooks'),
  })

  const deliveriesQuery = useQuery({
    queryKey: ['admin', 'webhooks', 'deliveries'],
    queryFn: () => api.get<{ deliveries: WebhookDelivery[] }>('/admin/webhooks/deliveries'),
    enabled: activeView === 'deliveries',
  })

  const createMut = useMutation({
    mutationFn: (data: { url: string; events: string[]; secret: string }) =>
      api.post('/admin/webhooks', data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
      setShowForm(false)
      setForm({ url: '', events: '', secret: '' })
    },
  })

  const processMut = useMutation({
    mutationFn: () => api.post('/admin/webhooks/process', {}),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin', 'webhooks', 'deliveries'] }),
  })

  const handleCreate = () => {
    const events = form.events
      .split(',')
      .map((e) => e.trim())
      .filter(Boolean)
    createMut.mutate({ url: form.url, events, secret: form.secret })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex rounded-lg border border-zinc-700/50 overflow-hidden">
          {(['endpoints', 'deliveries'] as const).map((view) => (
            <button
              key={view}
              onClick={() => setActiveView(view)}
              className={`px-3 py-1.5 text-xs font-medium capitalize transition-colors ${
                activeView === view
                  ? 'bg-amber-600 text-white'
                  : 'text-zinc-400 hover:text-zinc-300 hover:bg-zinc-800/40'
              }`}
            >
              {view}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          {activeView === 'deliveries' && (
            <button
              onClick={() => processMut.mutate()}
              disabled={processMut.isPending}
              className="rounded border border-zinc-600/50 px-3 py-1.5 text-xs font-medium text-zinc-400 hover:bg-zinc-800/40 disabled:opacity-50 transition-colors"
            >
              {processMut.isPending ? 'Processing…' : 'Process Pending'}
            </button>
          )}
          {activeView === 'endpoints' && (
            <button
              onClick={() => setShowForm(!showForm)}
              className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 transition-colors"
            >
              {showForm ? 'Cancel' : '+ Add Endpoint'}
            </button>
          )}
        </div>
      </div>

      {showForm && activeView === 'endpoints' && (
        <div className="rounded-lg border border-zinc-700/50 bg-zinc-800/40 p-4 space-y-3">
          <p className="text-xs font-semibold uppercase tracking-widest text-zinc-400">New Webhook Endpoint</p>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="sm:col-span-1">
              <label className="block text-xs text-zinc-400 mb-1">URL</label>
              <input
                value={form.url}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                placeholder="http://10.0.0.1:8080/hook"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Events (comma-separated)</label>
              <input
                value={form.events}
                onChange={(e) => setForm({ ...form, events: e.target.value })}
                placeholder="export.completed, settlement.approved"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-zinc-400 mb-1">Signing Secret</label>
              <input
                type="password"
                value={form.secret}
                onChange={(e) => setForm({ ...form, secret: e.target.value })}
                placeholder="secret"
                className="w-full rounded border border-zinc-600/50 px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
              />
            </div>
          </div>
          <div className="flex justify-end">
            <button
              onClick={handleCreate}
              disabled={createMut.isPending || !form.url}
              className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 disabled:opacity-50"
            >
              {createMut.isPending ? 'Saving…' : 'Save Endpoint'}
            </button>
          </div>
        </div>
      )}

      {activeView === 'endpoints' && (
        <>
          {endpointsQuery.isLoading && <Spinner />}
          {endpointsQuery.error && <div className="py-6 text-sm text-red-600">Failed to load endpoints.</div>}
          {!endpointsQuery.isLoading && (endpointsQuery.data?.endpoints ?? []).length === 0 && (
            <EmptyState message="No webhook endpoints configured." />
          )}
          {(endpointsQuery.data?.endpoints ?? []).length > 0 && (
            <Table headers={['ID', 'URL', 'Events', 'Active', 'Created By', 'Created']}>
              {(endpointsQuery.data?.endpoints ?? []).map((ep) => (
                <tr key={ep.id} className="hover:bg-zinc-800/40 transition-colors">
                  <TD mono>{ep.id.slice(0, 8)}…</TD>
                  <TD mono>{ep.url}</TD>
                  <TD>
                    <div className="flex flex-wrap gap-1">
                      {ep.events.map((ev) => (
                        <Badge key={ev} label={ev} variant="blue" />
                      ))}
                    </div>
                  </TD>
                  <TD>
                    <StatusBadge status={ep.is_active ? 'active' : 'inactive'} />
                  </TD>
                  <TD>
                    <span className="text-xs text-zinc-400">{ep.created_by}</span>
                  </TD>
                  <TD>
                    <span className="text-xs text-zinc-500 tabular-nums">
                      {new Date(ep.created_at).toLocaleDateString()}
                    </span>
                  </TD>
                </tr>
              ))}
            </Table>
          )}
        </>
      )}

      {activeView === 'deliveries' && (
        <>
          {deliveriesQuery.isLoading && <Spinner />}
          {deliveriesQuery.error && <div className="py-6 text-sm text-red-600">Failed to load deliveries.</div>}
          {!deliveriesQuery.isLoading && (deliveriesQuery.data?.deliveries ?? []).length === 0 && (
            <EmptyState message="No webhook deliveries yet." />
          )}
          {(deliveriesQuery.data?.deliveries ?? []).length > 0 && (
            <Table headers={['Delivery ID', 'Event Type', 'Status', 'Attempts', 'Response', 'Last Attempt']}>
              {(deliveriesQuery.data?.deliveries ?? []).map((d) => (
                <tr key={d.id} className="hover:bg-zinc-800/40 transition-colors">
                  <TD mono>{d.id.slice(0, 8)}…</TD>
                  <TD mono>{d.event_type}</TD>
                  <TD><StatusBadge status={d.status} /></TD>
                  <TD>
                    <span className="tabular-nums text-xs">{d.attempts}</span>
                  </TD>
                  <TD>
                    <span className="tabular-nums text-xs text-zinc-400">
                      {d.response_status ?? '—'}
                    </span>
                  </TD>
                  <TD>
                    <span className="text-xs text-zinc-500 tabular-nums">
                      {d.last_attempt_at ? new Date(d.last_attempt_at).toLocaleString() : '—'}
                    </span>
                  </TD>
                </tr>
              ))}
            </Table>
          )}
        </>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 6: Users
// ─────────────────────────────────────────────────────────────────────────────

interface AdminUser {
  id: string
  username: string
  email?: string
  roles: string[]
  last_login?: string
}

interface AdminUsersResponse {
  users: AdminUser[]
}

function maskEmail(email?: string): string {
  if (!email) return '—'
  const [local, domain] = email.split('@')
  if (!domain) return email
  const masked = local.slice(0, 2) + '***'
  return `${masked}@${domain}`
}

function EditRolesModal({
  user,
  onClose,
  onSave,
  isPending,
  error,
}: {
  user: AdminUser
  onClose: () => void
  onSave: (userId: string, roles: string[]) => void
  isPending: boolean
  error: string | null
}) {
  // Mirrors the seeded role names in seeds/001_bootstrap.sql. Keep in sync
  // when introducing new roles — there is no roles endpoint yet.
  const ALL_ROLES = ['admin', 'learner', 'procurement', 'approver', 'finance', 'moderator']
  const [selectedRoles, setSelectedRoles] = useState<string[]>([...user.roles])

  const toggle = (role: string) => {
    setSelectedRoles((prev) =>
      prev.includes(role) ? prev.filter((r) => r !== role) : [...prev, role]
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-sm rounded-xl border bg-card shadow-xl">
        <div className="flex items-center justify-between border-b px-5 py-4">
          <h2 className="font-semibold text-sm">Edit Roles — {user.username}</h2>
          <button onClick={onClose} className="rounded p-1 text-muted-foreground hover:bg-accent">
            ✕
          </button>
        </div>
        <div className="p-5 space-y-3">
          {error && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          <div className="space-y-2">
            {ALL_ROLES.map((role) => (
              <label key={role} className="flex items-center gap-3 cursor-pointer group">
                <input
                  type="checkbox"
                  checked={selectedRoles.includes(role)}
                  onChange={() => toggle(role)}
                  className="h-4 w-4 rounded border-zinc-600/50 accent-slate-800"
                />
                <span className="text-sm font-medium capitalize">{role}</span>
              </label>
            ))}
          </div>
          <div className="flex gap-3 justify-end pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border px-4 py-2 text-sm font-medium hover:bg-accent"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={isPending}
              onClick={() => onSave(user.id, selectedRoles)}
              className="rounded-lg bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-500 disabled:opacity-50"
            >
              {isPending ? 'Saving…' : 'Save Roles'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function UsersTab() {
  const queryClient = useQueryClient()
  const [editingUser, setEditingUser] = useState<AdminUser | null>(null)
  const [modalError, setModalError]   = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'users'],
    queryFn: () => api.get<AdminUsersResponse>('/admin/users'),
  })

  const updateRolesMut = useMutation({
    mutationFn: ({ userId, roles }: { userId: string; roles: string[] }) =>
      api.put(`/admin/users/${userId}/roles`, { roles }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
      setEditingUser(null)
      setModalError(null)
    },
    onError: (err) => {
      setModalError(
        err instanceof PortalApiError ? err.error.message : 'Failed to update roles.'
      )
    },
  })

  const users = data?.users ?? []

  if (isLoading) return <Spinner />
  if (error)     return <div className="py-6 text-sm text-red-600">Failed to load users.</div>
  if (users.length === 0) return <EmptyState message="No users found." />

  return (
    <>
      {editingUser && (
        <EditRolesModal
          user={editingUser}
          onClose={() => { setEditingUser(null); setModalError(null) }}
          onSave={(userId, roles) => updateRolesMut.mutate({ userId, roles })}
          isPending={updateRolesMut.isPending}
          error={modalError}
        />
      )}
      <Table headers={['Username', 'Email', 'Roles', 'Last Login', 'Actions']}>
        {users.map((u) => (
          <tr key={u.id} className="group hover:bg-zinc-800/40 transition-colors">
            <TD mono>{u.username}</TD>
            <TD>
              <span className="text-xs text-zinc-400">{maskEmail(u.email)}</span>
            </TD>
            <TD>
              <div className="flex flex-wrap gap-1">
                {u.roles.length === 0 ? (
                  <span className="text-xs text-zinc-500">—</span>
                ) : (
                  u.roles.map((r) => <Badge key={r} label={r} variant="slate" />)
                )}
              </div>
            </TD>
            <TD>
              <span className="text-xs text-zinc-500 tabular-nums">
                {u.last_login ? new Date(u.last_login).toLocaleString() : 'Never'}
              </span>
            </TD>
            <td className="px-4 py-2.5">
              <button
                onClick={() => { setEditingUser(u); setModalError(null) }}
                className="invisible group-hover:visible rounded bg-zinc-800/50 px-2 py-1 text-xs font-medium text-zinc-300 hover:bg-zinc-700/50 transition-colors"
              >
                Edit Roles
              </button>
            </td>
          </tr>
        ))}
      </Table>
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Tab 7: Audit Log
// ─────────────────────────────────────────────────────────────────────────────

interface AuditEntry {
  id: string
  timestamp: string
  user_id?: string
  username?: string
  action: string
  details?: string
}

interface AuditResponse {
  events: AuditEntry[]
  total?: number
}

const AUDIT_PAGE_SIZE = 50

function AuditLogTab() {
  const [filterUserId, setFilterUserId] = useState('')
  const [filterAction, setFilterAction] = useState('')
  const [offset, setOffset]             = useState(0)

  // Committed filters (applied on search)
  const [appliedUserId, setAppliedUserId] = useState('')
  const [appliedAction, setAppliedAction] = useState('')

  const buildQuery = () => {
    const params = new URLSearchParams()
    params.set('limit', String(AUDIT_PAGE_SIZE))
    params.set('offset', String(offset))
    if (appliedUserId) params.set('user_id', appliedUserId)
    if (appliedAction) params.set('action', appliedAction)
    return params.toString()
  }

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'audit', appliedUserId, appliedAction, offset],
    queryFn: () => api.get<AuditResponse>(`/admin/audit?${buildQuery()}`),
  })

  const applyFilters = () => {
    setOffset(0)
    setAppliedUserId(filterUserId)
    setAppliedAction(filterAction)
  }

  const entries = data?.events ?? []
  const total   = data?.total ?? entries.length
  const hasNext = offset + AUDIT_PAGE_SIZE < total
  const hasPrev = offset > 0

  if (isLoading && entries.length === 0) return <Spinner />

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label className="block text-xs text-zinc-400 mb-1">User ID</label>
          <input
            value={filterUserId}
            onChange={(e) => setFilterUserId(e.target.value)}
            placeholder="Filter by user ID"
            onKeyDown={(e) => e.key === 'Enter' && applyFilters()}
            className="w-40 rounded border border-zinc-600/50 px-2 py-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-amber-500/40"
          />
        </div>
        <div>
          <label className="block text-xs text-zinc-400 mb-1">Action</label>
          <input
            value={filterAction}
            onChange={(e) => setFilterAction(e.target.value)}
            placeholder="Filter by action"
            onKeyDown={(e) => e.key === 'Enter' && applyFilters()}
            className="w-40 rounded border border-zinc-600/50 px-2 py-1.5 text-xs focus:outline-none focus:ring-1 focus:ring-amber-500/40"
          />
        </div>
        <button
          onClick={applyFilters}
          className="rounded bg-amber-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-amber-500 transition-colors"
        >
          Apply
        </button>
        {(appliedUserId || appliedAction) && (
          <button
            onClick={() => {
              setFilterUserId('')
              setFilterAction('')
              setAppliedUserId('')
              setAppliedAction('')
              setOffset(0)
            }}
            className="rounded border border-zinc-600/50 px-3 py-1.5 text-xs font-medium text-zinc-400 hover:bg-zinc-800/40 transition-colors"
          >
            Clear
          </button>
        )}
      </div>

      {error && <div className="py-6 text-sm text-red-600">Failed to load audit log.</div>}

      {isLoading && <Spinner />}

      {!isLoading && entries.length === 0 && (
        <EmptyState message="No audit log entries found." />
      )}

      {!isLoading && entries.length > 0 && (
        <>
          <Table headers={['Timestamp', 'User', 'Action', 'Details']}>
            {entries.map((entry) => (
              <tr key={entry.id} className="hover:bg-zinc-800/40 transition-colors">
                <TD>
                  <span className="text-xs tabular-nums text-zinc-500">
                    {new Date(entry.timestamp).toLocaleString()}
                  </span>
                </TD>
                <TD>
                  <span className="text-xs font-mono">{entry.username ?? entry.user_id ?? '—'}</span>
                </TD>
                <TD mono>{entry.action}</TD>
                <TD>
                  <span className="text-xs text-zinc-400 line-clamp-1">{entry.details ?? '—'}</span>
                </TD>
              </tr>
            ))}
          </Table>

          {/* Pagination */}
          <div className="flex items-center justify-between pt-1">
            <span className="text-xs text-zinc-500 tabular-nums">
              Showing {offset + 1}–{Math.min(offset + AUDIT_PAGE_SIZE, total)} of {total}
            </span>
            <div className="flex gap-2">
              <button
                onClick={() => setOffset((o) => Math.max(0, o - AUDIT_PAGE_SIZE))}
                disabled={!hasPrev}
                className="rounded border border-zinc-600/50 px-3 py-1 text-xs font-medium text-zinc-400 hover:bg-zinc-800/40 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                Previous
              </button>
              <button
                onClick={() => setOffset((o) => o + AUDIT_PAGE_SIZE)}
                disabled={!hasNext}
                className="rounded border border-zinc-600/50 px-3 py-1 text-xs font-medium text-zinc-400 hover:bg-zinc-800/40 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main AdminPage
// ─────────────────────────────────────────────────────────────────────────────

const TABS = [
  { id: 'flags',        label: 'Config Flags',   adminOnly: true  },
  { id: 'params',       label: 'Parameters',     adminOnly: false },
  { id: 'versions',     label: 'Version Rules',  adminOnly: false },
  { id: 'taxonomy',     label: 'Taxonomy',       adminOnly: true  },
  { id: 'catalog',      label: 'Catalog',        adminOnly: false },
  { id: 'exports',      label: 'Export Jobs',    adminOnly: false },
  { id: 'webhooks',     label: 'Webhooks',       adminOnly: true  },
  { id: 'users',        label: 'Users',          adminOnly: false },
  { id: 'audit',        label: 'Audit Log',      adminOnly: false },
] as const

type TabId = typeof TABS[number]['id']

// Map URL path segments to tab IDs so sidebar links like /admin/users and
// /admin/audit open the correct tab instead of always defaulting to 'flags'.
const PATH_TO_TAB: Record<string, TabId> = {
  'users':    'users',
  'audit':    'audit',
  'config':   'flags',
  'taxonomy': 'taxonomy',
}

function tabFromPath(pathname: string): TabId | null {
  const lastSegment = pathname.split('/').filter(Boolean).pop() ?? ''
  return PATH_TO_TAB[lastSegment] ?? null
}

export function AdminPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const isAdmin = useAuthStore((s) => s.isAdmin())

  // Derive initial tab from URL (e.g. /admin/users → 'users', /admin/audit → 'audit')
  const [activeTab, setActiveTab] = useState<TabId>(() => tabFromPath(location.pathname) ?? 'flags')

  // When the URL changes externally (sidebar click), sync the tab
  useEffect(() => {
    const fromUrl = tabFromPath(location.pathname)
    if (fromUrl && fromUrl !== activeTab) {
      setActiveTab(fromUrl)
    }
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // When the user clicks a tab inside the page, update the URL to match
  function switchTab(tabId: TabId) {
    setActiveTab(tabId)
    // Keep the URL in sync with the active tab so the sidebar highlight matches
    if (tabId === 'users') navigate('/admin/users', { replace: true })
    else if (tabId === 'audit') navigate('/admin/audit', { replace: true })
    else if (tabId === 'taxonomy') navigate('/admin/taxonomy', { replace: true })
    else navigate('/admin/config', { replace: true })
  }

  // Non-admins skip admin-only tabs
  const visibleTabs = TABS.filter((t) => !t.adminOnly || isAdmin)

  // If current tab is hidden (no longer admin?), fall back
  const currentTab = visibleTabs.find((t) => t.id === activeTab) ? activeTab : visibleTabs[0]?.id ?? 'params'

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      {/* Header */}
      <div className="border-b border-white/[0.08] px-6 py-5" style={{ background: '#0f1219' }}>
        <div className="flex items-baseline gap-3">
          <h1 className="text-base font-semibold text-zinc-100 tracking-tight">
            Configuration Center
          </h1>
          <span className="text-xs text-zinc-500 font-mono">admin</span>
        </div>
        <p className="mt-0.5 text-xs text-zinc-400">
          Manage feature flags, system parameters, version compatibility, export jobs, webhooks, users, and audit logs.
        </p>
      </div>

      {/* Tab bar */}
      <div className="border-b border-white/[0.08] px-6" style={{ background: '#0f1219' }}>
        <nav className="flex gap-0" aria-label="Admin tabs">
          {visibleTabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => switchTab(tab.id)}
              className={`relative -mb-px px-4 py-3 text-xs font-medium transition-colors focus:outline-none ${
                currentTab === tab.id
                  ? 'border-b-2 border-zinc-100 text-zinc-100'
                  : 'text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </nav>
      </div>

      {/* Tab content */}
      <div className="px-6 py-5">
        {currentTab === 'flags' && <ConfigFlagsTab />}
        {currentTab === 'params' && <ParametersTab />}
        {currentTab === 'versions' && <VersionRulesTab />}
        {currentTab === 'taxonomy' && <TaxonomyTab />}
        {currentTab === 'catalog' && <CatalogTab />}
        {currentTab === 'exports' && <ExportJobsTab />}
        {currentTab === 'webhooks' && <WebhooksTab />}
        {currentTab === 'users' && <UsersTab />}
        {currentTab === 'audit' && <AuditLogTab />}
      </div>
    </div>
  )
}
