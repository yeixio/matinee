import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clock, Play, Plus, Search as SearchIcon, Star, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type DiscoverTitle, type User } from '../api'
import { RemotePoster, useRequestOptions } from '../components/Arr'
import { Row, RowItem } from '../components/Row'
import { Button, EmptyState, Spinner } from '../components/ui'

const statusLabel: Record<NonNullable<DiscoverTitle['status']>, string> = {
  available: 'In your library',
  added: 'On its way',
  requested: 'Requested',
}

// Discover: new movies and shows from Overseerr, starting with ones like
// what you watched lately, and a way to ask for them. Only there when an
// admin connected Overseerr.
export default function Discover() {
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data: opts } = useRequestOptions()
  const [params, setParams] = useSearchParams()
  const [text, setText] = useState(params.get('q') ?? '')
  const [q, setQ] = useState(text.trim())
  const [open, setOpen] = useState<DiscoverTitle>()

  // Searching goes out to the internet through Overseerr, so wait for a pause in typing.
  useEffect(() => {
    const t = window.setTimeout(() => {
      setQ(text.trim())
      setParams(text.trim() ? { q: text.trim() } : {}, { replace: true })
    }, 400)
    return () => window.clearTimeout(t)
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
      <EmptyState title="Nothing to discover yet">
        {me?.isAdmin ? (
          <>
            <p>Connect Overseerr to see what's trending, get recommendations, and ask for new movies and shows.</p>
            <Link to="/settings">
              <Button variant="primary" className="mt-6">
                Open settings
              </Button>
            </Link>
          </>
        ) : (
          <p>An admin hasn't connected Overseerr to this server.</p>
        )}
      </EmptyState>
    )
  }

  const canRequest = !!rows.data?.canRequest
  return (
    <div className="space-y-10">
      <div className="space-y-4">
        <h1 className="text-2xl font-bold tracking-tight">Discover</h1>
        <div className="relative max-w-2xl">
          <SearchIcon className="absolute top-1/2 left-4 size-5 -translate-y-1/2 text-faint" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Find any movie or show…"
            className="h-12 w-full rounded-xl border border-line bg-surface pr-4 pl-12 placeholder:text-faint focus:border-accent focus:outline-none"
          />
          {found.isFetching && <div className="absolute top-1/2 right-4 size-2 -translate-y-1/2 animate-pulse rounded-full bg-accent" />}
        </div>
      </div>

      {q.length > 1 ? (
        found.error ? (
          <p className="text-sm text-danger">{found.error.message}</p>
        ) : found.data && found.data.length === 0 ? (
          <EmptyState title={`Nothing matches "${q}"`} />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(8rem,1fr))] gap-4 md:grid-cols-[repeat(auto-fill,minmax(10rem,1fr))]">
            {found.data?.map((t) => <TitleCard key={t.kind + t.tmdbId} title={t} onOpen={setOpen} />)}
          </div>
        )
      ) : rows.error ? (
        <EmptyState title="Discover isn't available right now">
          <p>{rows.error.message}</p>
        </EmptyState>
      ) : !rows.data ? (
        <div className="grid h-[40vh] place-items-center">
          <Spinner />
        </div>
      ) : (
        rows.data.rows.map((r) => (
          <Row key={r.id} title={r.title}>
            {r.items.map((t) => (
              <RowItem key={t.kind + t.tmdbId}>
                <TitleCard title={t} onOpen={setOpen} />
              </RowItem>
            ))}
          </Row>
        ))
      )}

      {open && <TitleSheet title={open} canRequest={canRequest} onClose={() => setOpen(undefined)} onChange={setOpen} />}
    </div>
  )
}

function StatusBadge({ status }: { status: NonNullable<DiscoverTitle['status']> }) {
  const Icon = status === 'available' ? Check : Clock
  return (
    <span className="absolute top-2 right-2 flex items-center gap-1 rounded-full bg-ink/80 px-2 py-0.5 text-[11px] font-medium text-text backdrop-blur">
      <Icon className="size-3 text-accent" /> {statusLabel[status]}
    </span>
  )
}

