import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Download, Link2, Plus, Search, Upload } from 'lucide-react'
import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, opmlUrl, type DirectoryShow, type Item, type Library, type OpmlImport, type User } from '../api'
import { Artwork } from '../components/Artwork'
import { GridSkeleton } from '../components/Card'
import { EpisodeList } from '../components/Episodes'
import { LibraryIcon } from '../components/Shell'
import { ViewTabs } from '../components/ViewTabs'
import { Button, EmptyState, Spinner } from '../components/ui'
import { BookGrid } from './Books'

type View = 'shows' | 'latest' | 'add'
const views: View[] = ['shows', 'latest', 'add']

const inputClass = 'h-10 w-full rounded-lg border border-line bg-surface px-3 text-sm focus:border-accent focus:outline-none'

// A podcast library: the shows, the latest episodes, and finding more
// shows to add, in Podcast Index or by their feed's address.
export default function Podcasts({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const view = (views.includes(params.get('view') as View) ? params.get('view') : 'shows') as View
  const pick = (v: View) => setParams(v === 'shows' ? {} : { view: v }, { replace: true })
  const me = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  // Kids profiles listen; grown-ups add shows.
  const canAdd = !!me.data && !me.data.kidsLevel
  const shown = canAdd ? views : views.filter((v) => v !== 'add')

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <LibraryIcon kind={lib.kind} className="size-6 text-accent" />
        <h1 className="text-2xl font-bold tracking-tight">{lib.name}</h1>
        {lib.scanning && <span className="animate-pulse text-sm text-accent">{t('common:podcasts.checking')}</span>}
      </div>
      <ViewTabs views={shown} view={view} label={(v) => t(`common:podcasts.${v}`)} onPick={pick} />
      {view === 'latest' ? <Latest lib={lib} /> : view === 'add' && canAdd ? <AddShows lib={lib} me={me.data!} /> : <Shows lib={lib} onAdd={canAdd ? () => pick('add') : undefined} />}
    </div>
  )
}

function Shows({ lib, onAdd }: { lib: Library; onAdd?: () => void }) {
  const { t } = useTranslation()
  const shows = useQuery({
    queryKey: ['podcasts', lib.id, 'shows'],
    queryFn: () => api.libraryItems(lib.id, { kind: ['podcast'], sort: 'title' }),
    placeholderData: keepPreviousData,
  })
  const following = useQuery({ queryKey: ['podcasts', 'following'], queryFn: api.followedPodcasts })
  if (shows.isPending) return <GridSkeleton />
  if (!shows.data?.length) {
    return (
      <EmptyState title={t('common:podcasts.noShows')}>
        {onAdd ? (
          <Button variant="primary" className="mt-2" icon={<Plus className="size-4" />} onClick={onAdd}>
            {t('common:podcasts.add')}
          </Button>
        ) : null}
      </EmptyState>
    )
  }
  const followed = new Map(following.data?.filter((f) => f.libraryId === lib.id).map((f) => [f.id, f.unplayed]))
  const mine = shows.data.filter((s) => followed.has(s.id))
  const others = shows.data.filter((s) => !followed.has(s.id))
  return (
    <div className="space-y-10">
      {mine.length > 0 && (
        <section className="space-y-4">
          <h2 className="text-lg font-semibold">{t('common:podcasts.following')}</h2>
          <BookGrid>
            {mine.map((s) => (
              <ShowCard key={s.id} show={s} unplayed={followed.get(s.id)} />
            ))}
          </BookGrid>
        </section>
      )}
      {others.length > 0 && (
        <section className="space-y-4">
          {mine.length > 0 && <h2 className="text-lg font-semibold">{t('common:podcasts.moreShows')}</h2>}
          <BookGrid>
            {others.map((s) => (
              <ShowCard key={s.id} show={s} />
            ))}
          </BookGrid>
        </section>
      )}
    </div>
  )
}

