import { useQuery } from '@tanstack/react-query'
import { Image as ImageIcon, Pause, Play, Shuffle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Item, type ItemDetail, type User } from '../api'
import { Artwork } from '../components/Artwork'
import { AddToCollectionButton } from '../components/Collections'
import { CoverPicker } from '../components/CoverPicker'
import { Row, RowItem } from '../components/Row'
import { TrackMenu, Tracks } from '../components/Tracks'
import { Button } from '../components/ui'
import { duration } from '../format'
import { useAudio } from '../player/AudioPlayer'
import { AlbumCard } from './Music'

// An album: its cover, the songs, and more by the same artist.
export function AlbumPage({ item, tracks }: { item: ItemDetail; tracks?: Item[] }) {
  const { t } = useTranslation()
  const total = tracks?.reduce((sum, tr) => sum + (tr.durationMs ?? 0), 0)
  const more = useQuery({
    queryKey: ['children', item.artistId],
    queryFn: () => api.children(item.artistId!),
    enabled: !!item.artistId,
  })
  const others = more.data?.filter((a) => a.id !== item.id).sort((a, b) => (b.year ?? 0) - (a.year ?? 0))
  return (
    <div className="animate-fade space-y-10">
      <Hero item={item} label={t('common:music.album')} round={false}>
        {item.artistId && (
          <Link to={`/item/${item.artistId}`} className="font-semibold text-text hover:underline">
            {item.artist}
          </Link>
        )}
        {item.year && <span>{item.year}</span>}
        {tracks && <span>{t('common:count.tracks', { count: tracks.length })}</span>}
        {!!total && <span>{duration(total)}</span>}
      </Hero>
      <Actions item={item} tracks={tracks} />
      {item.overview && <Overview text={item.overview} />}
      {tracks && (
        <div className="relative">
          {/* A soundtrack or compilation names each song's artist. */}
          <Tracks items={tracks} showArtist={tracks.some((t) => t.artist !== item.artist)} />
        </div>
      )}
      {!!others?.length && (
        <Row title={t('common:music.moreBy', { artist: item.artist })}>
          {others.map((a) => (
            <RowItem key={a.id}>
              <AlbumCard album={a} sub={a.year ? String(a.year) : ''} />
            </RowItem>
          ))}
        </Row>
      )}
    </div>
  )
}

