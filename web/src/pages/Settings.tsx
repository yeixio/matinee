import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Folder, FolderOpen, Gauge, Globe, Image as ImageIcon, KeyRound, LibraryBig, Monitor, Plus, Puzzle, RefreshCw, Trash2, UserPlus, UserRound, UsersRound, X, type LucideIcon } from 'lucide-react'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { Link, Navigate, useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, type ConnectStatus, type LibraryKind, type PlaybackPrefs, type RemoteStatus, type User } from '../api'
import { LanguagePicker } from '../components/LanguagePicker'
import { NewFiles, watchSummary } from '../components/LibraryWatch'
import { MediaAnalysis } from '../components/MediaAnalysis'
import { Plugins } from '../components/PluginSettings'
import { Profiles } from '../components/ProfileSettings'
import { Appearance } from '../components/ThemePicker'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, Field, inputClass } from '../components/ui'
import { Integrations } from './Integrations'
import { ImportSettings } from '../components/ImportSettings'
import i18n, { formatLocale } from '../i18n'
import { DeviceIcon } from './Link'

// Settings, in groups: everyone's own settings first, then the server's,
// one group per page. Desktop lists the groups down the side; a phone opens
// on the list and each group is its own page, as in the phone app.
type SettingsGroup = { id: string; icon: LucideIcon; sections: [string, ReactNode][] }

/** Which group each section lives in, so /settings#<section> links still land. */
export const settingsSections: Record<string, string> = {
  language: 'you', appearance: 'you', playback: 'you', account: 'you', devices: 'you',
  libraries: 'libraries', import: 'libraries', 'new-files': 'libraries', 'media-analysis': 'libraries',
  people: 'people', profiles: 'people',
  posters: 'content', 'podcast-index': 'content', subtitles: 'content', trailers: 'content', 'home-rows': 'content',
  transcoding: 'streaming',
  'home-network': 'network', 'remote-access': 'network',
  arr: 'add-ons', overseerr: 'add-ons', plugins: 'add-ons',
}