// A show's artwork and name, with how many episodes wait for the person.
export function ShowCard({ show, unplayed }: { show: Item; unplayed?: number }) {
  const { t } = useTranslation()
  return (
    <Link to={`/item/${show.id}`} className="group block animate-fade">
      <div className="relative">
        <Artwork item={show} type="poster" className="aspect-square rounded-lg shadow-md ring-1 ring-text/5 transition duration-200 group-hover:brightness-110" />
        {!!unplayed && (
          <span className="absolute end-2 top-2 min-w-6 rounded-full bg-accent px-1.5 py-0.5 text-center text-xs font-semibold text-on-accent shadow" title={t('common:podcasts.newEpisodesCount', { count: unplayed })}>
            {unplayed > 99 ? '99+' : unplayed}
          </span>
        )}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium group-hover:text-accent-strong">{show.title}</div>
        <div className="truncate text-xs text-muted">{show.author || ' '}</div>
      </div>
    </Link>
  )
}

// The newest episodes of every show in the library.
function Latest({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const episodes = useQuery({
    queryKey: ['podcasts', lib.id, 'latest'],
    queryFn: () => api.libraryItems(lib.id, { kind: ['podcastEpisode'], sort: 'released', order: 'desc', limit: 100 }),
  })
  if (episodes.isPending) return <Spinner />
  if (!episodes.data?.length) return <EmptyState title={t('common:podcasts.noEpisodes')} />
  return <EpisodeList episodes={episodes.data} withShow />
}

// Finding shows: Podcast Index when the server has a key, a feed's address,
// or an OPML file from another podcast app.
function AddShows({ lib, me }: { lib: Library; me: User }) {
  const { t, i18n } = useTranslation()
  const [q, setQ] = useState('')
  const [term, setTerm] = useState('')
  const dir = useQuery({
    queryKey: ['podcasts', 'directory', term, i18n.language],
    queryFn: () => api.podcastDirectory(term ? { q: term } : { lang: i18n.language }),
    staleTime: 10 * 60_000,
    placeholderData: keepPreviousData,
  })
  const search = (e: FormEvent) => {
    e.preventDefault()
    setTerm(q.trim())
  }
  return (
    <div className="space-y-10">
      {dir.data?.connected === false ? (
        <div className="rounded-xl border border-line/60 bg-surface/50 p-5 text-sm text-muted">
          <p>{t('common:podcasts.noDirectory')}</p>
          {me.isAdmin && (
            <Link to="/settings#podcast-index" className="mt-2 inline-block font-medium text-accent hover:underline">
              {t('common:podcasts.setUpDirectory')}
            </Link>
          )}
        </div>
      ) : (
        <section className="space-y-4">
          <form onSubmit={search} className="flex gap-2">
            <label className="relative flex-1">
              <Search className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted" />
              <input
                className={`${inputClass} ps-9`}
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder={t('common:podcasts.searchPlaceholder')}
                aria-label={t('common:podcasts.search')}
              />
            </label>
            <Button variant="primary">{t('common:podcasts.search')}</Button>
          </form>
          <h2 className="text-lg font-semibold">{term ? t('common:podcasts.resultsFor', { term }) : t('common:podcasts.trending')}</h2>
          {dir.isPending ? (
            <GridSkeleton />
          ) : dir.error ? (
            <p className="text-sm text-danger">{dir.error.message}</p>
          ) : !dir.data?.shows.length ? (
            <p className="text-sm text-muted">{t('common:podcasts.nothingFound')}</p>
          ) : (
            <ul className="grid gap-3 md:grid-cols-2">
              {dir.data.shows.map((s) => (
                <DirectoryRow key={s.indexId} lib={lib} show={s} />
              ))}
            </ul>
          )}
          <p className="text-xs text-faint">{t('common:podcasts.poweredBy')}</p>
        </section>
      )}
      <AddByAddress lib={lib} />
      <MoveShows lib={lib} />
    </div>
  )
}

function useAddShow(lib: Library) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (feedUrl: string) => api.addPodcast(lib.id, feedUrl),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['podcasts'] })
      qc.invalidateQueries({ queryKey: ['home'] })
    },
  })
}

