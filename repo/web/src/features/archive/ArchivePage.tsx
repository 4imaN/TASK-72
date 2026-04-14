// ArchivePage — browse the resource library by month or tag.
//
// Lists archive_buckets via GET /archive/buckets, then loads the resources
// for the selected bucket via GET /archive/buckets/:type/:key/resources.
// This is the read path that turns the bucket index into actual archive
// page browsing — the medium-severity gap the auditor called out.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../app/api/client'

interface ArchiveBucket {
  type: 'month' | 'tag' | string
  key: string
  label: string
  count: number
}

interface BucketResource {
  id: string
  title: string
  description?: string
  content_type: string
  category: string
  publish_date?: string
  view_count: number
}

interface BucketResourcesResponse {
  bucket_type: string
  bucket_key:  string
  resources:   BucketResource[]
  total:       number
  limit:       number
  offset:      number
}

export function ArchivePage() {
  const [bucketType, setBucketType] = useState<'month' | 'tag'>('month')
  const [selected, setSelected]     = useState<{ type: string; key: string; label: string } | null>(null)
  const [offset, setOffset]         = useState(0)
  const PAGE = 20

  const bucketsQuery = useQuery({
    queryKey: ['archive', 'buckets', bucketType],
    queryFn: () => api.get<{ buckets: ArchiveBucket[] }>(
      `/archive/buckets?type=${bucketType}`,
    ),
  })

  const resourcesQuery = useQuery({
    enabled: !!selected,
    queryKey: ['archive', 'bucket-resources', selected?.type, selected?.key, offset],
    queryFn: () => api.get<BucketResourcesResponse>(
      `/archive/buckets/${selected!.type}/${encodeURIComponent(selected!.key)}/resources?limit=${PAGE}&offset=${offset}`,
    ),
  })

  const buckets = bucketsQuery.data?.buckets ?? []
  const resources = resourcesQuery.data?.resources ?? []
  const total = resourcesQuery.data?.total ?? 0

  return (
    <div className="min-h-screen text-zinc-100" style={{ background: '#0f1219' }}>
      <div className="border-b border-white/[0.08] px-6 py-5">
        <h1 className="text-base font-semibold tracking-tight">Resource Archive</h1>
        <p className="mt-0.5 text-xs text-zinc-400">
          Browse the library by month or tag. Counts come from <code className="font-mono">archive_buckets</code>;
          listings are backed by <code className="font-mono">archive_membership</code>.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[280px_1fr] gap-6 px-6 py-5">
        {/* Bucket sidebar */}
        <aside className="space-y-3">
          <div className="inline-flex rounded-md border border-white/[0.08] overflow-hidden text-xs">
            {(['month', 'tag'] as const).map((t) => (
              <button
                key={t}
                onClick={() => { setBucketType(t); setSelected(null); setOffset(0) }}
                className={`px-3 py-1.5 transition-colors ${
                  bucketType === t
                    ? 'bg-[#161922] text-zinc-100'
                    : 'text-zinc-400 hover:bg-[#161922]'
                }`}
              >
                {t === 'month' ? 'By Month' : 'By Tag'}
              </button>
            ))}
          </div>

          {bucketsQuery.isLoading && <p className="text-xs text-zinc-500">Loading buckets…</p>}
          {bucketsQuery.error && <p className="text-xs text-red-400">Failed to load buckets.</p>}

          <ul className="divide-y divide-white/[0.06] border border-white/[0.08] rounded-md overflow-hidden">
            {buckets.length === 0 && !bucketsQuery.isLoading && (
              <li className="px-3 py-4 text-xs text-zinc-500 text-center">No buckets.</li>
            )}
            {buckets.map((b) => {
              const isActive = selected?.type === b.type && selected.key === b.key
              return (
                <li key={`${b.type}:${b.key}`}>
                  <button
                    onClick={() => { setSelected({ type: b.type, key: b.key, label: b.label }); setOffset(0) }}
                    className={`flex w-full items-center justify-between px-3 py-2 text-xs text-left transition-colors ${
                      isActive ? 'bg-[#161922] font-medium text-zinc-100' : 'hover:bg-[#161922]/60 text-zinc-400'
                    }`}
                  >
                    <span className="truncate">{b.label || b.key}</span>
                    <span className="text-[10px] text-zinc-500 tabular-nums">{b.count}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </aside>

        {/* Bucket contents */}
        <section>
          {!selected && (
            <p className="text-sm text-zinc-500">
              Select a bucket on the left to browse the resources it contains.
            </p>
          )}

          {selected && (
            <>
              <div className="mb-4 flex items-baseline justify-between">
                <h2 className="text-sm font-semibold text-zinc-100">{selected.label || selected.key}</h2>
                <span className="text-xs text-zinc-500 tabular-nums">
                  {total === 0 ? 'No resources' : `${offset + 1}–${Math.min(offset + PAGE, total)} of ${total}`}
                </span>
              </div>

              {resourcesQuery.isLoading && <p className="text-sm text-zinc-500">Loading resources…</p>}
              {resourcesQuery.error && <p className="text-sm text-red-400">Failed to load.</p>}

              <ul className="divide-y divide-white/[0.06] border border-white/[0.08] rounded-md">
                {resources.length === 0 && !resourcesQuery.isLoading && (
                  <li className="px-4 py-6 text-center text-sm text-zinc-500">No resources in this bucket.</li>
                )}
                {resources.map((r) => (
                  <li key={r.id} className="px-4 py-3">
                    <div className="flex items-start justify-between gap-4">
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium text-zinc-100">{r.title}</p>
                        {r.description && (
                          <p className="mt-0.5 text-xs text-zinc-400 line-clamp-2">{r.description}</p>
                        )}
                        <div className="mt-1 flex items-center gap-3 text-[11px] text-zinc-500">
                          <span className="uppercase tracking-wide">{r.content_type}</span>
                          <span>•</span>
                          <span>{r.category}</span>
                          {r.publish_date && (
                            <>
                              <span>•</span>
                              <span className="tabular-nums">{r.publish_date}</span>
                            </>
                          )}
                        </div>
                      </div>
                      <span className="text-[10px] text-zinc-500 tabular-nums shrink-0">
                        {r.view_count.toLocaleString()} views
                      </span>
                    </div>
                  </li>
                ))}
              </ul>

              {/* Pagination */}
              {total > PAGE && (
                <div className="mt-3 flex items-center justify-end gap-2">
                  <button
                    disabled={offset === 0}
                    onClick={() => setOffset(Math.max(0, offset - PAGE))}
                    className="rounded border border-white/[0.08] px-3 py-1 text-xs hover:bg-[#161922] disabled:opacity-50"
                  >
                    Previous
                  </button>
                  <button
                    disabled={offset + PAGE >= total}
                    onClick={() => setOffset(offset + PAGE)}
                    className="rounded border border-white/[0.08] px-3 py-1 text-xs hover:bg-[#161922] disabled:opacity-50"
                  >
                    Next
                  </button>
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  )
}
