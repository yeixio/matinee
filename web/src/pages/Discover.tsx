import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clock, Play, Plus, Search as SearchIcon, Star, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, type DiscoverRow, type DiscoverTitle, type User } from '../api'
import { RemotePoster, useRequestOptions } from '../components/Arr'
import { Row, RowItem } from '../components/Row'
import { Button, EmptyState, Spinner } from '../components/ui'
import i18n from '../i18n'

// A row's name in the page's language: the lists by their id, and
// "Because you watched …" with the title.
function discoverRowTitle(r: DiscoverRow): string {
  if (r.because) return i18n.t('common:discover.because', { title: r.because })
  return i18n.exists(`common:discover.rows.${r.id}`) ? i18n.t(`common:discover.rows.${r.id}`) : r.title
}

// Discover: new movies and shows from Overseerr, starting with ones like
// what you watched lately, and a way to ask for them. Only there when an
// admin connected Overseerr.
export default function Discover() {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data: opts } = useRequestOptions()
  const [params, setParams] = useSearchParams()
  const [text, setText] = useState(params.get('q') ?? '')
  const [q, setQ] = useState(text.trim())
  const [open, setOpen] = useState<DiscoverTitle>()

  // Searching goes out to the internet through Overseerr, so wait for a pause in typing.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(text.trim())
      setParams(text.trim() ? { q: text.trim() } : {}, { replace: true })
    }, 400)
    return () => window.clearTimeout(timer)
  }, [text, setParams])

  const enabled = !!opts?.discover
  const rows = useQuery({ queryKey: ['discover'], queryFn: api.discover, enabled, staleTime: 10 * 60_000 })
  const found = useQuery({
    queryKey: ['discoverSearch', q],
    queryFn: () => api.discoverSearch(q),
    enabled: enabled && q.length > 1,
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  })

  if (opts && !opts.discover) {
    return (
      <EmptyState title={t('discover.emptyTitle')}>
        {me?.isAdmin ? (
          <>
            <p>{t('discover.connectIt')}</p>
            <Link to="/settings">
              <Button variant="primary" className="mt-6">
                {t('discover.openSettings')}
              </Button>
            </Link>
          </>
        ) : (
          <p>{t('discover.notConnected')}</p>
        )}
      </EmptyState>
    )
  }

  const canRequest = !!rows.data?.canRequest
  return (
    <div className="space-y-10">
      <div className="space-y-4">
        <h1 className="text-2xl font-bold tracking-tight">{t('discover.title')}</h1>
        <div className="relative max-w-2xl">
          <SearchIcon className="absolute top-1/2 start-4 size-5 -translate-y-1/2 text-faint" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={t('discover.placeholder')}
            className="h-12 w-full rounded-xl border border-line bg-surface pe-4 ps-12 placeholder:text-faint focus:border-accent focus:outline-none"
          />
          {found.isFetching && <div className="absolute top-1/2 end-4 size-2 -translate-y-1/2 animate-pulse rounded-full bg-accent" />}
        </div>
      </div>

      {q.length > 1 ? (
        found.error ? (
          <p className="text-sm text-danger">{found.error.message}</p>
        ) : found.data && found.data.length === 0 ? (
          <EmptyState title={t('search.nothing', { q })} />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(8rem,1fr))] gap-4 md:grid-cols-[repeat(auto-fill,minmax(10rem,1fr))]">
            {found.data?.map((d) => <TitleCard key={d.kind + d.tmdbId} title={d} onOpen={setOpen} />)}
          </div>
        )
      ) : rows.error ? (
        <EmptyState title={t('discover.unavailable')}>
          <p>{rows.error.message}</p>
        </EmptyState>
      ) : !rows.data ? (
        <div className="grid h-[40vh] place-items-center">
          <Spinner />
        </div>
      ) : (
        rows.data.rows.map((r) => (
          <Row key={r.id} title={discoverRowTitle(r)} more={r.more ? discoverRowLink(r.id) : undefined} hasMore={r.more}>
            {r.items.map((d) => (
              <RowItem key={d.kind + d.tmdbId}>
                <TitleCard title={d} onOpen={setOpen} />
              </RowItem>
            ))}
          </Row>
        ))
      )}

      {open && <TitleSheet title={open} canRequest={canRequest} onClose={() => setOpen(undefined)} onChange={setOpen} />}
    </div>
  )
}

const discoverRowLink = (id: string) => `/discover/row/${encodeURIComponent(id)}`

// All of one of Discover's rows, a page from Overseerr at a time as you
// scroll, for the See more at the end of the row.
export function DiscoverRowPage() {
  const { t } = useTranslation()
  const id = useParams().id ?? ''
  const [open, setOpen] = useState<DiscoverTitle>()
  const pages = useInfiniteQuery({
    queryKey: ['discoverRow', id],
    queryFn: ({ pageParam }) => api.discoverRow(id, pageParam),
    initialPageParam: 1,
    getNextPageParam: (last) => (last.page < last.pages ? last.page + 1 : undefined),
    staleTime: 10 * 60_000,
  })
  const end = useRef<HTMLDivElement>(null)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = pages
  // The next page loads as the end of the grid comes into view.
  useEffect(() => {
    const el = end.current
    if (!el || !hasNextPage) return
    const seen = new IntersectionObserver((e) => e[0].isIntersecting && !isFetchingNextPage && fetchNextPage(), { rootMargin: '800px' })
    seen.observe(el)
    return () => seen.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  if (pages.error) {
    return (
      <EmptyState title={t('discover.unavailable')}>
        <p>{pages.error.message}</p>
      </EmptyState>
    )
  }
  if (!pages.data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  const canRequest = pages.data.pages[0].canRequest
  // A title can move up a list between pages; it shows once.
  const seen = new Set<string>()
  const titles = pages.data.pages
    .flatMap((p) => p.row.items)
    .filter((d) => {
      const key = d.kind + d.tmdbId
      return !seen.has(key) && !!seen.add(key)
    })
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{discoverRowTitle(pages.data.pages[0].row)}</h1>
      {titles.length > 0 ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(8rem,1fr))] gap-4 md:grid-cols-[repeat(auto-fill,minmax(10rem,1fr))]">
          {titles.map((d) => (
            <TitleCard key={d.kind + d.tmdbId} title={d} onOpen={setOpen} />
          ))}
        </div>
      ) : (
        !hasNextPage && <EmptyState title={t('common:rows.empty')} />
      )}
      <div ref={end} className="grid h-16 place-items-center">
        {hasNextPage && (isFetchingNextPage ? <Spinner /> : <Button onClick={() => fetchNextPage()}>{t('common:rows.loadMore')}</Button>)}
      </div>
      {open && <TitleSheet title={open} canRequest={canRequest} onClose={() => setOpen(undefined)} onChange={setOpen} />}
    </div>
  )
}