function DirectoryRow({ lib, show }: { lib: Library; show: DirectoryShow }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const add = useAddShow(lib)
  const have = show.itemId ?? add.data?.id
  return (
    <li className="flex gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
      {show.image ? (
        <img src={show.image} alt="" loading="lazy" referrerPolicy="no-referrer" className="size-20 shrink-0 rounded-lg bg-text/5 object-cover" />
      ) : (
        <div className="size-20 shrink-0 rounded-lg bg-text/5" />
      )}
      <div className="min-w-0 flex-1 space-y-1">
        <div className="truncate font-medium">{show.title}</div>
        <div className="truncate text-xs text-muted">{[show.author, ...(show.categories ?? []).slice(0, 2)].filter(Boolean).join(' · ')}</div>
        {show.description && <p className="line-clamp-2 text-xs text-muted">{show.description}</p>}
        {add.error && <p className="text-xs text-danger">{add.error.message}</p>}
      </div>
      <div className="shrink-0 self-center">
        {have ? (
          <Button variant="ghost" icon={<Check className="size-4" />} onClick={() => navigate(`/item/${have}`)}>
            {t('common:podcasts.open')}
          </Button>
        ) : (
          <Button icon={add.isPending ? <Spinner className="size-4" /> : <Plus className="size-4" />} disabled={add.isPending} onClick={() => add.mutate(show.feedUrl)}>
            {t('common:podcasts.addButton')}
          </Button>
        )}
      </div>
    </li>
  )
}

function AddByAddress({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [url, setUrl] = useState('')
  const add = useAddShow(lib)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (url.trim()) add.mutate(url.trim(), { onSuccess: (show) => navigate(`/item/${show.id}`) })
  }
  return (
    <section className="space-y-3">
      <h2 className="flex items-center gap-2 text-lg font-semibold">
        <Link2 className="size-5 text-accent" />
        {t('common:podcasts.byAddress')}
      </h2>
      <p className="text-sm text-muted">{t('common:podcasts.byAddressHint')}</p>
      <form onSubmit={submit} className="flex gap-2">
        <input className={inputClass} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/feed.xml" inputMode="url" aria-label={t('common:podcasts.feedAddress')} />
        <Button variant="primary" disabled={!url.trim() || add.isPending}>
          {add.isPending ? <Spinner className="size-4" /> : t('common:podcasts.addButton')}
        </Button>
      </form>
      {add.error && <p className="text-sm text-danger">{add.error.message}</p>}
    </section>
  )
}

// OPML moves shows between podcast apps: in from another app, or out.
function MoveShows({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const file = useRef<HTMLInputElement>(null)
  const [result, setResult] = useState<OpmlImport>()
  const imp = useMutation({
    mutationFn: (f: File) => api.importOpml(lib.id, f),
    onSuccess: (r) => {
      setResult(r)
      qc.invalidateQueries({ queryKey: ['podcasts'] })
    },
  })
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">{t('common:podcasts.fromAnotherApp')}</h2>
      <p className="text-sm text-muted">{t('common:podcasts.opmlHint')}</p>
      <div className="flex flex-wrap gap-2">
        <input
          ref={file}
          type="file"
          accept=".opml,.xml,text/xml,text/x-opml,application/xml"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) imp.mutate(f)
            e.target.value = ''
          }}
        />
        <Button icon={imp.isPending ? <Spinner className="size-4" /> : <Upload className="size-4" />} disabled={imp.isPending} onClick={() => file.current?.click()}>
          {imp.isPending ? t('common:podcasts.importing') : t('common:podcasts.import')}
        </Button>
        <a href={opmlUrl(lib.id)} download className="inline-flex h-10 items-center gap-2 rounded-lg px-4 text-sm text-muted transition-colors hover:bg-text/5 hover:text-text">
          <Download className="size-4" />
          {t('common:podcasts.export')}
        </a>
      </div>
      {imp.error && <p className="text-sm text-danger">{imp.error.message}</p>}
      {result && (
        <div className="space-y-1 text-sm">
          <p className="text-muted">{t('common:podcasts.imported', { count: result.added })}</p>
          {result.problems.map((p) => (
            <p key={p.feedUrl} className="text-xs text-danger">
              {p.title || p.feedUrl}: {p.error}
            </p>
          ))}
        </div>
      )}
    </section>
  )
}
