// LibraryPage — resource library with full search, filters, sort, and results.
import { useState, useRef, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { api } from '../../app/api/client'
import {
  Search, SlidersHorizontal, X, BookOpen, Video, FileText,
  GraduationCap, TrendingUp, Clock, Sparkles, ChevronDown,
  Eye, Calendar, Tag, Zap, Star, Users, ChevronLeft, ChevronRight,
  Info
} from 'lucide-react'

// ── Types ──────────────────────────────────────────────────────────────────────

interface Resource {
  id: string
  title: string
  description?: string
  content_type: string
  category: string
  tags: string[]
  view_count: number
  publish_date?: string
  rank?: number
  matched_synonyms?: string[]
}

interface SearchResponse {
  results: Resource[]
  total: number
  limit: number
  offset: number
  expanded_synonyms?: string[]
}

interface TraceFactor {
  factor: string
  weight: number
  label: string
}

interface RecommendedItem {
  resource_id: string
  title: string
  content_type: string
  category: string
  score: number
  factors: TraceFactor[]
}

interface RecommendationsResponse {
  items: RecommendedItem[]
}

interface SearchForm {
  q: string
  category: string
  content_type: string
  sort: string
  synonyms: boolean
  fuzzy: boolean
  pinyin: boolean
  tags: string      // comma-separated; sent to backend as ?tags=a,b,c
  from_date: string // YYYY-MM-DD
  to_date: string   // YYYY-MM-DD
}

// ── Constants ──────────────────────────────────────────────────────────────────

const CONTENT_TYPE_ICONS: Record<string, React.ReactNode> = {
  article: <FileText size={13} />,
  video: <Video size={13} />,
  course: <GraduationCap size={13} />,
  document: <BookOpen size={13} />,
}

const CONTENT_TYPE_COLORS: Record<string, string> = {
  article:  'bg-sky-950/60 text-sky-300 border-sky-800/50',
  video:    'bg-rose-950/60 text-rose-300 border-rose-800/50',
  course:   'bg-amber-950/60 text-amber-300 border-amber-800/50',
  document: 'bg-emerald-950/60 text-emerald-300 border-emerald-800/50',
}

const FACTOR_ICONS: Record<string, React.ReactNode> = {
  'popular in your role':    <Star size={9} />,
  'matches your skills':     <Zap size={9} />,
  'recently viewed by peers': <Users size={9} />,
  'trending':                <TrendingUp size={9} />,
  'complete your path':      <BookOpen size={9} />,
}

const FACTOR_COLORS: Record<string, string> = {
  'popular in your role':    'bg-amber-950/70 text-amber-300 border-amber-700/40',
  'matches your skills':     'bg-violet-950/70 text-violet-300 border-violet-700/40',
  'recently viewed by peers': 'bg-sky-950/70 text-sky-300 border-sky-700/40',
  'trending':                'bg-emerald-950/70 text-emerald-300 border-emerald-700/40',
  'complete your path':      'bg-rose-950/70 text-rose-300 border-rose-700/40',
}

const CATEGORIES = [
  { value: '',             label: 'All categories' },
  { value: 'leadership',   label: 'Leadership' },
  { value: 'procurement',  label: 'Procurement' },
  { value: 'data',         label: 'Data' },
  { value: 'finance',      label: 'Finance' },
  { value: 'communication',label: 'Communication' },
  { value: 'project_mgmt', label: 'Project Management' },
  { value: 'compliance',   label: 'Compliance' },
  { value: 'engineering',  label: 'Engineering' },
]

const CONTENT_TYPES = [
  { value: '',         label: 'All types' },
  { value: 'article',  label: 'Article' },
  { value: 'video',    label: 'Video' },
  { value: 'course',   label: 'Course' },
  { value: 'document', label: 'Document' },
]

// ── RecommendationCard sub-component ─────────────────────────────────────────

function RecommendationCard({ item, index }: { item: RecommendedItem; index: number }) {
  const [tooltipVisible, setTooltipVisible] = useState(false)
  const topFactors = item.factors.slice(0, 2)

  const handleCardClick = () => {
    // Fire-and-forget behavior event for recommendation click
    api.post('/recommendations/events', {
      resource_id: item.resource_id,
      event_type: 'click',
    }).catch(() => {})
  }

  return (
    <div
      className="rec-card relative flex-shrink-0 w-[220px] rounded-xl p-4 flex flex-col gap-3 cursor-default"
      style={{ animationDelay: `${index * 0.07}s` }}
      onClick={handleCardClick}
    >
      {/* Content type badge + "Why?" button */}
      <div className="flex items-center justify-between">
        <div className={`flex items-center gap-1.5 px-2 py-1 rounded-md border text-[10px] font-medium ${CONTENT_TYPE_COLORS[item.content_type] ?? 'bg-[rgba(255,255,255,0.06)] text-[#9ca3af] border-[rgba(255,255,255,0.10)]'}`}>
          {CONTENT_TYPE_ICONS[item.content_type] ?? <FileText size={11} />}
          <span className="capitalize">{item.content_type}</span>
        </div>

        {/* Why recommended? popover trigger */}
        <div className="relative">
          <button
            type="button"
            onMouseEnter={() => setTooltipVisible(true)}
            onMouseLeave={() => setTooltipVisible(false)}
            onFocus={() => setTooltipVisible(true)}
            onBlur={() => setTooltipVisible(false)}
            className="w-5 h-5 rounded-full flex items-center justify-center text-[#4b5563] hover:text-[#9ca3af] transition-colors"
            aria-label="Why recommended?"
          >
            <Info size={12} />
          </button>

          {tooltipVisible && (
            <div className="rec-tooltip absolute right-0 top-7 z-50 w-52 rounded-xl p-3 pointer-events-none">
              <p className="text-[10px] font-semibold text-[#9ca3af] uppercase tracking-wide mb-2">
                Why recommended?
              </p>
              {item.factors.length === 0 && (
                <p className="text-[11px] text-[#6b7280]">Based on popular resources.</p>
              )}
              {item.factors.map((f, i) => (
                <div key={i} className="flex items-start gap-1.5 mb-1.5 last:mb-0">
                  <span className="mt-0.5 text-amber-500/80 flex-shrink-0">
                    {FACTOR_ICONS[f.label] ?? <Star size={9} />}
                  </span>
                  <span className="text-[11px] text-[#c9cdd8] leading-tight">{f.label}</span>
                </div>
              ))}
              <div className="mt-2 pt-2 border-t border-[rgba(255,255,255,0.07)]">
                <div className="flex items-center gap-1">
                  <div className="h-1 flex-1 rounded-full bg-[rgba(255,255,255,0.06)] overflow-hidden">
                    <div
                      className="h-full rounded-full bg-amber-500/60"
                      style={{ width: `${Math.min(item.score * 100, 100)}%` }}
                    />
                  </div>
                  <span className="text-[9px] text-[#4b5563] tabular-nums">
                    {(item.score * 100).toFixed(0)}%
                  </span>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Title */}
      <h3 className="text-sm font-medium text-[#f0f1f4] leading-snug line-clamp-2 flex-1">
        {item.title}
      </h3>

      {/* Factor badges */}
      {topFactors.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {topFactors.map((f, i) => {
            const colorClass = FACTOR_COLORS[f.label] ?? 'bg-[rgba(255,255,255,0.06)] text-[#9ca3af] border-[rgba(255,255,255,0.10)]'
            return (
              <span
                key={i}
                className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full border text-[9px] font-medium ${colorClass}`}
              >
                {FACTOR_ICONS[f.label] ?? <Star size={8} />}
                {f.label}
              </span>
            )
          })}
        </div>
      )}
    </div>
  )
}

// ── Main LibraryPage ───────────────────────────────────────────────────────────

export function LibraryPage() {
  const [params, setParams] = useState<Record<string, string>>({})
  const [showFilters, setShowFilters] = useState(false)
  const [hasSearched, setHasSearched] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const carouselRef = useRef<HTMLDivElement>(null)
  const [canScrollLeft, setCanScrollLeft] = useState(false)
  const [canScrollRight, setCanScrollRight] = useState(false)

  const { register, handleSubmit, watch, setValue, reset } = useForm<SearchForm>({
    defaultValues: {
      q: '',
      category: '',
      content_type: '',
      sort: 'relevance',
      synonyms: true,
      fuzzy: true,
      pinyin: false,
      tags: '',
      from_date: '',
      to_date: '',
    },
  })

  const watchedQ = watch('q')
  const watchedCategory = watch('category')
  const watchedContentType = watch('content_type')
  const watchedTags = watch('tags')
  const watchedFromDate = watch('from_date')
  const watchedToDate = watch('to_date')

  const { data, isLoading, isError, isFetching } = useQuery({
    queryKey: ['search', params],
    queryFn: () => {
      const qs = new URLSearchParams(params).toString()
      return api.get<SearchResponse>(`/search${qs ? '?' + qs : ''}`)
    },
    staleTime: 30_000,
  })

  const { data: recData } = useQuery({
    queryKey: ['recommendations'],
    queryFn: () => api.get<RecommendationsResponse>('/recommendations?limit=5'),
    staleTime: 60_000,
    retry: false,
  })

  const recommendations = recData?.items ?? []
  const hasRecommendations = recommendations.length > 0

  // Update scroll arrow state
  const checkScroll = () => {
    const el = carouselRef.current
    if (!el) return
    setCanScrollLeft(el.scrollLeft > 8)
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 8)
  }

  useEffect(() => {
    checkScroll()
  }, [recommendations])

  const scrollCarousel = (dir: 'left' | 'right') => {
    const el = carouselRef.current
    if (!el) return
    el.scrollBy({ left: dir === 'left' ? -240 : 240, behavior: 'smooth' })
    setTimeout(checkScroll, 300)
  }

  function onSearch(form: SearchForm) {
    const p: Record<string, string> = {}
    if (form.q) p.q = form.q
    if (form.category) p.category = form.category
    if (form.content_type) p.content_type = form.content_type
    if (form.sort && form.sort !== 'relevance') p.sort = form.sort
    if (!form.synonyms) p.synonyms = 'false'
    if (!form.fuzzy) p.fuzzy = 'false'
    if (form.pinyin) p.pinyin = 'true'
    const cleanedTags = form.tags
      .split(',')
      .map(t => t.trim())
      .filter(Boolean)
      .join(',')
    if (cleanedTags) p.tags = cleanedTags
    if (form.from_date) p.from_date = form.from_date
    if (form.to_date) p.to_date = form.to_date
    setParams(p)
    setHasSearched(true)
  }

  function clearSearch() {
    reset()
    setParams({})
    setHasSearched(false)
    inputRef.current?.focus()
  }

  // Auto-trigger search when sort or filters change after initial search
  useEffect(() => {
    if (hasSearched) {
      handleSubmit(onSearch)()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watchedCategory, watchedContentType, watchedTags, watchedFromDate, watchedToDate])

  const activeFilterCount = [
    watchedCategory,
    watchedContentType,
    watchedTags,
    watchedFromDate,
    watchedToDate,
  ].filter(Boolean).length

  const results = data?.results ?? []
  const total = data?.total ?? 0

  return (
    <div className="min-h-screen text-[#e2e4ea]" style={{ background: '#0f1219', fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif" }}>
      <style>{`
        .library-heading {
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
          letter-spacing: -0.02em;
        }

        .search-input::-webkit-search-cancel-button { display: none; }

        .result-card {
          background: rgba(255,255,255,0.025);
          border: 1px solid rgba(255,255,255,0.06);
          transition: border-color 0.2s ease, background 0.2s ease, transform 0.15s ease, box-shadow 0.2s ease;
          border-radius: 14px;
        }
        .result-card:hover {
          background: rgba(255,255,255,0.045);
          border-color: rgba(245,158,11,0.3);
          transform: translateY(-2px);
          box-shadow: 0 8px 24px rgba(0,0,0,0.3), 0 0 0 1px rgba(245,158,11,0.1);
        }

        .filter-pill {
          background: rgba(255,255,255,0.04);
          border: 1px solid rgba(255,255,255,0.08);
          transition: border-color 0.15s, background 0.15s;
          border-radius: 10px;
        }
        .filter-pill:hover {
          background: rgba(255,255,255,0.09);
          border-color: rgba(255,255,255,0.20);
        }
        .filter-pill.active {
          background: rgba(245,158,11,0.12);
          border-color: rgba(245,158,11,0.4);
          color: rgb(245,158,11);
        }

        .sort-btn {
          transition: color 0.12s, border-color 0.12s;
        }
        .sort-btn.active {
          color: rgb(245,158,11);
          border-bottom: 2px solid rgb(245,158,11);
        }

        .tag-chip {
          background: rgba(99,102,241,0.12);
          border: 1px solid rgba(99,102,241,0.25);
          color: rgb(165,180,252);
          font-size: 0.7rem;
          padding: 1px 7px;
          border-radius: 9999px;
          display: inline-flex;
          align-items: center;
          gap: 3px;
        }

        .skeleton {
          animation: skeleton-pulse 1.4s ease-in-out infinite;
          background: linear-gradient(90deg, rgba(255,255,255,0.04) 25%, rgba(255,255,255,0.08) 50%, rgba(255,255,255,0.04) 75%);
          background-size: 200% 100%;
          border-radius: 4px;
        }
        @keyframes skeleton-pulse {
          0% { background-position: 200% 0; }
          100% { background-position: -200% 0; }
        }

        .fade-in {
          animation: fadeIn 0.25s ease;
        }
        @keyframes fadeIn {
          from { opacity: 0; transform: translateY(4px); }
          to   { opacity: 1; transform: translateY(0); }
        }

        .rec-fade-in {
          animation: recFadeIn 0.3s ease both;
        }
        @keyframes recFadeIn {
          from { opacity: 0; transform: translateY(6px) scale(0.98); }
          to   { opacity: 1; transform: translateY(0) scale(1); }
        }

        .checkbox-toggle {
          appearance: none;
          width: 32px;
          height: 18px;
          background: rgba(255,255,255,0.12);
          border-radius: 9999px;
          position: relative;
          cursor: pointer;
          transition: background 0.2s;
          flex-shrink: 0;
        }
        .checkbox-toggle:checked {
          background: rgb(245,158,11);
        }
        .checkbox-toggle::after {
          content: '';
          position: absolute;
          top: 2px;
          left: 2px;
          width: 14px;
          height: 14px;
          border-radius: 9999px;
          background: white;
          transition: transform 0.2s;
        }
        .checkbox-toggle:checked::after {
          transform: translateX(14px);
        }

        /* Recommendation carousel */
        .rec-carousel {
          display: flex;
          gap: 12px;
          overflow-x: auto;
          scroll-snap-type: x mandatory;
          scrollbar-width: none;
          -ms-overflow-style: none;
          padding-bottom: 4px;
        }
        .rec-carousel::-webkit-scrollbar { display: none; }

        .rec-card {
          scroll-snap-align: start;
          background: linear-gradient(145deg, rgba(255,255,255,0.04) 0%, rgba(245,158,11,0.02) 100%);
          border: 1px solid rgba(255,255,255,0.07);
          border-radius: 16px;
          transition: border-color 0.2s ease, background 0.2s ease, transform 0.2s ease, box-shadow 0.2s ease;
        }
        .rec-card:hover {
          background: linear-gradient(145deg, rgba(245,158,11,0.08) 0%, rgba(217,119,6,0.04) 100%);
          border-color: rgba(245,158,11,0.35);
          transform: translateY(-3px);
          box-shadow: 0 12px 32px rgba(0,0,0,0.4), 0 0 0 1px rgba(245,158,11,0.12);
        }

        .rec-tooltip {
          background: #181b24;
          border: 1px solid rgba(255,255,255,0.12);
          box-shadow: 0 12px 32px rgba(0,0,0,0.5);
          animation: tooltipIn 0.15s ease;
        }
        @keyframes tooltipIn {
          from { opacity: 0; transform: translateY(-4px); }
          to   { opacity: 1; transform: translateY(0); }
        }

        .scroll-arrow {
          background: rgba(15,17,23,0.9);
          border: 1px solid rgba(255,255,255,0.10);
          backdrop-filter: blur(8px);
          transition: border-color 0.15s, opacity 0.15s;
        }
        .scroll-arrow:hover {
          border-color: rgba(245,158,11,0.4);
        }
      `}</style>

      <div className="max-w-4xl mx-auto px-6 py-8">

        {/* Header */}
        <div className="mb-10">
          <div className="flex items-center gap-3 mb-2">
            <div className="h-10 w-10 rounded-xl flex items-center justify-center" style={{ background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)', boxShadow: '0 4px 16px rgba(245,158,11,0.25)' }}>
              <BookOpen size={18} className="text-white" />
            </div>
            <div>
              <h1 className="library-heading text-2xl font-bold text-white">Learning Library</h1>
              <p className="text-xs text-zinc-500 -mt-0.5">Explore workforce knowledge across skill families</p>
            </div>
          </div>
        </div>

        {/* ── Recommended for you carousel ──────────────────────────────────── */}
        {hasRecommendations && !hasSearched && (
          <div className="mb-8 fade-in">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <Sparkles size={14} className="text-amber-500/80" />
                <h2 className="text-xs font-semibold uppercase tracking-widest text-[#6b7280]">
                  Recommended for you
                </h2>
              </div>

              {/* Scroll arrows */}
              {recommendations.length > 3 && (
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => scrollCarousel('left')}
                    disabled={!canScrollLeft}
                    className="scroll-arrow w-6 h-6 rounded-full flex items-center justify-center text-[#6b7280] disabled:opacity-30 disabled:cursor-not-allowed"
                    aria-label="Scroll left"
                  >
                    <ChevronLeft size={12} />
                  </button>
                  <button
                    type="button"
                    onClick={() => scrollCarousel('right')}
                    disabled={!canScrollRight}
                    className="scroll-arrow w-6 h-6 rounded-full flex items-center justify-center text-[#6b7280] disabled:opacity-30 disabled:cursor-not-allowed"
                    aria-label="Scroll right"
                  >
                    <ChevronRight size={12} />
                  </button>
                </div>
              )}
            </div>

            <div
              ref={carouselRef}
              className="rec-carousel"
              onScroll={checkScroll}
            >
              {recommendations.map((item, i) => (
                <RecommendationCard key={item.resource_id} item={item} index={i} />
              ))}
            </div>

            <p className="text-[10px] text-[#374151] mt-2.5">
              Powered by your job family and learning history. Hover a card for details.
            </p>
          </div>
        )}

        {/* Search bar */}
        <form onSubmit={handleSubmit(onSearch)} className="mb-4">
          <div className="flex gap-2">
            <div className="relative flex-1">
              <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[#6b7280] pointer-events-none">
                <Search size={16} />
              </span>

              <input
                {...register('q')}
                ref={(e) => {
                  register('q').ref(e)
                  ;(inputRef as React.MutableRefObject<HTMLInputElement | null>).current = e
                }}
                type="search"
                placeholder="Search resources, topics, skills..."
                className="search-input w-full pl-10 pr-10 py-3.5 rounded-xl text-sm bg-[#161922] border border-[rgba(255,255,255,0.08)] text-[#e2e4ea] placeholder-[#4b5563] focus:outline-none focus:border-amber-500/40 focus:ring-2 focus:ring-amber-500/15 transition-all"
              />

              {watchedQ && (
                <button
                  type="button"
                  onClick={clearSearch}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-[#6b7280] hover:text-[#e2e4ea] transition-colors"
                >
                  <X size={14} />
                </button>
              )}
            </div>

            <button
              type="submit"
              disabled={isFetching}
              className="px-5 py-3 bg-amber-500 hover:bg-amber-400 disabled:opacity-60 text-[#0f1117] font-semibold text-sm rounded-lg transition-colors"
            >
              {isFetching ? 'Searching...' : 'Search'}
            </button>

            <button
              type="button"
              onClick={() => setShowFilters(!showFilters)}
              className={`relative px-3.5 py-3 rounded-lg border text-sm transition-colors ${
                showFilters
                  ? 'bg-amber-500/15 border-amber-500/40 text-amber-400'
                  : 'bg-[#181b24] border-[rgba(255,255,255,0.10)] text-[#9ca3af] hover:text-[#e2e4ea]'
              }`}
              aria-label="Toggle filters"
            >
              <SlidersHorizontal size={16} />
              {activeFilterCount > 0 && (
                <span className="absolute -top-1.5 -right-1.5 w-4 h-4 bg-amber-500 text-[#0f1117] text-[9px] font-bold rounded-full flex items-center justify-center">
                  {activeFilterCount}
                </span>
              )}
            </button>
          </div>

          {/* Expanded filters panel */}
          {showFilters && (
            <div className="mt-2 p-4 rounded-lg bg-[#181b24] border border-[rgba(255,255,255,0.08)] fade-in">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                {/* Category */}
                <div>
                  <label className="block text-xs font-medium text-[#9ca3af] mb-1.5 uppercase tracking-wide">
                    Category
                  </label>
                  <div className="relative">
                    <select
                      {...register('category')}
                      className="w-full appearance-none bg-[#0f1117] border border-[rgba(255,255,255,0.10)] text-[#e2e4ea] text-sm rounded-md px-3 py-2 pr-8 focus:outline-none focus:border-amber-500/50 transition-colors"
                    >
                      {CATEGORIES.map(c => (
                        <option key={c.value} value={c.value}>{c.label}</option>
                      ))}
                    </select>
                    <ChevronDown size={13} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[#6b7280] pointer-events-none" />
                  </div>
                </div>

                {/* Content type */}
                <div>
                  <label className="block text-xs font-medium text-[#9ca3af] mb-1.5 uppercase tracking-wide">
                    Content type
                  </label>
                  <div className="relative">
                    <select
                      {...register('content_type')}
                      className="w-full appearance-none bg-[#0f1117] border border-[rgba(255,255,255,0.10)] text-[#e2e4ea] text-sm rounded-md px-3 py-2 pr-8 focus:outline-none focus:border-amber-500/50 transition-colors"
                    >
                      {CONTENT_TYPES.map(ct => (
                        <option key={ct.value} value={ct.value}>{ct.label}</option>
                      ))}
                    </select>
                    <ChevronDown size={13} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[#6b7280] pointer-events-none" />
                  </div>
                </div>

                {/* Tags (comma-separated) */}
                <div>
                  <label className="block text-xs font-medium text-[#9ca3af] mb-1.5 uppercase tracking-wide">
                    Tags
                  </label>
                  <input
                    type="text"
                    {...register('tags')}
                    placeholder="e.g. python, security"
                    className="w-full bg-[#0f1117] border border-[rgba(255,255,255,0.10)] text-[#e2e4ea] text-sm rounded-md px-3 py-2 focus:outline-none focus:border-amber-500/50 transition-colors"
                  />
                  <p className="text-[10px] text-[#6b7280] mt-1">Comma-separated; all listed tags must match.</p>
                </div>

                {/* Publish-date range */}
                <div>
                  <label className="block text-xs font-medium text-[#9ca3af] mb-1.5 uppercase tracking-wide">
                    Publish date
                  </label>
                  <div className="flex items-center gap-2">
                    <input
                      type="date"
                      {...register('from_date')}
                      aria-label="From date"
                      className="flex-1 bg-[#0f1117] border border-[rgba(255,255,255,0.10)] text-[#e2e4ea] text-sm rounded-md px-3 py-2 focus:outline-none focus:border-amber-500/50 transition-colors"
                    />
                    <span className="text-xs text-[#6b7280]">→</span>
                    <input
                      type="date"
                      {...register('to_date')}
                      aria-label="To date"
                      className="flex-1 bg-[#0f1117] border border-[rgba(255,255,255,0.10)] text-[#e2e4ea] text-sm rounded-md px-3 py-2 focus:outline-none focus:border-amber-500/50 transition-colors"
                    />
                  </div>
                </div>

                {/* Synonym toggle */}
                <div className="flex items-center justify-between">
                  <div>
                    <span className="text-sm text-[#d1d5db]">Synonym expansion</span>
                    <p className="text-xs text-[#6b7280] mt-0.5">Include related term variants</p>
                  </div>
                  <input type="checkbox" {...register('synonyms')} className="checkbox-toggle" />
                </div>

                {/* Fuzzy toggle */}
                <div className="flex items-center justify-between">
                  <div>
                    <span className="text-sm text-[#d1d5db]">Fuzzy matching</span>
                    <p className="text-xs text-[#6b7280] mt-0.5">Tolerate typos and near-matches</p>
                  </div>
                  <input type="checkbox" {...register('fuzzy')} className="checkbox-toggle" />
                </div>

                {/* Pinyin toggle */}
                <div className="flex items-center justify-between">
                  <div>
                    <span className="text-sm text-[#d1d5db]">Pinyin expansion</span>
                    <p className="text-xs text-[#6b7280] mt-0.5">Match CJK titles via pinyin (e.g. &quot;anquan&quot; → 安全)</p>
                  </div>
                  <input type="checkbox" {...register('pinyin')} className="checkbox-toggle" />
                </div>
              </div>
            </div>
          )}
        </form>

        {/* Sort tabs (shown once we have results or have searched) */}
        {(hasSearched || (data && data.total > 0)) && !isLoading && (
          <div className="flex items-center gap-1 mb-5 border-b border-[rgba(255,255,255,0.07)] pb-0 fade-in">
            {[
              { value: 'relevance', label: 'Relevance', icon: <Sparkles size={12} /> },
              { value: 'popular',   label: 'Popular',   icon: <TrendingUp size={12} /> },
              { value: 'recent',    label: 'Recent',    icon: <Clock size={12} /> },
            ].map(s => {
              const isActive = (watch('sort') || 'relevance') === s.value
              return (
                <button
                  key={s.value}
                  type="button"
                  onClick={() => {
                    setValue('sort', s.value)
                    if (hasSearched) handleSubmit(onSearch)()
                  }}
                  className={`sort-btn flex items-center gap-1.5 px-3 py-2.5 text-xs font-medium transition-colors border-b-2 -mb-px ${
                    isActive
                      ? 'active text-amber-400 border-amber-500'
                      : 'text-[#6b7280] border-transparent hover:text-[#9ca3af]'
                  }`}
                >
                  {s.icon}
                  {s.label}
                </button>
              )
            })}

            {/* Results count */}
            <span className="ml-auto text-xs text-[#4b5563] tabular-nums">
              {total} result{total !== 1 ? 's' : ''}
            </span>
          </div>
        )}

        {/* Loading skeleton */}
        {(isLoading || (isFetching && hasSearched)) && (
          <div className="space-y-3">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="rounded-xl p-4 bg-[rgba(255,255,255,0.03)] border border-[rgba(255,255,255,0.06)]">
                <div className="flex gap-3">
                  <div className="flex-1 space-y-2">
                    <div className="skeleton h-4 w-2/3" />
                    <div className="skeleton h-3 w-full" />
                    <div className="skeleton h-3 w-4/5" />
                    <div className="flex gap-2 mt-2">
                      <div className="skeleton h-5 w-14 rounded-full" />
                      <div className="skeleton h-5 w-20 rounded-full" />
                    </div>
                  </div>
                  <div className="space-y-1 text-right">
                    <div className="skeleton h-3 w-16" />
                    <div className="skeleton h-3 w-12" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Error state */}
        {isError && !isLoading && (
          <div className="rounded-xl p-5 bg-rose-950/30 border border-rose-800/40 text-rose-300 text-sm fade-in">
            Search failed. Please try again.
          </div>
        )}

        {/* Results */}
        {!isLoading && !isFetching && data && (
          <div className="fade-in">
            {/* Synonym expansion notice */}
            {data.expanded_synonyms && data.expanded_synonyms.length > 0 && (
              <p className="text-xs text-[#6b7280] mb-3 flex items-center gap-1.5">
                <Sparkles size={11} className="text-amber-500/70" />
                Also searched: <span className="text-[#9ca3af]">{data.expanded_synonyms.join(', ')}</span>
              </p>
            )}

            {/* Empty state */}
            {results.length === 0 && hasSearched && (
              <div className="py-16 text-center">
                <div className="w-12 h-12 rounded-full bg-[rgba(255,255,255,0.04)] border border-[rgba(255,255,255,0.08)] flex items-center justify-center mx-auto mb-4">
                  <Search size={20} className="text-[#4b5563]" />
                </div>
                <p className="text-[#6b7280] text-sm">No resources found.</p>
                <p className="text-[#4b5563] text-xs mt-1">Try a different search term or remove some filters.</p>
                <button
                  type="button"
                  onClick={clearSearch}
                  className="mt-4 text-xs text-amber-500 hover:text-amber-400 transition-colors underline underline-offset-2"
                >
                  Clear search
                </button>
              </div>
            )}

            {/* Default empty (no search yet, no results) */}
            {results.length === 0 && !hasSearched && !hasRecommendations && (
              <div className="py-16 text-center">
                <div className="w-12 h-12 rounded-full bg-amber-500/10 border border-amber-500/20 flex items-center justify-center mx-auto mb-4">
                  <BookOpen size={20} className="text-amber-500/70" />
                </div>
                <p className="text-[#6b7280] text-sm">Enter a search term to discover resources.</p>
                <p className="text-[#4b5563] text-xs mt-1">Articles, videos, courses, and documents available.</p>
              </div>
            )}

            {/* Default prompt when no search but recommendations are showing */}
            {results.length === 0 && !hasSearched && hasRecommendations && (
              <div className="py-10 text-center">
                <p className="text-[#4b5563] text-sm">Enter a search term to find more resources.</p>
              </div>
            )}

            {/* Result list */}
            {results.length > 0 && (
              <ul className="space-y-2.5">
                {results.map((r) => (
                  <li
                    key={r.id}
                    className="result-card rounded-xl p-4 cursor-pointer"
                    onClick={() => {
                      // Fire-and-forget behavior event for resource view
                      api.post('/recommendations/events', {
                        resource_id: r.id,
                        event_type: 'view',
                      }).catch(() => {})
                    }}
                  >
                    <div className="flex items-start gap-3">
                      {/* Content type icon badge */}
                      <div className={`mt-0.5 flex-shrink-0 w-7 h-7 rounded-lg flex items-center justify-center border ${CONTENT_TYPE_COLORS[r.content_type] ?? 'bg-[rgba(255,255,255,0.06)] text-[#9ca3af] border-[rgba(255,255,255,0.10)]'}`}>
                        {CONTENT_TYPE_ICONS[r.content_type] ?? <FileText size={13} />}
                      </div>

                      <div className="flex-1 min-w-0">
                        <h2 className="font-medium text-[#f0f1f4] text-sm leading-snug truncate">
                          {r.title}
                        </h2>

                        {r.description && (
                          <p className="mt-1 text-xs text-[#6b7280] leading-relaxed line-clamp-2">
                            {r.description}
                          </p>
                        )}

                        {/* Meta tags row */}
                        <div className="mt-2 flex flex-wrap items-center gap-1.5">
                          {/* Category pill */}
                          <span className="filter-pill text-xs px-2 py-0.5 rounded-full text-[#9ca3af]">
                            {r.category.replace('_', ' ')}
                          </span>

                          {/* Skill tags */}
                          {r.tags?.slice(0, 4).map(t => (
                            <span key={t} className="tag-chip">
                              <Tag size={9} />
                              {t}
                            </span>
                          ))}
                          {r.tags?.length > 4 && (
                            <span className="text-[10px] text-[#4b5563]">+{r.tags.length - 4}</span>
                          )}

                          {/* Synonym match note */}
                          {r.matched_synonyms && r.matched_synonyms.length > 0 && (
                            <span className="text-[10px] text-amber-500/80 flex items-center gap-1 ml-0.5">
                              <Sparkles size={9} />
                              via {r.matched_synonyms.join(', ')}
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Right-side meta */}
                      <div className="flex-shrink-0 text-right space-y-1">
                        <div className="flex items-center gap-1 text-[10px] text-[#4b5563] justify-end">
                          <Eye size={10} />
                          <span className="tabular-nums">{r.view_count.toLocaleString()}</span>
                        </div>
                        {r.publish_date && (
                          <div className="flex items-center gap-1 text-[10px] text-[#4b5563] justify-end">
                            <Calendar size={10} />
                            <span>{r.publish_date}</span>
                          </div>
                        )}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