export default function Settings() {
  const { t } = useTranslation()
  const { group: groupId } = useParams()
  const { hash } = useLocation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  useScrollToHash(hash)
  if (!me) return null

  const admin = me.isAdmin
  const groups: SettingsGroup[] = [
    {
      id: 'you',
      icon: UserRound,
      sections: [
        ['language', <LanguagePicker me={me} />],
        ['appearance', <Appearance me={me} />],
        ['playback', <Playback />],
        ['account', !me.guest && <Account />],
        ['devices', <Devices />],
      ],
    },
    ...(admin
      ? [
          { id: 'libraries', icon: LibraryBig, sections: [['libraries', <Libraries />], ['import', <ImportSettings />], ['new-files', <NewFiles />], ['media-analysis', <MediaAnalysis />]] },
          { id: 'people', icon: UsersRound, sections: [['people', <Users me={me} />], ['profiles', <Profiles />]] },
          { id: 'content', icon: ImageIcon, sections: [['posters', <TMDBKey />], ['podcast-index', <PodcastIndexKey />], ['subtitles', <OpenSubtitlesKey />], ['trailers', <Trailers />], ['home-rows', <HolidayRows />]] },
          { id: 'streaming', icon: Gauge, sections: [['transcoding', <Transcoding />]] },
          { id: 'network', icon: Globe, sections: [['home-network', <HomeNetwork />], ['remote-access', <RemoteAccess />]] },
          { id: 'add-ons', icon: Puzzle, sections: [['arr', <Integrations />], ['plugins', <Plugins />]] },
        ] satisfies SettingsGroup[]
      : []),
  ]

  // An old link to a section (/settings#podcast-index) goes to its group.
  const wantGroup = settingsSections[hash.slice(1)]
  if (wantGroup && wantGroup !== (groupId ?? 'you') && groups.some((g) => g.id === wantGroup)) {
    return <Navigate replace to={`/settings/${wantGroup}${hash}`} />
  }
  const group = groups.find((g) => g.id === (groupId ?? groups[0].id))
  if (!group) return <Navigate replace to="/settings" />
  const single = groups.length === 1
  // On a phone, /settings is the list of groups; desktop shows the first group beside it.
  const index = !groupId && !single

  const footer = (
    <div className="space-y-3">
      {/* TMDB's API terms ask apps that use it for this notice. */}
      <p className="text-xs text-faint">
        <Trans
          i18nKey="settings.tmdbNotice"
          components={{ site: <a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer" className="underline hover:text-muted" /> }}
        />
      </p>
      <ServerVersion />
    </div>
  )
  return (
    <div className="max-w-5xl">
      <h1 className={`text-2xl font-bold tracking-tight ${groupId ? 'hidden md:block' : ''}`}>{t('nav.settings')}</h1>
      {groupId && (
        <div className="md:hidden">
          <Link to="/settings" className="-ms-1 inline-flex items-center gap-1 text-sm text-muted hover:text-text">
            <ChevronLeft className="size-4 rtl:-scale-x-100" /> {t('nav.settings')}
          </Link>
          <h1 className="mt-2 text-2xl font-bold tracking-tight">{t(`settings.groups.${group.id}.title`)}</h1>
        </div>
      )}
      <div className={`mt-8 ${single ? '' : 'md:grid md:grid-cols-[14rem_minmax(0,1fr)] md:gap-12'}`}>
        {!single && (
          <nav aria-label={t('nav.settings')} className={`${index ? '' : 'hidden'} md:block`}>
            <ul className="space-y-2.5 md:sticky md:top-24 md:space-y-1">
              {groups.map((g) => (
                <li key={g.id}>
                  <Link
                    to={`/settings/${g.id}`}
                    aria-current={g.id === group.id ? 'page' : undefined}
                    className={`flex items-center gap-3 rounded-xl border border-line/60 bg-surface/50 px-4 py-3.5 font-medium md:border-0 md:px-3 md:py-2 md:text-sm ${
                      g.id === group.id ? 'md:bg-text/10 md:text-text' : 'md:bg-transparent md:text-muted md:hover:bg-text/5 md:hover:text-text'
                    }`}
                  >
                    <g.icon className="size-5 shrink-0 text-muted md:size-4" />
                    <span className="min-w-0 flex-1">
                      {t(`settings.groups.${g.id}.title`)}
                      <span className="block text-xs font-normal text-faint md:hidden">{t(`settings.groups.${g.id}.hint`)}</span>
                    </span>
                    <ChevronRight className="size-4 text-faint md:hidden rtl:-scale-x-100" />
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        )}
        <div className={`max-w-3xl space-y-12 ${index ? 'hidden md:block' : ''}`}>
          {group.sections
            .filter(([, el]) => el)
            .map(([id, el]) => (
              <div key={id} id={id} className="scroll-mt-24">
                {el}
              </div>
            ))}
          {footer}
        </div>
        {index && <div className="mt-12 md:hidden">{footer}</div>}
      </div>
    </div>
  )
}

/** Scrolls to the section a link names once it's on the page. */
function useScrollToHash(hash: string) {
  useEffect(() => {
    if (!hash) return
    let tries = 0
    const timer = setInterval(() => {
      const el = document.getElementById(decodeURIComponent(hash.slice(1)))
      if (el || ++tries > 20) clearInterval(timer)
      el?.scrollIntoView()
    }, 100)
    return () => clearInterval(timer)
  }, [hash])
}

function Libraries() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: libraries = [] } = useLibraries()
  const { data: settings } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const [adding, setAdding] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: ['libraries'] })
  const scan = useMutation({ mutationFn: api.scanLibrary, onSuccess: () => setTimeout(refresh, 300) })
  const [findingArt, setFindingArt] = useState<number[]>([])
  const findArt = useMutation({
    mutationFn: api.findArtwork,
    onSuccess: (_, id) => {
      setFindingArt((ids) => [...ids, id])
      setTimeout(refresh, 300)
    },
  })
  const remove = useMutation({
    mutationFn: api.deleteLibrary,
    onSuccess: () => {
      refresh()
      qc.invalidateQueries({ queryKey: ['home'] })
    },
  })

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">{t('settings.libraries.title')}</h2>
        {!adding && (
          <Button icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
            {t('home.addLibrary')}
          </Button>
        )}
      </div>
      {adding && <AddLibrary onDone={() => setAdding(false)} />}
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {libraries.length === 0 && !adding && <p className="p-5 text-sm text-muted">{t('settings.libraries.none')}</p>}
        {libraries.map((l) => (
          <div key={l.id} className="flex items-center gap-4 p-4">
            <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
              <LibraryIcon kind={l.kind} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-medium">{l.name}</div>
              <div className="truncate text-xs text-muted">{l.kind === 'podcasts' ? t('settings.libraries.podcastsChecked') : l.paths.join(' · ')}</div>
              {l.watch.length > 0 && <div className="truncate text-xs text-faint">{watchSummary(l, settings?.watchPollSeconds || 30)}</div>}
              {findingArt.includes(l.id) && <div className="text-xs text-accent">{t('settings.libraries.findingArtwork', { name: l.name })}</div>}
            </div>
            {(l.kind === 'music' || l.kind === 'audiobooks') && (
              <Button
                variant="ghost"
                icon={<ImageIcon className="size-4" />}
                aria-label={t('settings.libraries.findArtwork')}
                title={t('settings.libraries.findArtwork')}
                disabled={l.scanning}
                onClick={() => findArt.mutate(l.id)}
              >
                <span className="hidden md:inline">{t('settings.libraries.findArtwork')}</span>
              </Button>
            )}
            <Button
              variant="ghost"
              icon={<RefreshCw className={`size-4 ${l.scanning ? 'animate-spin' : ''}`} />}
              disabled={l.scanning}
              onClick={() => scan.mutate(l.id)}
            >
              <span className="hidden sm:inline">{l.scanning ? t('nav.scanning') : t('settings.libraries.scan')}</span>
            </Button>
            <Button
              variant="ghost"
              icon={<Trash2 className="size-4" />}
              aria-label={t('settings.libraries.remove', { name: l.name })}
              onClick={() => confirm(t('settings.libraries.confirmRemove', { name: l.name })) && remove.mutate(l.id)}
            />
          </div>
        ))}
      </div>
    </section>
  )
}

const builtInKinds: LibraryKind[] = [{ name: 'movies' }, { name: 'tv' }, { name: 'music' }, { name: 'audiobooks' }, { name: 'podcasts' }]