function TitleCard({ title: t, onOpen }: { title: DiscoverTitle; onOpen: (t: DiscoverTitle) => void }) {
  return (
    <button onClick={() => onOpen(t)} className="group block w-full animate-fade text-left">
      <div className="relative">
        <RemotePoster src={t.poster} title={t.title} className="rounded-xl ring-1 ring-text/5 transition group-hover:ring-accent/70" />
        {t.status && <StatusBadge status={t.status} />}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium">{t.title}</div>
        <div className="truncate text-xs text-muted">
          {[t.year, t.kind === 'show' ? 'Show' : 'Movie'].filter(Boolean).join(' · ')}
        </div>
      </div>
    </button>
  )
}

// A title's overview, and what can be done with it: watch it when it's in
// a library, or ask for it.
function TitleSheet({
  title: t,
  canRequest,
  onClose,
  onChange,
}: {
  title: DiscoverTitle
  canRequest: boolean
  onClose: () => void
  onChange: (t: DiscoverTitle) => void
}) {
  const qc = useQueryClient()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const ask = useMutation({
    mutationFn: () => api.discoverRequest(t),
    onSuccess: (v) => {
      onChange(v)
      qc.invalidateQueries({ queryKey: ['discover'] })
      qc.invalidateQueries({ queryKey: ['discoverSearch'] })
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t.title}
        onClick={(e) => e.stopPropagation()}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-t-2xl border border-line/60 bg-surface shadow-2xl sm:rounded-2xl"
      >
        <div className="relative">
          {t.backdrop ? (
            <img src={t.backdrop} alt="" referrerPolicy="no-referrer" className="aspect-video w-full object-cover" />
          ) : (
            <div className="h-16" />
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-surface via-surface/30 to-transparent" />
          <button
            onClick={onClose}
            aria-label="Close"
            className="absolute top-3 right-3 rounded-full bg-ink/70 p-1.5 text-muted backdrop-blur hover:text-text"
          >
            <X className="size-5" />
          </button>
        </div>
        <div className="relative -mt-16 flex gap-4 px-5 pb-6 md:px-6">
          <RemotePoster src={t.poster} title={t.title} label={false} className="w-24 shrink-0 self-start rounded-lg shadow-lg ring-1 ring-text/10 md:w-32" />
          <div className="min-w-0 flex-1 space-y-3 pt-12">
            <div>
              <h2 className="font-display text-2xl font-semibold tracking-tight">{t.title}</h2>
              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
                {t.year && <span>{t.year}</span>}
                <span>{t.kind === 'show' ? 'Show' : 'Movie'}</span>
                {!!t.rating && (
                  <span className="flex items-center gap-1">
                    <Star className="size-3.5 fill-accent text-accent" /> {t.rating.toFixed(1)}
                  </span>
                )}
                {t.status && <span className="text-accent">{statusLabel[t.status]}</span>}
              </div>
            </div>
            {t.overview && <p className="text-sm leading-relaxed text-muted">{t.overview}</p>}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              {t.status === 'available' && t.itemId ? (
                <Link to={`/item/${t.itemId}`}>
                  <Button variant="primary" icon={<Play className="size-4" />}>
                    Open
                  </Button>
                </Link>
              ) : !t.status && canRequest ? (
                <Button variant="primary" icon={<Plus className="size-4" />} disabled={ask.isPending} onClick={() => ask.mutate()}>
                  {ask.isPending ? 'Asking…' : t.kind === 'show' ? 'Request the show' : 'Request'}
                </Button>
              ) : t.status === 'requested' ? (
                <span className="text-sm text-muted">Waiting for an admin to approve it.</span>
              ) : t.status === 'added' ? (
                <span className="text-sm text-muted">It'll show up in your library once it's downloaded.</span>
              ) : null}
            </div>
            {ask.error && <p className="text-sm text-danger">{ask.error.message}</p>}
          </div>
        </div>
      </div>
    </div>
  )
}