// An artist: their photo, what's known about them, and their albums,
// newest first.
export function ArtistPage({ item, albums }: { item: ItemDetail; albums?: Item[] }) {
  const { t } = useTranslation()
  const tracks = useQuery({ queryKey: ['tracks', item.id], queryFn: () => api.tracks(item.id) })
  const sorted = albums && [...albums].sort((a, b) => (b.year ?? 0) - (a.year ?? 0))
  return (
    <div className="animate-fade space-y-10">
      <Hero item={item} label={t('common:music.artist')} round>
        {albums && <span>{t('common:count.albums', { count: albums.length })}</span>}
        {tracks.data && <span>{t('common:count.tracks', { count: tracks.data.length })}</span>}
      </Hero>
      <Actions item={item} tracks={tracks.data} />
      {item.overview && <Overview text={item.overview} />}
      {!!sorted?.length && (
        <section className="relative space-y-4">
          <h2 className="text-lg font-semibold">{t('common:music.albums')}</h2>
          <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
            {sorted.map((a) => (
              <AlbumCard key={a.id} album={a} sub={a.year ? String(a.year) : ''} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

export function Hero({ item, label, round, children }: { item: ItemDetail; label: string; round: boolean; children: React.ReactNode }) {
  return (
    <section className="relative -mx-4 -mt-4 md:-mx-8 md:-mt-8">
      {/* The cover, blurred, colors the top of the page. */}
      <div className="absolute inset-0 overflow-hidden">
        <Artwork item={item} type="poster" label={false} className="size-full scale-110 opacity-50 blur-3xl" />
        <div className="absolute inset-0 bg-gradient-to-b from-ink/10 via-ink/60 to-ink" />
      </div>
      <div className="relative flex flex-col items-center gap-6 px-4 pt-8 pb-2 text-center sm:flex-row sm:items-end sm:text-start md:gap-8 md:px-8 md:pt-20">
        <HeroCover item={item} round={round} />
        <div className="min-w-0 flex-1 space-y-3 pb-1">
          <div className="text-xs font-semibold tracking-widest text-muted uppercase">{label}</div>
          <h1 className="text-3xl font-bold tracking-tight text-balance md:text-6xl">{item.title}</h1>
          <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-sm text-muted sm:justify-start [&>*:not(:first-child)]:before:me-2 [&>*:not(:first-child)]:before:content-['·']">
            {children}
          </div>
        </div>
      </div>
    </section>
  )
}

// The cover, which an admin clicks to pick another for an album or book.
function HeroCover({ item, round }: { item: ItemDetail; round: boolean }) {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const [picking, setPicking] = useState(false)
  const cover = (
    <Artwork
      item={item}
      type="poster"
      className={`aspect-square w-48 shrink-0 shadow-2xl shadow-black/60 ring-1 ring-text/10 md:w-60 ${round ? 'rounded-full' : 'rounded-xl'}`}
    />
  )
  if (!me?.isAdmin || (item.kind !== 'album' && item.kind !== 'book')) return cover
  return (
    <>
      <button
        type="button"
        onClick={() => setPicking(true)}
        title={t('cover.change')}
        aria-label={t('cover.change')}
        className="group relative shrink-0 rounded-xl focus-visible:outline-2 focus-visible:outline-accent"
      >
        {cover}
        <span className="absolute inset-x-0 bottom-0 flex items-center justify-center gap-1.5 rounded-b-xl bg-black/60 py-2 text-xs font-semibold text-white opacity-0 transition group-hover:opacity-100 group-focus-visible:opacity-100">
          <ImageIcon className="size-3.5" /> {t('cover.change')}
        </span>
      </button>
      {picking && <CoverPicker item={item} onClose={() => setPicking(false)} />}
    </>
  )
}

// Play, shuffle and the ⋯ menu. Play pauses when this is what's playing.
function Actions({ item, tracks }: { item: ItemDetail; tracks?: Item[] }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const ids = new Set(tracks?.map((tr) => tr.id))
  const playingThis = !!audio.current && ids.has(audio.current.id) && audio.queue.entries.length === tracks?.length
  return (
    <div className="relative flex flex-wrap items-center gap-3">
      <button
        disabled={!tracks?.length}
        onClick={() => (playingThis ? audio.toggle() : tracks && audio.play(tracks, 0, { shuffle: false }))}
        className="grid size-14 place-items-center rounded-full bg-accent text-on-accent shadow-lg transition hover:scale-105 hover:bg-accent-strong disabled:opacity-50"
        aria-label={playingThis && audio.playing ? t('player.pause') : t('item.play')}
      >
        {playingThis && audio.playing ? <Pause className="size-6 fill-current" /> : <Play className="size-6 translate-x-0.5 fill-current" />}
      </button>
      <Button
        disabled={!tracks?.length}
        className="h-11 rounded-full"
        icon={<Shuffle className="size-4" />}
        onClick={() => tracks && audio.play(tracks, -1, { shuffle: true })}
      >
        {t('common:music.shuffle')}
      </Button>
      <AddToCollectionButton item={item} />
      <TrackMenu item={item} items={async () => tracks ?? api.tracks(item.id)} />
    </div>
  )
}

// A biography, cut short until opened.
export function Overview({ text }: { text: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <section className="relative max-w-3xl">
      <p className={`leading-relaxed text-text/85 ${open ? '' : 'line-clamp-4'}`}>{text}</p>
      {text.length > 300 && (
        <button onClick={() => setOpen(!open)} className="mt-1 text-sm font-medium text-muted hover:text-text">
          {open ? t('common:music.less') : t('common:music.readMore')}
        </button>
      )}
    </section>
  )
}
