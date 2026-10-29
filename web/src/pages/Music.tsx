import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Play, Shuffle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, pendingRefetch, type Item, type Library } from '../api'
import { Artwork } from '../components/Artwork'
import { GridSkeleton } from '../components/Card'
import { LibraryIcon } from '../components/Shell'
import { Row, RowItem } from '../components/Row'
import { TrackMenu, Tracks } from '../components/Tracks'
import { Button, EmptyState, Spinner } from '../components/ui'
import type { ItemFilter, SortField } from '../filter'
import { useAudio } from '../player/AudioPlayer'

type View = 'overview' | 'artists' | 'albums' | 'songs'
const views: View[] = ['overview', 'artists', 'albums', 'songs']

// How each view sorts, and the orders it offers.
const sorts: Record<Exclude<View, 'overview'>, SortField[]> = {
  artists: ['title', 'added', 'lastPlayed'],
  albums: ['added', 'title', 'year', 'lastPlayed'],
  songs: ['title', 'added', 'lastPlayed'],
}
const kinds = { artists: 'artist', albums: 'album', songs: 'track' }

// A music library: an overview of what was played and added lately, then
// every artist, album or song.
export default function Music({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const view = (views.includes(params.get('view') as View) ? params.get('view') : 'overview') as View
  const audio = useAudio()
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
      <nav className="scrollbar-none -mx-4 flex gap-2 overflow-x-auto px-4 md:mx-0 md:px-0" role="tablist">
        {views.map((v) => (
          <button
            key={v}
            role="tab"
            aria-selected={view === v}
            onClick={() => pick(v)}
            className={`shrink-0 rounded-full px-4 py-1.5 text-sm transition-colors ${
              view === v ? 'bg-text font-medium text-ink' : 'bg-text/5 text-muted hover:bg-text/10 hover:text-text'
            }`}
          >
            {t(`common:music.${v}`)}
          </button>
        ))}
      </nav>
      {view === 'overview' ? <Overview lib={lib} onMore={pick} /> : <All lib={lib} view={view} />}
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

function Overview({ lib, onMore }: { lib: Library; onMore: (v: View) => void }) {
  const { t } = useTranslation()
  const played = useMusic(lib, { kind: ['album'], watched: 'started', sort: 'lastPlayed', limit: 12 }, 'played')
  const added = useMusic(lib, { kind: ['album'], sort: 'added', limit: 12 }, 'added')
  const artists = useMusic(lib, { kind: ['artist'], sort: 'lastPlayed', limit: 12 }, 'artists')
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
    </div>
  )
}

function All({ lib, view }: { lib: Library; view: Exclude<View, 'overview'> }) {
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
