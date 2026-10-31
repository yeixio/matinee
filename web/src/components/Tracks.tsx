import { Disc3, Ellipsis, GripVertical, ListEnd, ListPlus, ListStart, Mic2, Play } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import type { Item } from '../api'
import { clock } from '../format'
import { useAudio } from '../player/AudioPlayer'
import { Artwork } from './Artwork'
import { AddToPlaylistDialog } from './Playlists'

// The bars that bounce beside the track that's playing.
export function Equalizer({ playing, className = '' }: { playing: boolean; className?: string }) {
  return (
    <span className={`inline-flex h-3.5 items-end gap-[2px] ${className}`} aria-hidden>
      {[0, 1, 2].map((i) => (
        <span
          key={i}
          className={`w-[3px] rounded-sm bg-accent ${playing ? 'animate-eq' : ''}`}
          style={{ height: `${[50, 80, 35][i]}%`, animationDelay: `${i * -0.35}s` }}
        />
      ))}
    </span>
  )
}

// One more choice in a song's ⋯ menu, such as removing it from a playlist.
export type MenuEntry = { icon: ReactNode; label: string; onClick: () => void }

// A list of songs: click one to play the list from there. Each has a menu
// to play it next, queue it, add it to a playlist, or go to its album or
// artist. With onMove, songs can be dragged into a new order.
export function Tracks<T extends Item>({
  items,
  showArt = false,
  showAlbum = false,
  showArtist = false,
  numbered = true,
  menu,
  onMove,
}: {
  items: T[]
  showArt?: boolean
  showAlbum?: boolean
  showArtist?: boolean
  numbered?: boolean
  menu?: (item: T, index: number) => MenuEntry[]
  onMove?: (from: number, to: number) => void
}) {
  const audio = useAudio()
  const [dragging, setDragging] = useState<number>()
  const [over, setOver] = useState<number>()
  const discs = new Set(items.map((t) => t.disc ?? 1)).size > 1
  return (
    <ol className="space-y-0.5">
      {items.map((t, i) => {
        const current = audio.current?.id === t.id
        const drag = onMove && {
          draggable: true,
          onDragStart: () => setDragging(i),
          onDragEnd: () => {
            setDragging(undefined)
            setOver(undefined)
          },
          onDragOver: (ev: React.DragEvent) => {
            ev.preventDefault()
            setOver(i)
          },
          onDrop: () => {
            if (dragging !== undefined && dragging !== i) onMove(dragging, i)
            setDragging(undefined)
            setOver(undefined)
          },
        }
        return (
          <li
            key={'entryId' in t ? String(t.entryId) : `${t.id}-${i}`}
            {...drag}
            className={`group flex items-center gap-3 rounded-lg px-2 py-1.5 transition-colors hover:bg-text/5 md:gap-4 md:px-3 ${current ? 'bg-text/5' : ''} ${
              dragging === i ? 'opacity-40' : ''
            } ${over === i && dragging !== i ? 'ring-1 ring-accent/60' : ''}`}
          >
            {onMove && <GripVertical className="-me-1 hidden size-4 shrink-0 cursor-grab text-faint md:block" aria-hidden />}
            <button onClick={() => audio.play(items, i, { shuffle: false })} className="flex min-w-0 flex-1 items-center gap-3 text-start md:gap-4">
              {numbered && (
                <span className="grid w-7 shrink-0 place-items-center text-sm text-faint tabular-nums">
                  {current ? (
                    <Equalizer playing={audio.playing} />
                  ) : (
                    <>
                      <span className="group-hover:hidden">{discs ? `${t.disc ?? 1}.${t.index ?? ''}` : (t.index ?? i + 1)}</span>
                      <Play className="hidden size-3.5 fill-current text-text group-hover:block" />
                    </>
                  )}
                </span>
              )}
              {showArt && (
                <span className="relative shrink-0">
                  <Artwork item={t} type="poster" label={false} className="size-10 rounded-md" />
                  {current && !numbered && (
                    <span className="absolute inset-0 grid place-items-center rounded-md bg-black/50">
                      <Equalizer playing={audio.playing} />
                    </span>
                  )}
                </span>
              )}
              <span className="min-w-0 flex-1">
                <span className={`block truncate ${current ? 'font-medium text-accent' : ''}`}>{t.title}</span>
                {(showArtist || showAlbum) && (
                  <span className="block truncate text-xs text-muted">
                    {showArtist && t.artist}
                    {/* Wide screens give the album its own column. */}
                    {showAlbum && t.album && <span className="lg:hidden">{showArtist && t.artist ? ` · ${t.album}` : t.album}</span>}
                  </span>
                )}
              </span>
            </button>
            {showAlbum && t.parentId && (
              <Link to={`/item/${t.parentId}`} className="hidden w-1/4 truncate text-sm text-muted hover:text-text hover:underline lg:block">
                {t.album}
              </Link>
            )}
            <span className="w-12 shrink-0 text-end text-sm text-muted tabular-nums">{t.durationMs ? clock(t.durationMs / 1000) : ''}</span>
            <TrackMenu item={t} extra={menu?.(t, i)} />
          </li>
        )
      })}
    </ol>
  )
}

