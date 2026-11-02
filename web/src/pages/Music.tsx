import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ChevronLeft, Play, Shuffle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, pendingRefetch, type Item, type Library, type RecordLabel } from '../api'
import { Artwork } from '../components/Artwork'
import { GridSkeleton } from '../components/Card'
import { PlaylistCard, usePlaylists } from '../components/Playlists'
import { PlaylistGrid } from './Playlist'
import { LibraryIcon } from '../components/Shell'
import { Row, RowItem } from '../components/Row'
import { ViewTabs } from '../components/ViewTabs'
import { TrackMenu, Tracks } from '../components/Tracks'
import { Button, EmptyState, Spinner } from '../components/ui'
import type { ItemFilter, SortField } from '../filter'
import { useAudio } from '../player/AudioPlayer'

type View = 'overview' | 'artists' | 'albums' | 'labels' | 'songs' | 'playlists'
const views: View[] = ['overview', 'playlists', 'artists', 'albums', 'labels', 'songs']

// How each view sorts, and the orders it offers.
const sorts: Record<Exclude<View, 'overview' | 'playlists' | 'labels'>, SortField[]> = {
  artists: ['title', 'added', 'lastPlayed'],
  albums: ['added', 'title', 'year', 'lastPlayed', 'myRating'],
  songs: ['title', 'added', 'lastPlayed', 'myRating'],
}
const kinds = { artists: 'artist', albums: 'album', songs: 'track' }

// A music library: an overview of what was played and added lately, then
// every artist, album or song.
export default function Music({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const view = (views.includes(params.get('view') as View) ? params.get('view') : 'overview') as View
  const audio = useAudio()
  const labels = useLabels(lib)
  const shown = views.filter((v) => v !== 'labels' || !!labels.data?.length || view === 'labels')
  const shuffleAll = async () => audio.play(await api.libraryItems(lib.id, { kind: ['track'] }), -1, { shuffle: true })
  const pick = (v: View) => setParams(v === 'overview' ? {} : { view: v }, { replace: true })

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <LibraryIcon kind={lib.kind} className="size-6 text-accent" />
        <h1 className="text-2xl font-bold tracking-tight">{lib.name}</h1>
        {lib.scanning && <span className="animate-pulse text-sm text-accent">{t('library.scanning')}</span>}
        <Button variant="primary" className="ms-auto rounded-full" icon={<Shuffle className="size-4" />} onClick={shuffleAll}>
          {t('common:music.shuffleAll')}
        </Button>
      </div>
      <ViewTabs views={shown} view={view} label={(v) => t(`common:music.${v}`)} onPick={pick} />
      {view === 'overview' ? (
        <Overview lib={lib} onMore={pick} />
      ) : view === 'playlists' ? (
        <PlaylistGrid />
      ) : view === 'labels' ? (
        <Labels lib={lib} />
      ) : (
        <All lib={lib} view={view} />
      )}
    </div>
  )
}

function useMusic(lib: Library, filter: ItemFilter, key: string) {
  return useQuery({
    queryKey: ['music', lib.id, key, filter],
    queryFn: () => api.libraryItems(lib.id, filter),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (lib.scanning ? 3000 : pendingRefetch(q.state.data)),
  })
}

// The record labels of a library's albums; none from servers from before labels.
function useLabels(lib: Library) {
  return useQuery({
    queryKey: ['labels', lib.id],
    queryFn: () => api.labels(lib.id).catch(() => [] as RecordLabel[]),
    refetchInterval: lib.scanning ? 10_000 : false,
  })
}

// Where a label's page is: the library's Labels view, opened on it.
export function labelPath(libraryId: number, label: string) {
  return `/library/${libraryId}?${new URLSearchParams({ view: 'labels', label })}`
}