function AddLibrary({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  // Plugins can add kinds of their own, with their own labels.
  const { data: fromServer } = useQuery({ queryKey: ['libraryKinds'], queryFn: api.libraryKinds, retry: false })
  const kinds = [...builtInKinds, ...(fromServer ?? []).filter((k) => k.plugin)]
  const [kind, setKind] = useState('movies')
  const kindName = (name: string) => kinds.find((k) => k.name === name)?.label ?? i18n.t(`settings.libraries.kind.${name}`)
  const description = kinds.find((k) => k.name === kind)?.description
  const [name, setName] = useState('')
  const [paths, setPaths] = useState<string[]>([])
  const create = useMutation({
    mutationFn: () => api.createLibrary(name || kindName(kind), kind, paths),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['libraries'] })
      onDone()
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5 rounded-xl border border-accent/30 bg-surface p-5">
      <Field label={t('settings.libraries.type')}>
        <div className="grid grid-cols-3 gap-2">
          {kinds.map(({ name: k }) => (
            <button
              type="button"
              key={k}
              onClick={() => setKind(k)}
              className={`flex h-16 flex-col items-center justify-center gap-1 rounded-lg border text-sm transition-colors ${
                kind === k ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              <LibraryIcon kind={k} />
              {kindName(k)}
            </button>
          ))}
        </div>
        {description && <span className="block text-sm text-muted">{description}</span>}
      </Field>
      <Field label={t('profileSettings.name')}>
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder={kindName(kind)} />
      </Field>
      {/* Podcasts come from their feeds, not from folders. */}
      {kind === 'podcasts' ? (
        <p className="text-sm text-muted">{t('settings.libraries.podcastsNoFolders')}</p>
      ) : (
      <Field label={t('settings.libraries.folders')}>
        {/* The picker stays put and chosen folders appear below it, so nothing new lands
            under the finger after a tap (a quick second tap used to hit a chip's remove button). */}
        <div className="space-y-2">
          <FolderPicker picked={paths} onPick={(p) => setPaths((prev) => (prev.includes(p) ? prev : [...prev, p]))} />
          {paths.map((p) => (
            <div key={p} className="flex items-center gap-2 rounded-lg bg-text/5 px-3 py-2 text-sm">
              <Folder className="size-4 text-accent" />
              <span className="flex-1 truncate">{p}</span>
              <button type="button" onClick={() => setPaths(paths.filter((x) => x !== p))} aria-label={t('filters.remove', { label: p })}>
                <X className="size-4 text-muted hover:text-text" />
              </button>
            </div>
          ))}
        </div>
      </Field>
      )}
      {create.error && <p className="text-sm text-danger">{create.error.message}</p>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onDone}>
          {t('cancel')}
        </Button>
        <Button variant="primary" disabled={(kind !== 'podcasts' && paths.length === 0) || create.isPending}>
          {t('home.addLibrary')}
        </Button>
      </div>
    </form>
  )
}

// Browse the server's folders, since the media lives on the server, not this device.
export function FolderPicker({ picked, onPick }: { picked: string[]; onPick: (path: string) => void }) {
  // Empty asks the server for its media root (MATINEE_MEDIA_ROOT), so the picker opens on the media.
  const { t } = useTranslation()
  const [path, setPath] = useState('')
  const [typed, setTyped] = useState('')
  const { data, error } = useQuery({ queryKey: ['fs', path], queryFn: () => api.browse(path) })
  // The server sends each subfolder's full path, written its own way (C:\Movies on Windows).
  const join = (i: number, dir: string) => {
    if (data?.paths?.[i]) return data.paths[i]
    const base = data?.path ?? path
    return base.endsWith('/') ? base + dir : `${base}/${dir}`
  }
  // A Windows server's list of drives isn't a folder to use.
  const usable = data && !data.drives
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <div className="flex items-center gap-2 border-b border-line bg-text/5 px-2 py-2">
        <button
          type="button"
          disabled={!data || data.path === data.parent}
          onClick={() => data && setPath(data.parent)}
          className="rounded p-1 text-muted hover:text-text disabled:opacity-30"
          aria-label={t('settings.libraries.up')}
        >
          <ChevronLeft className="size-4 rtl:-scale-x-100" />
        </button>
        <span className="flex-1 truncate font-mono text-xs text-muted" dir="ltr">
          {data?.drives ? t('settings.libraries.drives') : (data?.path ?? path)}
        </span>
        <Button
          type="button"
          variant="primary"
          className="h-8 px-3 text-xs"
          disabled={!usable || picked.includes(data.path)}
          onClick={() => usable && onPick(data.path)}
        >
          {usable && picked.includes(data.path) ? t('settings.libraries.added') : t('settings.libraries.useFolder')}
        </Button>
      </div>
      {/* Network shares that aren't mapped to a drive (\\nas\media) can't be browsed to, only typed. */}
      <div className="flex items-center gap-2 border-b border-line px-2 py-2">
        <input
          className="h-8 flex-1 rounded-md border border-line bg-transparent px-2 font-mono text-xs"
          dir="ltr"
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              if (typed.trim()) setPath(typed.trim())
            }
          }}
          placeholder={t('settings.libraries.goTo')}
          aria-label={t('settings.libraries.goTo')}
        />
        <Button type="button" variant="ghost" className="h-8 px-3 text-xs" disabled={!typed.trim()} onClick={() => setPath(typed.trim())}>
          {t('settings.libraries.go')}
        </Button>
      </div>
      <div className="max-h-60 overflow-y-auto">
        {error && <p className="p-3 text-sm text-danger">{error.message}</p>}
        {data?.dirs.length === 0 && <p className="p-3 text-sm text-faint">{t('settings.libraries.noSubfolders')}</p>}
        {data?.dirs.map((d, i) => (
          <button
            type="button"
            key={d}
            onClick={() => setPath(join(i, d))}
            className="flex w-full items-center gap-2 px-3 py-2 text-start text-sm hover:bg-text/5"
          >
            <FolderOpen className="size-4 text-faint" /> {d}
          </button>
        ))}
      </div>
    </div>
  )
}

function Users({ me }: { me: User }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: users = [] } = useQuery({ queryKey: ['users'], queryFn: api.users })
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const create = useMutation({
    mutationFn: () => api.createUser(username, password, isAdmin),
    onSuccess: () => {
      setUsername('')
      setPassword('')
      setIsAdmin(false)
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.people.title')}</h2>
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {users.map((u) => (
          <Person key={u.id} user={u} isMe={u.id === me.id} />
        ))}
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
        className="grid gap-3 sm:grid-cols-[1fr_1fr_auto_auto] sm:items-end"
      >
        <Field label={t('auth.username')}>
          <input className={inputClass} value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label={t('auth.password')}>
          <input className={inputClass} type="password" minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required />
        </Field>
        <label className="flex h-11 items-center gap-2 text-sm text-muted">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} className="accent-accent" /> {t('settings.people.admin')}
        </label>
        <Button variant="primary" icon={<UserPlus className="size-4" />} disabled={create.isPending}>
          {t('requests.add')}
        </Button>
      </form>
      {create.error && <p className="text-sm text-danger">{create.error.message}</p>}
    </section>
  )
}

// Person's actions sit in a row of three on phones.
const actionClass = 'h-9 whitespace-nowrap px-2 text-xs sm:px-3 sm:text-sm'

// Person is one account in the admin's list, with what an admin can do to it.
// Admins change their own password under Your account, and can't remove
// themselves or take away their own admin, so another admin is always left.
function Person({ user, isMe }: { user: User; isMe: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [mode, setMode] = useState<'idle' | 'reset' | 'remove'>('idle')
  const [password, setPassword] = useState('')
  const [done, setDone] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['users'] })
  const close = () => {
    setMode('idle')
    setPassword('')
    reset.reset()
    remove.reset()
  }
  const reset = useMutation({
    mutationFn: () => api.resetPassword(user.id, password),
    onSuccess: () => {
      close()
      setDone(t('settings.people.passwordChanged', { name: user.username }))
    },
  })
  const remove = useMutation({ mutationFn: () => api.deleteUser(user.id), onSuccess: refresh })
  const admin = useMutation({
    mutationFn: () => api.setAdmin(user.id, !user.isAdmin),
    onSuccess: () => {
      setDone('')
      refresh()
    },
  })
  const error = reset.error ?? remove.error ?? admin.error
  return (
    <div className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className="grid size-9 shrink-0 place-items-center rounded-full bg-text/10 text-sm font-semibold uppercase">{user.username[0]}</span>
        <span className="min-w-0 flex-1 truncate">
          {user.username}
          {isMe && <span className="text-muted"> {t('settings.people.you')}</span>}
        </span>
        {user.isAdmin && <span className="rounded bg-accent/15 px-2 py-0.5 text-xs text-accent">{t('settings.people.admin')}</span>}
        {!isMe && mode === 'idle' && (
          <div className="grid w-full grid-cols-3 gap-1 sm:flex sm:w-auto">
            <Button variant="ghost" className={actionClass} disabled={admin.isPending} onClick={() => admin.mutate()}>
              {user.isAdmin ? t('settings.people.removeAdmin') : t('settings.people.makeAdmin')}
            </Button>
            <Button
              variant="ghost"
              className={actionClass}
              icon={<KeyRound className="hidden size-4 sm:block" />}
              onClick={() => (setDone(''), setMode('reset'))}
            >
              {t('settings.people.resetPassword')}
            </Button>
            <Button
              variant="ghost"
              className={`${actionClass} hover:text-danger`}
              icon={<Trash2 className="hidden size-4 sm:block" />}
              onClick={() => (setDone(''), setMode('remove'))}
            >
              {t('settings.people.remove')}
            </Button>
          </div>
        )}
      </div>
      {mode === 'reset' && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            reset.mutate()
          }}
          className="grid gap-3 sm:grid-cols-[1fr_auto_auto] sm:items-end"
        >
          <Field label={t('settings.people.newPasswordFor', { name: user.username })}>
            <input
              className={inputClass}
              type="password"
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              autoFocus
              required
            />
          </Field>
          <Button type="button" variant="ghost" onClick={close}>
            {t('cancel')}
          </Button>
          <Button variant="primary" disabled={reset.isPending}>
            {t('settings.people.setPassword')}
          </Button>
        </form>
      )}
      {mode === 'remove' && (
        <div className="space-y-3 rounded-lg border border-danger/40 bg-danger/5 p-3">
          <p className="text-sm">
            <Trans i18nKey="settings.people.confirmRemove" values={{ name: user.username }} components={{ name: <strong /> }} />
          </p>
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="ghost" onClick={close}>
              {t('cancel')}
            </Button>
            <Button variant="danger" icon={<Trash2 className="size-4" />} disabled={remove.isPending} onClick={() => remove.mutate()}>
              {t('profileSettings.remove', { name: user.username })}
            </Button>
          </div>
        </div>
      )}
      {done && <p className="text-sm text-muted">{done}</p>}
      {error && <p className="text-sm text-danger">{error.message}</p>}
    </div>
  )
}

// Account lets the signed-in person change their own password.
function Account() {
  const { t } = useTranslation()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [done, setDone] = useState(false)
  const change = useMutation({
    mutationFn: () => api.changePassword(current, next),
    onSuccess: () => {
      setCurrent('')
      setNext('')
      setAgain('')
      setDone(true)
    },
  })
  const mismatch = again !== '' && again !== next
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.account.title')}</h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!mismatch) change.mutate()
        }}
        className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4"
      >
        <h3 className="font-medium">{t('settings.account.changePassword')}</h3>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label={t('settings.account.current')}>
            <input
              className={inputClass}
              type="password"
              value={current}
              onChange={(e) => (setDone(false), setCurrent(e.target.value))}
              autoComplete="current-password"
              required
            />
          </Field>
          <Field label={t('settings.account.new')}>
            <input
              className={inputClass}
              type="password"
              minLength={8}
              value={next}
              onChange={(e) => (setDone(false), setNext(e.target.value))}
              autoComplete="new-password"
              required
            />
          </Field>
          <Field label={t('settings.account.again')}>
            <input
              className={inputClass}
              type="password"
              minLength={8}
              value={again}
              onChange={(e) => (setDone(false), setAgain(e.target.value))}
              autoComplete="new-password"
              required
            />
          </Field>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs text-muted">{t('settings.account.hint')}</p>
          <Button variant="primary" icon={<KeyRound className="size-4" />} disabled={change.isPending || mismatch}>
            {t('settings.account.changePassword')}
          </Button>
        </div>
        {mismatch && <p className="text-sm text-danger">{t('settings.account.mismatch')}</p>}
        {change.error && <p className="text-sm text-danger">{change.error.message}</p>}
        {done && <p className="text-sm text-muted">{t('settings.account.changed')}</p>}
      </form>
    </section>
  )
}

function lastSeen(unix: number) {
  if (!unix) return i18n.t('settings.devices.aWhileAgo')
  const mins = Math.round((Date.now() / 1000 - unix) / 60)
  if (mins < 10) return i18n.t('settings.devices.justNow')
  const rtf = new Intl.RelativeTimeFormat(formatLocale(), { style: 'short' })
  if (mins < 60) return rtf.format(-mins, 'minute')
  if (mins < 60 * 24) return rtf.format(-Math.round(mins / 60), 'hour')
  return new Date(unix * 1000).toLocaleDateString(formatLocale())
}

function ServerVersion() {
  const { t } = useTranslation()
  const { data } = useQuery({ queryKey: ['server'], queryFn: api.server, staleTime: Infinity })
  if (!data) return null
  return (
    <p className="text-xs text-faint">
      {/* Builds from main are "main-<commit>"; the short commit is enough. */}
      {t('settings.version', { version: data.version.replace(/^main-([0-9a-f]{7})[0-9a-f]+$/, 'main-$1'), api: data.apiVersion })}
    </p>
  )
}

function HomeNetwork() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const required = data?.requireSignInAtHome ?? false
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.home.title')}</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('settings.home.requireSignIn')}</span>
          <span className="mt-1 block text-sm text-muted">
            {required
              ? t('settings.home.required')
              : t('settings.home.open')}
          </span>
        </span>
        <input
          type="checkbox"
          className="mt-1 size-5 accent-[var(--color-accent)]"
          checked={required}
          disabled={!data || save.isPending}
          onChange={(e) => save.mutate({ requireSignInAtHome: e.target.checked })}
        />
      </label>
    </section>
  )
}

function RemoteAccess() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
    // Opening the port and getting a certificate takes a little while, and
    // so does a payment going through.
    refetchInterval: (q) => (q.state.data?.remote?.state === 'working' || q.state.data?.remote?.connect?.state === 'checkout' ? 2000 : false),
  })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  if (!data) return null
  const remote = data.remote
  if (!remote && !data.publicURL) return null
  // A public URL whose name doesn't resolve to a public address only works
  // at home, so remote access is still offered.
  const publicURL = data.publicURLHomeOnly ? '' : data.publicURL
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.remote.title')}</h2>
      {data.publicURL && data.publicURLHomeOnly && (
        <p className="rounded-xl border border-line/60 bg-surface/50 p-4 text-sm text-muted">
          <Trans i18nKey="settings.remote.publicURLHomeOnly" values={{ url: data.publicURL }} components={{ url: <span className="font-medium text-text" dir="ltr" /> }} />
        </p>
      )}
      {publicURL ? (
        <p className="rounded-xl border border-line/60 bg-surface/50 p-4 text-sm text-muted">
          <Trans i18nKey="settings.remote.publicURL" values={{ url: publicURL }} components={{ url: <span className="font-medium text-text" dir="ltr" /> }} />
        </p>
      ) : remote && (
        <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
          <span className="min-w-0 flex-1">
            <span className="block font-medium">{t('settings.remote.toggle')}</span>
            <span className="mt-1 block text-sm text-muted">
              {data.remoteAccess
                ? <RemoteState status={remote} />
                : t('settings.remote.hint')}
            </span>
          </span>
          <input
            type="checkbox"
            className="mt-1 size-5 accent-[var(--color-accent)]"
            checked={data.remoteAccess}
            disabled={save.isPending}
            onChange={(e) => save.mutate({ remoteAccess: e.target.checked })}
          />
        </label>
      )}
      {!publicURL && data.remoteAccess && remote?.connect && <MatineeConnect status={remote.connect} />}
      {!publicURL && remote && data.remoteAccess && (
        <div className="rounded-xl border border-line/60 bg-surface/50 p-4">
          <Field label={t('settings.remote.forward')}>
            <select
              className={inputClass}
              value={data.remoteForwardedPort}
              disabled={save.isPending}
              onChange={(e) => save.mutate({ remoteForwardedPort: Number(e.target.value) })}
            >
              <option value={0}>{t('settings.remote.forwardAuto')}</option>
              <option value={443}>{t('settings.remote.forwardPort', { port: 443 })}</option>
              <option value={80}>{t('settings.remote.forwardPort', { port: 80 })}</option>
            </select>
          </Field>
          <p className="mt-2 text-sm text-muted">
            {t('settings.remote.forwardHint', { port: String(remote?.listenPort || 7443) })}
          </p>
        </div>
      )}
    </section>
  )
}

// Matinee Connect: the paid service that keeps the apps able to find the
// server when the home's address changes. Bought here, paid on yeix.io.
function MatineeConnect({ status }: { status: ConnectStatus }) {
  const { t } = useTranslation()
  const offer = status.state === 'none' || status.state === 'ended'
  const plans = useQuery({ queryKey: ['connectPlans'], queryFn: api.connectPlans, enabled: offer, staleTime: 60 * 60 * 1000, retry: false })
  const [error, setError] = useState('')
  // The page opens in a new tab; it's opened before the request so it isn't
  // blocked as a popup.
  const open = (get: () => Promise<{ url: string }>) => {
    const tab = window.open('', '_blank')
    setError('')
    get()
      .then(({ url }) => {
        if (tab) tab.location.href = url
        else window.location.href = url
      })
      .catch((e: Error) => {
        tab?.close()
        setError(e.message)
      })
  }
  const until = status.until ? new Date(status.until).toLocaleDateString(formatLocale()) : ''
  return (
    <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
      <div>
        <span className="block font-medium">{t('settings.connect.title')}</span>
        <span className="mt-1 block text-sm text-muted">{t('settings.connect.about')}</span>
      </div>
      {status.state === 'active' && (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm">{t('settings.connect.active', { date: until })}</span>
          <Button onClick={() => open(api.connectManage)}>{t('settings.connect.manage')}</Button>
        </div>
      )}
      {status.state === 'checkout' && (
        <p className="text-sm text-muted">
          {t('settings.connect.waiting')}{' '}
          {status.checkout && (
            <a className="text-accent underline" href={status.checkout} target="_blank" rel="noreferrer">
              {t('settings.connect.openCheckout')}
            </a>
          )}
        </p>
      )}
      {offer && (
        <>
          {status.state === 'ended' && <p className="text-sm text-danger">{t('settings.connect.ended')}</p>}
          <div className="flex flex-wrap gap-3">
            {(plans.data ?? []).map((p) => (
              <Button key={p.id} variant={p.id === 'yearly' ? 'primary' : 'secondary'} onClick={() => open(() => api.connectSubscribe(p.id))}>
                {t(p.interval === 'year' ? 'settings.connect.perYear' : 'settings.connect.perMonth', { price: p.price })}
              </Button>
            ))}
          </div>
          {plans.isError && <p className="text-sm text-danger">{(plans.error as Error).message}</p>}
          <p className="text-xs text-faint">{t('settings.connect.paid')}</p>
        </>
      )}
      {(error || status.problem) && <p className="text-sm text-danger">{error || status.problem}</p>}
    </div>
  )
}

function RemoteState({ status }: { status?: RemoteStatus }) {
  const { t } = useTranslation()
  if (!status || status.state === 'working' || status.state === 'off') return <>{t('settings.remote.working')}</>
  if (status.state === 'ready')
    return (
      <>
        <Trans i18nKey="settings.remote.ready" values={{ url: status.url }} components={{ url: <span className="font-medium text-text" dir="ltr" /> }} />
        {status.router && ` ${t('settings.remote.router', { router: status.router })}`} {t('settings.remote.pickUp')}
        {status.findable && ` ${t('settings.remote.findable')}`}
      </>
    )
  return (
    <>
      <span className="block text-danger">{status.problem}</span>{' '}
      {status.sharedAddress ? t('settings.remote.direct') : t('settings.remote.retry')}
    </>
  )
}

// Each person's own auto-play choices, which follow them to every device.
function Playback() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['playback'], queryFn: api.playback })
  const save = useMutation({ mutationFn: api.setPlayback, onSuccess: (p) => qc.setQueryData(['playback'], p) })
  const prefs = data ?? { autoPlay: true, binge: false }
  const toggle = (title: string, body: string, checked: boolean, change: Partial<PlaybackPrefs>, disabled = false) => (
    <label className={`flex items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4 ${disabled ? 'opacity-50' : 'cursor-pointer'}`}>
      <span className="min-w-0 flex-1">
        <span className="block font-medium">{title}</span>
        <span className="mt-1 block text-sm text-muted">{body}</span>
      </span>
      <input
        type="checkbox"
        className="mt-1 size-5 accent-[var(--color-accent)]"
        checked={checked}
        disabled={!data || save.isPending || disabled}
        onChange={() => save.mutate({ ...prefs, ...change })}
      />
    </label>
  )
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.playback.title')}</h2>
      {toggle(
        t('settings.playback.autoPlay'),
        t('settings.playback.autoPlayHint'),
        prefs.autoPlay,
        { autoPlay: !prefs.autoPlay },
      )}
      {toggle(
        t('settings.playback.binge'),
        t('settings.playback.bingeHint'),
        prefs.autoPlay && prefs.binge,
        { binge: !prefs.binge },
        !prefs.autoPlay,
      )}
      {toggle(
        t('settings.playback.skipIntros'),
        t('settings.playback.skipIntrosHint'),
        !!prefs.skipIntros,
        { skipIntros: !prefs.skipIntros },
      )}
    </section>
  )
}

// Where people learn to get a TMDB key.
const tmdbGuide = 'https://matinee.yeix.io/docs/getting-started/posters-and-details/'

function TMDBKey() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const [key, setKey] = useState('')
  const [saved, setSaved] = useState(false)
  const save = useMutation({
    mutationFn: (tmdbKey: string) => api.saveSettings({ tmdbKey }),
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s)
      setKey('')
      setSaved(true)
    },
  })
  const tmdb = data?.tmdb
  const hint = tmdb?.hint ?? '…'
  const using = {
    settings: t('settings.tmdb.usingSaved', { hint }),
    environment: t('settings.tmdb.usingEnvironment', { hint }),
    'built-in': t('settings.tmdb.usingBuiltIn'),
    none: t('settings.tmdb.none'),
  }[tmdb?.source ?? 'none']
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setSaved(false)
    save.mutate(key)
  }
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.tmdb.title')}</h2>
      <p className="text-sm text-muted">
        <Trans
          i18nKey="settings.tmdb.intro"
          components={{ guide: <a href={tmdbGuide} target="_blank" rel="noreferrer" className="text-accent underline" /> }}
        />
      </p>
      <form onSubmit={submit} className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
        <p className="text-sm">{using}</p>
        <Field label={t('settings.tmdb.label')}>
          <input
            type="password"
            autoComplete="off"
            spellCheck={false}
            className={inputClass}
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
        </Field>
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
        {saved && !save.error && <p className="text-sm text-muted">{t('settings.tmdb.saved')}</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" icon={<KeyRound className="size-4" />} disabled={!key.trim() || save.isPending}>
            {t('settings.tmdb.save')}
          </Button>
          {tmdb?.source === 'settings' && (
            <Button type="button" variant="ghost" disabled={save.isPending} onClick={() => save.mutate('')}>
              {t('settings.tmdb.remove')}
            </Button>
          )}
        </div>
      </form>
    </section>
  )
}

// Podcast Index finds shows to add to podcast libraries. Its free key and
// secret come from api.podcastindex.org; they stay on the server.
function PodcastIndexKey() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['podcastIndex'], queryFn: api.podcastIndex })
  const [key, setKey] = useState('')
  const [secret, setSecret] = useState('')
  const save = useMutation({
    mutationFn: () => api.savePodcastIndex(key, secret),
    onSuccess: (v) => {
      qc.setQueryData(['podcastIndex'], v)
      qc.invalidateQueries({ queryKey: ['podcasts', 'directory'] })
      setKey('')
      setSecret('')
    },
  })
  const remove = useMutation({
    mutationFn: api.deletePodcastIndex,
    onSuccess: () => {
      qc.setQueryData(['podcastIndex'], { connected: false })
      qc.invalidateQueries({ queryKey: ['podcasts', 'directory'] })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.podcastIndex.title')}</h2>
      <p className="text-sm text-muted">
        <Trans
          i18nKey="settings.podcastIndex.intro"
          components={{ site: <a href="https://api.podcastindex.org/" target="_blank" rel="noreferrer" className="text-accent underline" /> }}
        />
      </p>
      <form onSubmit={submit} className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
        <p className="text-sm">{data?.connected ? t('settings.podcastIndex.using', { hint: data.hint ?? '…' }) : t('settings.podcastIndex.none')}</p>
        <Field label={t('settings.podcastIndex.key')}>
          <input autoComplete="off" spellCheck={false} className={inputClass} value={key} onChange={(e) => setKey(e.target.value)} />
        </Field>
        <Field label={t('settings.podcastIndex.secret')}>
          <input type="password" autoComplete="off" spellCheck={false} className={inputClass} value={secret} onChange={(e) => setSecret(e.target.value)} />
        </Field>
        {(save.error || remove.error) && <p className="text-sm text-danger">{(save.error ?? remove.error)?.message}</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" variant="primary" icon={<KeyRound className="size-4" />} disabled={!key.trim() || !secret.trim() || save.isPending}>
            {t('settings.podcastIndex.save')}
          </Button>
          {data?.connected && (
            <Button type="button" variant="ghost" disabled={remove.isPending} onClick={() => remove.mutate()}>
              {t('settings.podcastIndex.remove')}
            </Button>
          )}
        </div>
      </form>
    </section>
  )
}

// OpenSubtitles finds subtitles a video doesn't have, from the player's
// subtitles menu. Its free API key comes from opensubtitles.com; a free
// account's username and password let the server fetch more files a day.
// They stay on the server.
function OpenSubtitlesKey() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['openSubtitles'], queryFn: api.openSubtitles })
  const [apiKey, setApiKey] = useState('')
  const [username, setUsername] = useState<string>()
  const [password, setPassword] = useState('')
  const user = username ?? data?.username ?? ''
  const save = useMutation({
    mutationFn: () => api.saveOpenSubtitles({ apiKey: apiKey.trim() || undefined, username: user.trim(), password: password || undefined }),
    onSuccess: (v) => {
      qc.setQueryData(['openSubtitles'], v)
      qc.invalidateQueries({ queryKey: ['item'] })
      setApiKey('')
      setUsername(undefined)
      setPassword('')
    },
  })
  const remove = useMutation({
    mutationFn: api.deleteOpenSubtitles,
    onSuccess: () => {
      qc.setQueryData(['openSubtitles'], { connected: false })
      qc.invalidateQueries({ queryKey: ['item'] })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }
  const changed = !!apiKey.trim() || !!password || (!!data?.connected && user.trim() !== (data.username ?? ''))
  const status = data?.connected
    ? data.username
      ? t('settings.subtitles.usingAccount', { hint: data.hint ?? '…', username: data.username })
      : t('settings.subtitles.using', { hint: data.hint ?? '…' })
    : t('settings.subtitles.none')
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.subtitles.title')}</h2>
      <p className="text-sm text-muted">
        <Trans
          i18nKey="settings.subtitles.intro"
          components={{ site: <a href="https://www.opensubtitles.com/consumers" target="_blank" rel="noreferrer" className="text-accent underline" /> }}
        />
      </p>
      <form onSubmit={submit} className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
        <p className="text-sm">{status}</p>
        <Field label={t('settings.subtitles.key')}>
          <input
            autoComplete="off"
            spellCheck={false}
            className={inputClass}
            value={apiKey}
            placeholder={data?.connected ? t('settings.subtitles.keepKey') : undefined}
            onChange={(e) => setApiKey(e.target.value)}
          />
        </Field>
        <p className="text-xs text-muted">{t('settings.subtitles.accountHint')}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('settings.subtitles.username')}>
            <input autoComplete="off" spellCheck={false} className={inputClass} value={user} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label={t('settings.subtitles.password')}>
            <input
              type="password"
              autoComplete="new-password"
              className={inputClass}
              value={password}
              placeholder={data?.username && user === data.username ? '••••••••' : undefined}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </div>
        {(save.error || remove.error) && <p className="text-sm text-danger">{(save.error ?? remove.error)?.message}</p>}
        <div className="flex flex-wrap gap-2">
          <Button
            type="submit"
            variant="primary"
            icon={<KeyRound className="size-4" />}
            disabled={!(data?.connected ? changed : apiKey.trim()) || save.isPending}
          >
            {t('settings.subtitles.save')}
          </Button>
          {data?.connected && (
            <Button type="button" variant="ghost" disabled={remove.isPending} onClick={() => remove.mutate()}>
              {t('settings.subtitles.remove')}
            </Button>
          )}
        </div>
      </form>
    </section>
  )
}

function Trailers() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const remote = data?.remoteTrailers ?? true
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.trailers.title')}</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('settings.trailers.remote')}</span>
          <span className="mt-1 block text-sm text-muted">
            {remote
              ? t('settings.trailers.remoteOn')
              : t('settings.trailers.remoteOff')}
          </span>
        </span>
        <input
          type="checkbox"
          className="mt-1 size-5 accent-[var(--color-accent)]"
          checked={remote}
          disabled={!data || save.isPending}
          onChange={(e) => {
            save.mutate({ remoteTrailers: e.target.checked }, { onSuccess: () => qc.invalidateQueries({ queryKey: ['trailers'] }) })
          }}
        />
      </label>
    </section>
  )
}

function HolidayRows() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const on = data?.holidayRows ?? true
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.homeRows.title')}</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('settings.homeRows.holidays')}</span>
          <span className="mt-1 block text-sm text-muted">{t('settings.homeRows.holidaysDetail')}</span>
        </span>
        <input
          type="checkbox"
          className="mt-1 size-5 accent-[var(--color-accent)]"
          checked={on}
          disabled={!data || save.isPending}
          onChange={(e) => {
            save.mutate({ holidayRows: e.target.checked }, { onSuccess: () => qc.invalidateQueries({ queryKey: ['home'] }) })
          }}
        />
      </label>
    </section>
  )
}

// Every accelerator the server knows, so an admin can pick one before it's set up.
const accelerators = ['nvenc', 'vaapi', 'qsv', 'videotoolbox', 'amf', 'v4l2m2m']

function Transcoding() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
    // The server tests its GPU for a few seconds after it starts.
    refetchInterval: (q) => (q.state.data?.hardware?.detecting ? 2000 : false),
  })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const hw = data?.hardware
  const active = hw?.active
  const found = new Set(hw?.available.map((a) => a.id))
  let status: string
  if (!hw) status = ''
  else if (active)
    status = [
      active.detail ? t('settings.transcoding.activeOn', { name: active.name, detail: active.detail }) : t('settings.transcoding.active', { name: active.name }),
      active.decode ? t('settings.transcoding.fullGPU') : t('settings.transcoding.encodeOnly'),
      t('settings.transcoding.fallback'),
    ].join(' ')
  else if (hw.detecting) status = t('settings.transcoding.testing')
  else if (data?.hardwareAcceleration === 'off') status = t('settings.transcoding.cpuOff')
  else if (data?.hardwareAcceleration !== 'auto') status = t('settings.transcoding.cpuFailed')
  else status = t('settings.transcoding.cpuNoGPU')
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.transcoding.title')}</h2>
      <p className="text-sm text-muted">
        {t('settings.transcoding.intro')}
      </p>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <div>
          <span className="block text-sm font-medium">{t('settings.transcoding.convertingWith')}</span>
          <span className="mt-1 block text-sm text-muted">{status}</span>
          {!active && hw?.problem && <span className="mt-2 block text-sm text-danger">{hw.problem}</span>}
        </div>
        <Field label={t('settings.transcoding.hardware')}>
          <select
            className={inputClass}
            value={data?.hardwareAcceleration ?? 'auto'}
            disabled={!data || save.isPending}
            onChange={(e) => save.mutate({ hardwareAcceleration: e.target.value })}
          >
            <option value="auto">{t('settings.transcoding.auto')}</option>
            <option value="off">{t('settings.transcoding.off')}</option>
            {accelerators.map((a) => (
              <option key={a} value={a}>
                {hw && !hw.detecting && !found.has(a)
                  ? t('settings.transcoding.notFound', { name: t(`settings.transcoding.accelerator.${a}`) })
                  : t(`settings.transcoding.accelerator.${a}`)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <StreamingQuality />
    </section>
  )
}

// Every quality step the server offers, best first (see internal/stream/quality.go).
const qualitySteps = [
  { id: '1080p', mbps: 8 },
  { id: '720p', mbps: 4 },
  { id: '480p', mbps: 1.5 },
  { id: '360p', mbps: 0.7 },
]

// Adaptive quality, the steps it uses, and the cap away from home.
function StreamingQuality() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s)
      qc.invalidateQueries({ queryKey: ['streaming'] })
    },
  })
  const on = data?.adaptiveQuality ?? false
  const steps = data?.qualitySteps ?? qualitySteps.map((s) => s.id)
  const label = (s: (typeof qualitySteps)[number]) =>
    `${s.id} · ${t('common:quality.mbps', { n: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(s.mbps) })}`
  const toggleStep = (id: string, checked: boolean) => {
    const next = qualitySteps.map((s) => s.id).filter((s) => (s === id ? checked : steps.includes(s)))
    if (next.length) save.mutate({ qualitySteps: next })
  }
  return (
    <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
      <label className="flex cursor-pointer items-start gap-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('settings.transcoding.adaptive')}</span>
          <span className="mt-1 block text-sm text-muted">{t('settings.transcoding.adaptiveDetail')}</span>
        </span>
        <input
          type="checkbox"
          className="mt-1 size-5 accent-[var(--color-accent)]"
          checked={on}
          disabled={!data || save.isPending}
          onChange={(e) => save.mutate({ adaptiveQuality: e.target.checked })}
        />
      </label>
      {on && (
        <fieldset>
          <legend className="text-sm font-medium">{t('settings.transcoding.steps')}</legend>
          <p className="mt-1 text-sm text-muted">{t('settings.transcoding.stepsDetail')}</p>
          <div className="mt-2 flex flex-wrap gap-x-5 gap-y-2">
            {qualitySteps.map((s) => (
              <label key={s.id} className="flex cursor-pointer items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="size-4 accent-[var(--color-accent)]"
                  checked={steps.includes(s.id)}
                  // The last one left stays: an adaptive stream needs at least one.
                  disabled={!data || save.isPending || (steps.length === 1 && steps.includes(s.id))}
                  onChange={(e) => toggleStep(s.id, e.target.checked)}
                />
                {label(s)}
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <Field label={t('settings.transcoding.cap')}>
        <select
          className={inputClass}
          value={data?.remoteQualityCap ?? ''}
          disabled={!data || save.isPending}
          onChange={(e) => save.mutate({ remoteQualityCap: e.target.value })}
        >
          <option value="">{t('settings.transcoding.noCap')}</option>
          {qualitySteps.map((s) => (
            <option key={s.id} value={s.id}>
              {label(s)}
            </option>
          ))}
        </select>
      </Field>
      <p className="-mt-2 text-sm text-muted">{t('settings.transcoding.capDetail')}</p>
    </div>
  )
}

function Devices() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: devices = [] } = useQuery({ queryKey: ['devices'], queryFn: api.devices })
  const navigate = useNavigate()
  const signOut = useMutation({ mutationFn: api.signOutDevice, onSuccess: () => qc.invalidateQueries({ queryKey: ['devices'] }) })
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">{t('settings.devices.title')}</h2>
        <Button onClick={() => navigate('/link')}>{t('link.title')}</Button>
      </div>
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {devices.map((d) => (
          <div key={d.id} className="flex items-center gap-4 p-4">
            <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
              {d.platform === 'web' ? <Monitor className="size-5" /> : <DeviceIcon platform={d.platform} />}
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{d.deviceName || t('settings.devices.unknown')}</div>
              <div className="text-xs text-muted">{d.current ? t('settings.devices.this') : t('settings.devices.lastUsed', { when: lastSeen(d.lastSeenAt) })}</div>
            </div>
            {!d.current && (
              <Button
                variant="ghost"
                disabled={signOut.isPending}
                onClick={() => confirm(t('settings.devices.confirmSignOut', { name: d.deviceName })) && signOut.mutate(d.id)}
              >
                {t('nav.signOut')}
              </Button>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