function StatusBadge({ status }: { status: NonNullable<DiscoverTitle['status']> }) {
  const { t } = useTranslation()
  const Icon = status === 'available' ? Check : Clock
  return (
    <span className="absolute top-2 end-2 flex items-center gap-1 rounded-full bg-ink/80 px-2 py-0.5 text-[11px] font-medium text-text backdrop-blur">
      <Icon className="size-3 text-accent" /> {t(`common:discover.status.${status}`)}
    </span>
  )
}

function TitleCard({ title: d, onOpen }: { title: DiscoverTitle; onOpen: (d: DiscoverTitle) => void }) {
  const { t } = useTranslation()
  return (
    <button onClick={() => onOpen(d)} className="group block w-full animate-fade text-start">
      <div className="relative">
        <RemotePoster src={d.poster} title={d.title} className="rounded-xl ring-1 ring-text/5 transition group-hover:ring-accent/70" />
        {d.status && <StatusBadge status={d.status} />}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium">{d.title}</div>
        <div className="truncate text-xs text-muted">
          {[d.year, t(`common:discover.kind.${d.kind}`)].filter(Boolean).join(' · ')}
        </div>
      </div>
    </button>
  )
}

// A title's overview, and what can be done with it: watch it when it's in
// a library, or ask for it.
function TitleSheet({
  title: d,
  canRequest,
  onClose,
  onChange,
}: {
  title: DiscoverTitle
  canRequest: boolean
  onClose: () => void
  onChange: (t: DiscoverTitle) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const ask = useMutation({
    mutationFn: () => api.discoverRequest(d),
    onSuccess: (v) => {
      onChange(v)
      qc.invalidateQueries({ queryKey: ['discover'] })
      qc.invalidateQueries({ queryKey: ['discoverSearch'] })
      qc.invalidateQueries({ queryKey: ['discoverRow'] })
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={d.title}
        onClick={(e) => e.stopPropagation()}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-t-2xl border border-line/60 bg-surface shadow-2xl sm:rounded-2xl"
      >
        <div className="relative">
          {d.backdrop ? (
            <img src={d.backdrop} alt="" referrerPolicy="no-referrer" className="aspect-video w-full object-cover" />
          ) : (
            <div className="h-16" />
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-surface via-surface/30 to-transparent" />
          <button
            onClick={onClose}
            aria-label={t('close')}
            className="absolute top-3 end-3 rounded-full bg-ink/70 p-1.5 text-muted backdrop-blur hover:text-text"
          >
            <X className="size-5" />
          </button>
        </div>
        <div className="relative -mt-16 flex gap-4 px-5 pb-6 md:px-6">
          <RemotePoster src={d.poster} title={d.title} label={false} className="w-24 shrink-0 self-start rounded-lg shadow-lg ring-1 ring-text/10 md:w-32" />
          <div className="min-w-0 flex-1 space-y-3 pt-12">
            <div>
              <h2 className="font-display text-2xl font-semibold tracking-tight">{d.title}</h2>
              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
                {d.year && <span>{d.year}</span>}
                <span>{t(`common:discover.kind.${d.kind}`)}</span>
                {!!d.rating && (
                  <span className="flex items-center gap-1">
                    <Star className="size-3.5 fill-accent text-accent" /> {d.rating.toFixed(1)}
                  </span>
                )}
                {d.status && <span className="text-accent">{t(`common:discover.status.${d.status}`)}</span>}
              </div>
            </div>
            {d.overview && <p className="text-sm leading-relaxed text-muted">{d.overview}</p>}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              {d.status === 'available' && d.itemId ? (
                <Link to={`/item/${d.itemId}`}>
                  <Button variant="primary" icon={<Play className="size-4" />}>
                    {t('discover.open')}
                  </Button>
                </Link>
              ) : !d.status && canRequest ? (
                <Button variant="primary" icon={<Plus className="size-4" />} disabled={ask.isPending} onClick={() => ask.mutate()}>
                  {ask.isPending ? t('discover.asking') : d.kind === 'show' ? t('discover.requestShow') : t('discover.request')}
                </Button>
              ) : d.status === 'requested' ? (
                <span className="text-sm text-muted">{t('discover.waiting')}</span>
              ) : d.status === 'added' ? (
                <span className="text-sm text-muted">{t('discover.onItsWay')}</span>
              ) : null}
            </div>
            {ask.error && <p className="text-sm text-danger">{ask.error.message}</p>}
          </div>
        </div>
      </div>
    </div>
  )
}