// The ⋯ menu on a song, an album or an artist.
export function TrackMenu({
  item,
  items,
  extra,
  className = '',
}: {
  item: Item
  items?: () => Promise<Item[]>
  extra?: MenuEntry[]
  className?: string
}) {
  const { t } = useTranslation()
  const audio = useAudio()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [picking, setPicking] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('pointerdown', close)
    window.addEventListener('keydown', esc)
    return () => {
      window.removeEventListener('pointerdown', close)
      window.removeEventListener('keydown', esc)
    }
  }, [open])
  const songs = async () => (items ? items() : [item])
  const act = (f: () => void) => () => {
    setOpen(false)
    f()
  }
  const albumId = item.kind === 'track' ? item.parentId : undefined
  const artistId = item.kind === 'artist' ? undefined : item.artistId
  const entry = (icon: ReactNode, label: string, onClick: () => void) => (
    <li key={label}>
      <button role="menuitem" onClick={act(onClick)} className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-text/10">
        {icon}
        <span className="truncate">{label}</span>
      </button>
    </li>
  )
  return (
    <div ref={ref} className={`relative shrink-0 ${className}`}>
      <button
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t('common:music.more')}
        title={t('common:music.more')}
        className={`grid size-8 place-items-center rounded-full text-muted transition hover:bg-text/10 hover:text-text ${open ? 'bg-text/10 text-text' : ''}`}
      >
        <Ellipsis className="size-5" />
      </button>
      {open && (
        <ul role="menu" className="absolute end-0 top-full z-40 mt-1 w-56 animate-fade rounded-xl border border-line bg-raised p-1 shadow-2xl shadow-black/50">
          {entry(<ListStart className="size-4 text-muted" />, t('common:music.playNext'), () => songs().then(audio.playNext))}
          {entry(<ListEnd className="size-4 text-muted" />, t('common:music.addToQueue'), () => songs().then(audio.addToQueue))}
          {entry(<ListPlus className="size-4 text-muted" />, t('common:music.addToPlaylist'), () => setPicking(true))}
          {extra?.map((e) => entry(e.icon, e.label, e.onClick))}
          {albumId && entry(<Disc3 className="size-4 text-muted" />, t('common:music.goToAlbum'), () => navigate(`/item/${albumId}`))}
          {artistId && entry(<Mic2 className="size-4 text-muted" />, t('common:music.goToArtist'), () => navigate(`/item/${artistId}`))}
        </ul>
      )}
      {picking && <AddToPlaylistDialog songs={songs} onClose={() => setPicking(false)} />}
    </div>
  )
}
