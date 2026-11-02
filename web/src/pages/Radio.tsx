import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Link2, Pencil, Play, Plus, Radio as RadioIcon, Search, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type FoundStation, type Item, type Library, type User } from '../api'
import { Artwork } from '../components/Artwork'
import { GridSkeleton } from '../components/Card'
import { Equalizer } from '../components/Tracks'
import { Button, Spinner } from '../components/ui'
import { useAudio } from '../player/AudioPlayer'

const inputClass = 'h-10 w-full rounded-lg border border-line bg-surface px-3 text-sm focus:border-accent focus:outline-none'

// Genres to browse the station directory by: its tags, in English.
export const radioGenres = ['electronic', 'trance', 'house', 'chillout', 'pop', 'rock', 'jazz', 'classical', 'hip hop', 'news'] as const
const genreKey = (g: string) => g.replace(' ', '')

// A music library's radio stations, which play live through the server.
export function useStations(lib: Library) {
  return useQuery({
    queryKey: ['stations', lib.id],
    queryFn: () => api.stations(lib.id).catch(() => [] as Item[]),
  })
}

// The Radio tab: the library's stations, then finding more in the station
// directory or adding one by its stream's address. Kids profiles and
// friends listen; the household's grown-ups add and change stations.
export function Radio({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const me = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const stations = useStations(lib)
  const canEdit = !!me.data && !me.data.kidsLevel && !me.data.friend
  if (stations.isPending || me.isPending) return <GridSkeleton />
  return (
    <div className="space-y-10">
      <section className="space-y-4">
        {stations.data?.length ? (
          <StationGrid>
            {stations.data.map((s) => (
              <StationCard key={s.id} station={s} canEdit={canEdit} />
            ))}
          </StationGrid>
        ) : (
          <div className="flex items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-5">
            <RadioIcon className="mt-0.5 size-6 shrink-0 text-accent" />
            <div>
              <p className="font-medium">{t('common:radio.noStations')}</p>
              {canEdit && <p className="mt-1 text-sm text-muted">{t('common:radio.noStationsHint')}</p>}
            </div>
          </div>
        )}
      </section>
      {canEdit && (
        <>
          <FindStations lib={lib} />
          <AddByAddress lib={lib} />
        </>
      )}
    </div>
  )
}

export function StationGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))' }}>
      {children}
    </div>
  )
}

// A station's tile: its logo, which plays it, its name and genres, and
// for the household, renaming and removing it.
export function StationCard({ station, canEdit = false }: { station: Item; canEdit?: boolean }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const qc = useQueryClient()
  const on = audio.current?.id === station.id
  const done = () => qc.invalidateQueries({ queryKey: ['stations', station.libraryId] })
  const rename = useMutation({ mutationFn: (name: string) => api.renameStation(station.id, name), onSuccess: done })
  const remove = useMutation({ mutationFn: () => api.deleteStation(station.id), onSuccess: done })
  return (
    <div className="group relative animate-fade">
      <button
        onClick={() => (on ? audio.toggle() : audio.playItem(station))}
        className="relative block w-full"
        aria-label={on && audio.playing ? t('player.pause') : t('common:radio.play', { name: station.title })}
      >
        <Artwork item={station} type="poster" className="aspect-square rounded-lg shadow-md ring-1 ring-text/5 transition duration-200 group-hover:brightness-110" />
        <span
          className={`absolute end-2 bottom-2 grid size-10 place-items-center rounded-full bg-accent text-on-accent shadow-lg transition ${on ? 'opacity-100' : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'}`}
        >
          {on && audio.playing ? <Equalizer playing className="text-on-accent" /> : <Play className="size-4 translate-x-px fill-current" />}
        </span>
      </button>
      {canEdit && (
        <div className="absolute end-1.5 top-1.5 flex gap-1 opacity-100 transition md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100">
          <button
            onClick={() => {
              const name = prompt(t('common:radio.renamePrompt'), station.title)?.trim()
              if (name && name !== station.title) rename.mutate(name)
            }}
            className="grid size-8 place-items-center rounded-full bg-black/55 text-white backdrop-blur hover:bg-black/75"
            aria-label={t('common:radio.rename')}
            title={t('common:radio.rename')}
          >
            <Pencil className="size-3.5" />
          </button>
          <button
            onClick={() => confirm(t('common:radio.removeConfirm', { name: station.title })) && remove.mutate()}
            className="grid size-8 place-items-center rounded-full bg-black/55 text-white backdrop-blur hover:bg-danger"
            aria-label={t('common:radio.remove')}
            title={t('common:radio.remove')}
          >
            <Trash2 className="size-3.5" />
          </button>
        </div>
      )}
      <div className="mt-2 px-0.5">
        <div className={`truncate text-sm font-medium ${on ? 'text-accent-strong' : ''}`}>{station.title}</div>
        <div className="truncate text-xs text-muted">{(station.genres ?? []).slice(0, 2).join(' · ') || ' '}</div>
      </div>
      {(rename.error || remove.error) && <p className="mt-1 text-xs text-danger">{(rename.error ?? remove.error)?.message}</p>}
    </div>
  )
}

function useAddStation(lib: Library) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (s: { url: string; name?: string; image?: string; homepage?: string; tags?: string[] }) => api.addStation({ libraryId: lib.id, ...s }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['stations', lib.id] })
      qc.invalidateQueries({ queryKey: ['stationDirectory'] })
    },
  })
}