function Overview({ lib, onMore }: { lib: Library; onMore: (v: View) => void }) {
  const { t } = useTranslation()
  const played = useMusic(lib, { kind: ['album'], watched: 'started', sort: 'lastPlayed', limit: 12 }, 'played')
  const added = useMusic(lib, { kind: ['album'], sort: 'added', limit: 12 }, 'added')
  const artists = useMusic(lib, { kind: ['artist'], sort: 'lastPlayed', limit: 12 }, 'artists')
  const labels = useLabels(lib)
  const playlists = usePlaylists()
  if (added.isPending) return <GridSkeleton />
  if (!added.data?.length) {
    return (
      <EmptyState title={lib.scanning ? t('library.scanning') : t('nothingHereYet')}>
        {lib.scanning ? t('library.appearAsFound') : t('library.noMedia', { paths: lib.paths.join(', ') })}
      </EmptyState>
    )
  }
  return (
    <div className="space-y-10">
      {!!played.data?.length && (
        <Row title={t('common:music.recentlyPlayed')}>
          {played.data.map((a) => (
            <RowItem key={a.id}>
              <AlbumCard album={a} />
            </RowItem>
          ))}
        </Row>
      )}
      {!!playlists.data?.length && (
        <section>
          <button onClick={() => onMore('playlists')} className="mb-3 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {t('common:music.playlists')}
          </button>
          <div className="scrollbar-none -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8">
            {playlists.data.slice(0, 12).map((p) => (
              <div key={p.id} className="w-36 shrink-0 md:w-44">
                <PlaylistCard playlist={p} />
              </div>
            ))}
          </div>
        </section>
      )}
      <Row title={t('common:music.recentlyAdded')}>
        {added.data.map((a) => (
          <RowItem key={a.id}>
            <AlbumCard album={a} />
          </RowItem>
        ))}
      </Row>
      {!!artists.data?.length && (
        <section>
          <button onClick={() => onMore('artists')} className="mb-3 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {t('common:music.artists')}
          </button>
          <div className="scrollbar-none -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8">
            {artists.data.map((a) => (
              <div key={a.id} className="w-28 shrink-0 md:w-36">
                <ArtistCard artist={a} />
              </div>
            ))}
          </div>
        </section>
      )}
      {!!labels.data?.length && (
        <section>
          <button onClick={() => onMore('labels')} className="mb-3 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {t('common:music.labels')}
          </button>
          <div className="scrollbar-none -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8">
            {[...labels.data]
              .sort((a, b) => b.albums - a.albums)
              .slice(0, 12)
              .map((l) => (
                <div key={l.name} className="w-36 shrink-0 md:w-44">
                  <LabelCard lib={lib} label={l} />
                </div>
              ))}
          </div>
        </section>
      )}
    </div>
  )
}

