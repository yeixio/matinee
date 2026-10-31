import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Pause, Pencil, Play, Shuffle, Trash2, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { api, imageUrl, type PlaylistDetail } from '../api'
import { Dialog } from '../components/Collections'
import { invalidatePlaylists, NewPlaylistCard, PlaylistCard, PlaylistCover, usePlaylists } from '../components/Playlists'
import { Tracks, type MenuEntry } from '../components/Tracks'
import { Button, EmptyState, inputClass, Spinner } from '../components/ui'
import { duration } from '../format'
import { useAudio } from '../player/AudioPlayer'

// Every playlist someone has, and a card to make another.
export function PlaylistGrid() {
  const { t } = useTranslation()
  const lists = usePlaylists()
  if (lists.isPending) return <Spinner />
  return (
    <div className="space-y-4">
      {!lists.data?.length && <p className="text-sm text-muted">{t('common:music.noPlaylistsHint')}</p>}
      <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
        <NewPlaylistCard />
        {lists.data?.map((p) => (
          <PlaylistCard key={p.id} playlist={p} />
        ))}
      </div>
    </div>
  )
}

export function PlaylistsPage() {
  const { t } = useTranslation()
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t('common:music.playlists')}</h1>
      <PlaylistGrid />
    </div>
  )
}

// One playlist: play or shuffle it, rename or delete it, and put its songs
// in order by dragging them or from their menus.
export default function PlaylistPage() {
  const { t } = useTranslation()
  const id = useParams().id ?? ''
  const qc = useQueryClient()
  const navigate = useNavigate()
  const audio = useAudio()
  const [renaming, setRenaming] = useState(false)
  const key = ['playlist', id]
  const { data: p, isPending, error } = useQuery({ queryKey: key, queryFn: () => api.playlist(id) })
  if (isPending) return <Spinner />
  if (error || !p) return <EmptyState title={t('common:music.playlistGone')} />

  const songs = p.entries
  const ids = new Set(songs.map((s) => s.id))
  const playingThis = !!audio.current && ids.has(audio.current.id) && audio.queue.entries.length === songs.length
  // Changes show right away, then the server's answer replaces them.
  const change = async (optimistic: PlaylistDetail | undefined, send: () => Promise<PlaylistDetail>) => {
    if (optimistic) qc.setQueryData(key, optimistic)
    try {
      qc.setQueryData(key, await send())
    } finally {
      invalidatePlaylists(qc)
    }
  }
  const move = (from: number, to: number) => {
    const next = [...songs]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    change({ ...p, entries: next }, () =>
      api.orderPlaylist(
        p.id,
        next.map((e) => e.entryId),
      ),
    )
  }
  const menu = (e: (typeof songs)[number], i: number): MenuEntry[] => [
    ...(i > 0 ? [{ icon: <ArrowUp className="size-4 text-muted" />, label: t('common:music.moveUp'), onClick: () => move(i, i - 1) }] : []),
    ...(i < songs.length - 1
      ? [{ icon: <ArrowDown className="size-4 text-muted" />, label: t('common:music.moveDown'), onClick: () => move(i, i + 1) }]
      : []),
    {
      icon: <X className="size-4 text-muted" />,
      label: t('common:music.removeFromPlaylist'),
      onClick: () =>
        change({ ...p, entries: songs.filter((s) => s.entryId !== e.entryId) }, () => api.removeFromPlaylist(p.id, e.entryId)),
    },
  ]
  const remove = async () => {
    if (!confirm(t('common:music.deleteConfirm', { name: p.name }))) return
    await api.deletePlaylist(p.id)
    invalidatePlaylists(qc)
    navigate(-1)
  }

  return (
    <div className="animate-fade space-y-8">
      <section className="relative -mx-4 -mt-4 md:-mx-8 md:-mt-8">
        {/* The first cover, blurred, colors the top of the page. */}
        <div className="absolute inset-0 overflow-hidden">
          {p.albumIds[0] && <img src={imageUrl(p.albumIds[0], 'poster')} alt="" className="size-full scale-110 object-cover opacity-50 blur-3xl" />}
          <div className="absolute inset-0 bg-gradient-to-b from-ink/10 via-ink/60 to-ink" />
        </div>
        <div className="relative flex flex-col items-center gap-6 px-4 pt-8 pb-2 text-center sm:flex-row sm:items-end sm:text-start md:gap-8 md:px-8 md:pt-20">
          <PlaylistCover playlist={p} className="w-48 shrink-0 rounded-xl shadow-2xl shadow-black/60 md:w-60" />
          <div className="min-w-0 flex-1 space-y-3 pb-1">
            <div className="text-xs font-semibold tracking-widest text-muted uppercase">{t('common:music.playlist')}</div>
            <h1 className="text-3xl font-bold tracking-tight text-balance break-words md:text-6xl">{p.name}</h1>
            <div className="flex flex-wrap items-center justify-center gap-x-2 text-sm text-muted sm:justify-start">
              <span>{t('common:count.tracks', { count: p.count })}</span>
              {p.durationMs > 0 && <span className="before:me-2 before:content-['·']">{duration(p.durationMs)}</span>}
            </div>
          </div>
        </div>
      </section>
      <div className="flex flex-wrap items-center gap-3">
        <button
          disabled={!songs.length}
          onClick={() => (playingThis ? audio.toggle() : audio.play(songs, 0, { shuffle: false }))}
          className="grid size-14 place-items-center rounded-full bg-accent text-on-accent shadow-lg transition hover:scale-105 hover:bg-accent-strong disabled:opacity-50"
          aria-label={playingThis && audio.playing ? t('player.pause') : t('item.play')}
        >
          {playingThis && audio.playing ? <Pause className="size-6 fill-current" /> : <Play className="size-6 translate-x-0.5 fill-current" />}
        </button>
        <Button disabled={!songs.length} className="h-11 rounded-full" icon={<Shuffle className="size-4" />} onClick={() => audio.play(songs, -1, { shuffle: true })}>
          {t('common:music.shuffle')}
        </Button>
        <Button className="h-11 rounded-full" variant="ghost" icon={<Pencil className="size-4" />} onClick={() => setRenaming(true)}>
          {t('common:music.rename')}
        </Button>
        <Button className="h-11 rounded-full" variant="ghost" icon={<Trash2 className="size-4" />} onClick={remove}>
          {t('common:music.deletePlaylist')}
        </Button>
      </div>
      {songs.length ? (
        <Tracks items={songs} showArt showArtist showAlbum numbered={false} menu={menu} onMove={move} />
      ) : (
        <EmptyState title={t('common:music.emptyPlaylist')}>{t('common:music.emptyPlaylistHint')}</EmptyState>
      )}
      {renaming && (
        <RenameDialog
          name={p.name}
          onClose={() => setRenaming(false)}
          onSave={(name) => change(undefined, () => api.renamePlaylist(p.id, name)).then(() => setRenaming(false))}
        />
      )}
    </div>
  )
}

function RenameDialog({ name, onClose, onSave }: { name: string; onClose: () => void; onSave: (name: string) => Promise<void> }) {
  const { t } = useTranslation()
  const [value, setValue] = useState(name)
  const [error, setError] = useState<string>()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onSave(value).catch((err: Error) => setError(err.message))
  }
  return (
    <Dialog title={t('common:music.rename')} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          maxLength={100}
          aria-label={t('common:music.playlistName')}
          className={inputClass}
        />
        {error && <p className="text-sm text-danger">{error}</p>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={!value.trim()}>
            {t('save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
