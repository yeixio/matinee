import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ListMusic, Plus } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { api, imageUrl, type Item, type Playlist } from '../api'
import { useAudio } from '../player/AudioPlayer'
import { Dialog } from './Collections'
import { Button, inputClass } from './ui'

export const playlistLink = (p: { id: string }) => `/playlist/${encodeURIComponent(p.id)}`

export function usePlaylists() {
  return useQuery({ queryKey: ['playlists'], queryFn: () => api.playlists() })
}

/** Everything that shows playlists refetches after a change. */
export function invalidatePlaylists(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['playlists'] })
  qc.invalidateQueries({ queryKey: ['playlist'] })
}

/** A square cover made from up to four of the playlist's albums. */
export function PlaylistCover({ playlist, className = '' }: { playlist: Pick<Playlist, 'albumIds'>; className?: string }) {
  const ids = playlist.albumIds.slice(0, 4)
  // Four covers make a grid; fewer, and the first fills it.
  const shown = ids.length === 4 ? ids : ids.slice(0, 1)
  return (
    <div className={`grid aspect-square overflow-hidden bg-raised ring-1 ring-text/5 ${shown.length === 4 ? 'grid-cols-2 grid-rows-2' : ''} ${className}`}>
      {shown.length === 0 ? (
        <div className="grid place-items-center bg-gradient-to-br from-accent/30 to-accent/5">
          <ListMusic className="size-1/3 text-accent" aria-hidden />
        </div>
      ) : (
        shown.map((id) => <img key={id} src={imageUrl(id, 'poster')} alt="" loading="lazy" className="size-full object-cover" />)
      )}
    </div>
  )
}

export function PlaylistCard({ playlist }: { playlist: Playlist }) {
  const { t } = useTranslation()
  return (
    <Link to={playlistLink(playlist)} className="group block animate-fade">
      <PlaylistCover playlist={playlist} className="rounded-lg shadow-md transition duration-200 group-hover:brightness-110 group-hover:ring-accent/70" />
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium group-hover:text-accent-strong">{playlist.name}</div>
        <div className="truncate text-xs text-muted">{t('common:count.tracks', { count: playlist.count })}</div>
      </div>
    </Link>
  )
}

/** A card that makes a new playlist and opens it. */
export function NewPlaylistCard() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  return (
    <>
      <button onClick={() => setOpen(true)} className="group block animate-fade text-start">
        <div className="grid aspect-square place-items-center rounded-lg border-2 border-dashed border-line text-muted transition group-hover:border-accent group-hover:text-accent">
          <Plus className="size-10" />
        </div>
        <div className="mt-2 px-0.5 text-sm font-medium group-hover:text-accent-strong">{t('common:music.newPlaylist')}</div>
      </button>
      {open && <NewPlaylistDialog onClose={() => setOpen(false)} onMade={(p) => navigate(playlistLink(p))} />}
    </>
  )
}

/** Asks for a name and makes a playlist, with songs on it already when given. */
export function NewPlaylistDialog({
  songs,
  onClose,
  onMade,
}: {
  songs?: () => Promise<Item[]>
  onClose: () => void
  onMade?: (p: Playlist) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const audio = useAudio()
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(undefined)
    try {
      const items = songs ? (await songs()).map((s) => s.id) : []
      const p = await api.createPlaylist(name, items)
      invalidatePlaylists(qc)
      if (items.length) audio.say(t('common:music.addedToPlaylist', { name: p.name }))
      onClose()
      onMade?.(p)
    } catch (err) {
      setError((err as Error).message)
      setBusy(false)
    }
  }
  return (
    <Dialog title={t('common:music.newPlaylist')} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
          placeholder={t('common:music.playlistName')}
          aria-label={t('common:music.playlistName')}
          className={inputClass}
        />
        {error && <p className="text-sm text-danger">{error}</p>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
            {t('create')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

/** Picks a playlist to add songs to, or makes a new one for them. */
export function AddToPlaylistDialog({ songs, onClose }: { songs: () => Promise<Item[]>; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const audio = useAudio()
  const lists = usePlaylists()
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string>()
  if (creating) return <NewPlaylistDialog songs={songs} onClose={onClose} />
  const add = async (p: Playlist) => {
    try {
      await api.addToPlaylist(
        p.id,
        (await songs()).map((s) => s.id),
      )
      invalidatePlaylists(qc)
      audio.say(t('common:music.addedToPlaylist', { name: p.name }))
      onClose()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Dialog title={t('common:music.addToPlaylist')} onClose={onClose}>
      <ul className="-mx-2 space-y-0.5">
        <li>
          <button onClick={() => setCreating(true)} className="flex w-full items-center gap-3 rounded-lg p-2 text-start hover:bg-text/5">
            <span className="grid size-12 shrink-0 place-items-center rounded-md border-2 border-dashed border-line text-muted">
              <Plus className="size-5" />
            </span>
            <span className="font-medium">{t('common:music.newPlaylist')}</span>
          </button>
        </li>
        {lists.data?.map((p) => (
          <li key={p.id}>
            <button onClick={() => add(p)} className="flex w-full items-center gap-3 rounded-lg p-2 text-start hover:bg-text/5">
              <PlaylistCover playlist={p} className="size-12 shrink-0 rounded-md" />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{p.name}</span>
                <span className="block truncate text-xs text-muted">{t('common:count.tracks', { count: p.count })}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {error && <p className="mt-3 text-sm text-danger">{error}</p>}
    </Dialog>
  )
}