// Every label, or one label's page when the address names it.
function Labels({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const labels = useLabels(lib)
  const open = params.get('label')
  if (open) return <LabelPage lib={lib} name={open} />
  if (labels.isPending) return <GridSkeleton />
  if (!labels.data?.length) return <EmptyState title={t('nothingHereYet')} />
  return (
    <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
      {labels.data.map((l) => (
        <LabelCard key={l.name} lib={lib} label={l} />
      ))}
    </div>
  )
}

// A label's tile: up to four of its album covers, its name and how many albums.
function LabelCard({ lib, label }: { lib: Library; label: RecordLabel }) {
  const { t } = useTranslation()
  const covers = label.covers.slice(0, label.covers.length >= 4 ? 4 : 1)
  return (
    <Link to={labelPath(lib.id, label.name)} className="group block animate-fade">
      <div
        className={`grid aspect-square overflow-hidden rounded-lg bg-surface shadow-md ring-1 ring-text/5 transition duration-200 group-hover:brightness-110 ${covers.length === 4 ? 'grid-cols-2' : ''}`}
      >
        {covers.length ? (
          covers.map((c) => <Artwork key={c.id} item={c} type="poster" className="size-full" />)
        ) : (
          <div className="grid place-items-center p-3 text-center font-display text-xl text-muted">{label.name}</div>
        )}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium group-hover:text-accent-strong">{label.name}</div>
        <div className="truncate text-xs text-muted">{t('common:count.albums', { count: label.albums })}</div>
      </div>
    </Link>
  )
}

// A label's page: play everything on it, its artists, and its albums, newest first.
function LabelPage({ lib, name }: { lib: Library; name: string }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const albums = useMusic(lib, { kind: ['album'], label: [name], sort: 'year' }, `label:${name}:albums`)
  const artists = useMusic(lib, { kind: ['artist'], label: [name] }, `label:${name}:artists`)
  // Play goes album by album, newest first, each in its own order.
  const play = async (shuffle: boolean) => {
    const tracks = shuffle
      ? await api.libraryItems(lib.id, { kind: ['track'], label: [name] })
      : (await Promise.all((albums.data ?? []).slice(0, 50).map((a) => api.tracks(a.id)))).flat()
    audio.play(tracks, shuffle ? -1 : 0, { shuffle })
  }
  return (
    <div className="space-y-8">
      <div className="space-y-3">
        <Link to={`/library/${lib.id}?view=labels`} className="inline-flex items-center gap-1 text-sm text-muted hover:text-text">
          <ChevronLeft className="size-4 rtl:rotate-180" />
          {t('common:music.labels')}
        </Link>
        <div className="text-xs font-semibold tracking-wide text-accent uppercase">{t('common:music.label')}</div>
        <h2 className="font-display text-3xl font-semibold tracking-tight md:text-4xl">{name}</h2>
        {!!albums.data?.length && <div className="text-sm text-muted">{t('common:count.albums', { count: albums.data.length })}</div>}
        <div className="flex flex-wrap gap-3">
          <Button variant="primary" className="rounded-full" icon={<Play className="size-4 fill-current" />} onClick={() => play(false)}>
            {t('item.play')}
          </Button>
          <Button className="rounded-full" icon={<Shuffle className="size-4" />} onClick={() => play(true)}>
            {t('common:music.shuffle')}
          </Button>
        </div>
      </div>
      {!!artists.data?.length && (
        <section>
          <h3 className="mb-3 text-lg font-semibold tracking-tight">{t('common:music.artists')}</h3>
          <div className="scrollbar-none -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8">
            {artists.data.map((a) => (
              <div key={a.id} className="w-28 shrink-0 md:w-36">
                <ArtistCard artist={a} />
              </div>
            ))}
          </div>
        </section>
      )}
      <section>
        <h3 className="mb-3 text-lg font-semibold tracking-tight">{t('common:music.albums')}</h3>
        {albums.isPending ? (
          <GridSkeleton />
        ) : !albums.data?.length ? (
          <EmptyState title={t('nothingHereYet')} />
        ) : (
          <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
            {albums.data.map((a) => (
              <AlbumCard key={a.id} album={a} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

function All({ lib, view }: { lib: Library; view: Exclude<View, 'overview' | 'playlists' | 'labels'> }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const sort = (sorts[view].includes(params.get('sort') as SortField) ? params.get('sort') : sorts[view][0]) as SortField
  const items = useMusic(lib, { kind: [kinds[view]], sort }, view)
  const setSort = (s: SortField) => setParams({ view, ...(s === sorts[view][0] ? {} : { sort: s }) }, { replace: true })
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <label className="ms-auto flex items-center gap-2 text-sm text-muted">
          {t('filters.sortBy')}
          <select
            value={sort}
            onChange={(e) => setSort(e.target.value as SortField)}
            className="rounded-lg border border-line bg-surface px-2 py-1.5 text-text focus:border-accent focus:outline-none"
          >
            {sorts[view].map((s) => (
              <option key={s} value={s}>
                {t(`common:sort.${s}`)}
              </option>
            ))}
          </select>
        </label>
      </div>
      {items.isPending ? (
        view === 'songs' ? (
          <Spinner />
        ) : (
          <GridSkeleton />
        )
      ) : !items.data?.length ? (
        <EmptyState title={t('nothingHereYet')} />
      ) : view === 'songs' ? (
        <Tracks items={items.data} showArt showArtist showAlbum numbered={false} />
      ) : (
        <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
          {items.data.map((it) => (view === 'artists' ? <ArtistCard key={it.id} artist={it} /> : <AlbumCard key={it.id} album={it} />))}
        </div>
      )}
    </div>
  )
}

// An album's cover, with a play button on hover that plays it right away.
export function AlbumCard({ album, sub }: { album: Item; sub?: string }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const play = async (e: React.MouseEvent) => {
    e.preventDefault()
    audio.play(await api.tracks(album.id), 0, { shuffle: false })
  }
  return (
    <div className="group relative animate-fade">
      <Link to={`/item/${album.id}`} className="block">
        <Artwork item={album} type="poster" className="aspect-square rounded-lg shadow-md ring-1 ring-text/5 transition duration-200 group-hover:brightness-110" />
        <div className="mt-2 px-0.5">
          <div className="truncate text-sm font-medium group-hover:text-accent-strong">{album.title}</div>
          <div className="truncate text-xs text-muted">{sub ?? ([album.artist, album.year].filter(Boolean).join(' · ') || ' ')}</div>
        </div>
      </Link>
      <button
        onClick={play}
        aria-label={`${t('item.play')} ${album.title}`}
        className="absolute end-2 top-[calc(100%-4.5rem)] grid size-11 translate-y-1 place-items-center rounded-full bg-accent text-on-accent opacity-0 shadow-xl transition group-hover:translate-y-0 group-hover:opacity-100 focus:translate-y-0 focus:opacity-100"
      >
        <Play className="size-5 translate-x-0.5 fill-current" />
      </button>
      <TrackMenu item={album} items={() => api.tracks(album.id)} className="absolute end-1 top-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 [&>button]:bg-black/50 [&>button]:text-white" />
    </div>
  )
}

// An artist's photo, round.
export function ArtistCard({ artist }: { artist: Item }) {
  const { t } = useTranslation()
  return (
    <Link to={`/item/${artist.id}`} className="group block animate-fade text-center">
      <Artwork item={artist} type="poster" className="aspect-square rounded-full shadow-md ring-1 ring-text/5 transition duration-200 group-hover:ring-accent/70" />
      <div className="mt-2 truncate text-sm font-medium group-hover:text-accent-strong">{artist.title}</div>
      <div className="truncate text-xs text-muted">{artist.childCount ? t('common:count.albums', { count: artist.childCount }) : ' '}</div>
    </Link>
  )
}