// Finding stations in Radio Browser, by name or genre.
function FindStations({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const [term, setTerm] = useState('')
  const [genre, setGenre] = useState('')
  const asked = term || genre
  const dir = useQuery({
    queryKey: ['stationDirectory', lib.id, term, genre],
    queryFn: () => api.stationDirectory(term ? { q: term, library: lib.id } : { tag: genre, library: lib.id }),
    enabled: !!asked,
    staleTime: 10 * 60_000,
    placeholderData: keepPreviousData,
  })
  const search = (e: FormEvent) => {
    e.preventDefault()
    setTerm(q.trim())
    if (q.trim()) setGenre('')
  }
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('common:radio.find')}</h2>
      <form onSubmit={search} className="flex gap-2">
        <label className="relative flex-1">
          <Search className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted" />
          <input
            className={`${inputClass} ps-9`}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t('common:radio.searchPlaceholder')}
            aria-label={t('common:radio.search')}
          />
        </label>
        <Button variant="primary">{t('common:radio.search')}</Button>
      </form>
      <div role="group" aria-label={t('common:radio.genres')} className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1 md:flex-wrap md:overflow-visible">
        {radioGenres.map((g) => {
          const on = !term && genre === g
          return (
            <button
              key={g}
              type="button"
              aria-pressed={on}
              onClick={() => {
                setGenre(g)
                setTerm('')
                setQ('')
              }}
              className={`h-8 shrink-0 rounded-full px-3.5 text-sm transition-colors ${on ? 'bg-text font-medium text-ink' : 'bg-text/5 text-muted hover:bg-text/10 hover:text-text'}`}
            >
              {t(`common:radio.genre.${genreKey(g)}`)}
            </button>
          )
        })}
      </div>
      {!asked ? (
        <p className="text-sm text-muted">{t('common:radio.findHint')}</p>
      ) : dir.isPending ? (
        <Spinner />
      ) : dir.error ? (
        <p className="text-sm text-danger">{dir.error.message}</p>
      ) : !dir.data?.length ? (
        <p className="text-sm text-muted">{t('common:radio.nothingFound')}</p>
      ) : (
        <ul className="grid gap-3 md:grid-cols-2">
          {dir.data.map((s) => (
            <FoundRow key={s.url} lib={lib} station={s} />
          ))}
        </ul>
      )}
      <p className="text-xs text-faint">{t('common:radio.poweredBy')}</p>
    </section>
  )
}

function FoundRow({ lib, station }: { lib: Library; station: FoundStation }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const add = useAddStation(lib)
  const have = station.itemId ?? add.data?.id
  const [broken, setBroken] = useState(false)
  const facts = [station.country, station.codec && station.bitrate ? `${station.codec} ${station.bitrate} kbps` : station.codec, ...(station.tags ?? []).slice(0, 3)].filter(Boolean)
  return (
    <li className="flex items-center gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
      {station.image && !broken ? (
        <img src={station.image} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setBroken(true)} className="size-14 shrink-0 rounded-lg bg-white/90 object-contain p-1" />
      ) : (
        <div className="grid size-14 shrink-0 place-items-center rounded-lg bg-text/5">
          <RadioIcon className="size-6 text-faint" />
        </div>
      )}
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="truncate font-medium">{station.name}</div>
        <div className="truncate text-xs text-muted">{facts.join(' · ')}</div>
        {add.error && <p className="text-xs text-danger">{add.error.message}</p>}
      </div>
      <div className="shrink-0">
        {have ? (
          <Button
            variant="ghost"
            icon={<Check className="size-4" />}
            onClick={async () => audio.playItem(add.data ?? (await api.item(have)))}
            aria-label={t('common:radio.play', { name: station.name })}
          >
            {t('common:radio.added')}
          </Button>
        ) : (
          <Button
            icon={add.isPending ? <Spinner className="size-4" /> : <Plus className="size-4" />}
            disabled={add.isPending}
            onClick={() => add.mutate({ url: station.url, name: station.name, image: station.image, homepage: station.homepage, tags: station.tags?.slice(0, 5) })}
          >
            {t('common:radio.add')}
          </Button>
        )}
      </div>
    </li>
  )
}

// A station that isn't in the directory: the address of its stream, or of
// a playlist (.pls, .m3u) from its website.
function AddByAddress({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const add = useAddStation(lib)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!url.trim()) return
    add.mutate(
      { url: url.trim(), name: name.trim() || undefined },
      {
        onSuccess: (st) => {
          setUrl('')
          setName('')
          audio.playItem(st)
        },
      },
    )
  }
  return (
    <section className="space-y-3">
      <h2 className="flex items-center gap-2 text-lg font-semibold">
        <Link2 className="size-5 text-accent" />
        {t('common:radio.byAddress')}
      </h2>
      <p className="text-sm text-muted">{t('common:radio.byAddressHint')}</p>
      <form onSubmit={submit} className="grid gap-2 md:grid-cols-[2fr_1fr_auto]">
        <input className={inputClass} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/stream.mp3" inputMode="url" aria-label={t('common:radio.streamAddress')} />
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder={t('common:radio.nameOptional')} aria-label={t('common:radio.nameOptional')} />
        <Button variant="primary" disabled={!url.trim() || add.isPending}>
          {add.isPending ? <Spinner className="size-4" /> : t('common:radio.add')}
        </Button>
      </form>
      {add.error && <p className="text-sm text-danger">{add.error.message}</p>}
    </section>
  )
}
