import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, Folder, FolderOpen, KeyRound, Monitor, Plus, RefreshCw, Trash2, UserPlus, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type PlaybackPrefs, type RemoteStatus, type User } from '../api'
import { NewFiles, watchSummary } from '../components/LibraryWatch'
import { MediaAnalysis } from '../components/MediaAnalysis'
import { Profiles } from '../components/ProfileSettings'
import { Appearance } from '../components/ThemePicker'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, Field, inputClass } from '../components/ui'
import { Integrations } from './Integrations'
import { DeviceIcon } from './Link'

export default function Settings() {
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  return (
    <div className="max-w-3xl space-y-12">
      <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
      {me && <Appearance me={me} />}
      {me && <Playback />}
      {me?.isAdmin && <Libraries />}
      {me?.isAdmin && <NewFiles />}
      {me?.isAdmin && <MediaAnalysis />}
      {me?.isAdmin && <Users me={me} />}
      {me?.isAdmin && <Profiles />}
      {me?.isAdmin && <HomeNetwork />}
      {me?.isAdmin && <RemoteAccess />}
      {me?.isAdmin && <Trailers />}
      {me?.isAdmin && <HolidayRows />}
      {me?.isAdmin && <Transcoding />}
      {me?.isAdmin && <Integrations />}
      {me && !me.guest && <Account />}
      <Devices />
      {/* TMDB's API terms ask apps that use it for this notice. */}
      <p className="text-xs text-faint">
        Posters and descriptions come from{' '}
        <a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer" className="underline hover:text-muted">
          TMDB
        </a>
        . This product uses the TMDB API but is not endorsed or certified by TMDB.
      </p>
      <ServerVersion />
    </div>
  )
}

function Libraries() {
  const qc = useQueryClient()
  const { data: libraries = [] } = useLibraries()
  const { data: settings } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const [adding, setAdding] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: ['libraries'] })
  const scan = useMutation({ mutationFn: api.scanLibrary, onSuccess: () => setTimeout(refresh, 300) })
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
        <h2 className="text-lg font-semibold">Libraries</h2>
        {!adding && (
          <Button icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
            Add library
          </Button>
        )}
      </div>
      {adding && <AddLibrary onDone={() => setAdding(false)} />}
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {libraries.length === 0 && !adding && <p className="p-5 text-sm text-muted">No libraries yet.</p>}
        {libraries.map((l) => (
          <div key={l.id} className="flex items-center gap-4 p-4">
            <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
              <LibraryIcon kind={l.kind} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-medium">{l.name}</div>
              <div className="truncate text-xs text-muted">{l.paths.join(' · ')}</div>
              {l.watch.length > 0 && <div className="truncate text-xs text-faint">{watchSummary(l, settings?.watchPollSeconds || 30)}</div>}
            </div>
            <Button
              variant="ghost"
              icon={<RefreshCw className={`size-4 ${l.scanning ? 'animate-spin' : ''}`} />}
              disabled={l.scanning}
              onClick={() => scan.mutate(l.id)}
            >
              <span className="hidden sm:inline">{l.scanning ? 'Scanning' : 'Scan'}</span>
            </Button>
            <Button
              variant="ghost"
              icon={<Trash2 className="size-4" />}
              aria-label={`Remove ${l.name}`}
              onClick={() => confirm(`Remove "${l.name}"? Your files stay where they are.`) && remove.mutate(l.id)}
            />
          </div>
        ))}
      </div>
    </section>
  )
}

const kinds = [
  { value: 'movies', label: 'Movies' },
  { value: 'tv', label: 'TV shows' },
  { value: 'music', label: 'Music' },
]

function AddLibrary({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient()
  const [kind, setKind] = useState('movies')
  const [name, setName] = useState('')
  const [paths, setPaths] = useState<string[]>([])
  const create = useMutation({
    mutationFn: () => api.createLibrary(name || kinds.find((k) => k.value === kind)!.label, kind, paths),
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
      <Field label="Type">
        <div className="grid grid-cols-3 gap-2">
          {kinds.map((k) => (
            <button
              type="button"
              key={k.value}
              onClick={() => setKind(k.value)}
              className={`flex h-16 flex-col items-center justify-center gap-1 rounded-lg border text-sm transition-colors ${
                kind === k.value ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              <LibraryIcon kind={k.value} />
              {k.label}
            </button>
          ))}
        </div>
      </Field>
      <Field label="Name">
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder={kinds.find((k) => k.value === kind)!.label} />
      </Field>
      <Field label="Folders">
        {/* The picker stays put and chosen folders appear below it, so nothing new lands
            under the finger after a tap (a quick second tap used to hit a chip's remove button). */}
        <div className="space-y-2">
          <FolderPicker picked={paths} onPick={(p) => setPaths((prev) => (prev.includes(p) ? prev : [...prev, p]))} />
          {paths.map((p) => (
            <div key={p} className="flex items-center gap-2 rounded-lg bg-text/5 px-3 py-2 text-sm">
              <Folder className="size-4 text-accent" />
              <span className="flex-1 truncate">{p}</span>
              <button type="button" onClick={() => setPaths(paths.filter((x) => x !== p))} aria-label={`Remove ${p}`}>
                <X className="size-4 text-muted hover:text-text" />
              </button>
            </div>
          ))}
        </div>
      </Field>
      {create.error && <p className="text-sm text-danger">{create.error.message}</p>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button variant="primary" disabled={paths.length === 0 || create.isPending}>
          Add library
        </Button>
      </div>
    </form>
  )
}

// Browse the server's folders, since the media lives on the server, not this device.
function FolderPicker({ picked, onPick }: { picked: string[]; onPick: (path: string) => void }) {
  // Empty asks the server for its media root (MATINEE_MEDIA_ROOT), so the picker opens on the media.
  const [path, setPath] = useState('')
  const { data, error } = useQuery({ queryKey: ['fs', path], queryFn: () => api.browse(path) })
  const join = (dir: string) => {
    const base = data?.path ?? path
    return base.endsWith('/') ? base + dir : `${base}/${dir}`
  }
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <div className="flex items-center gap-2 border-b border-line bg-text/5 px-2 py-2">
        <button
          type="button"
          disabled={!data || data.path === data.parent}
          onClick={() => data && setPath(data.parent)}
          className="rounded p-1 text-muted hover:text-text disabled:opacity-30"
          aria-label="Up one folder"
        >
          <ChevronLeft className="size-4" />
        </button>
        <span className="flex-1 truncate font-mono text-xs text-muted">{data?.path ?? path}</span>
        <Button
          type="button"
          variant="primary"
          className="h-8 px-3 text-xs"
          disabled={!data || picked.includes(data.path)}
          onClick={() => data && onPick(data.path)}
        >
          {data && picked.includes(data.path) ? 'Added' : 'Use this folder'}
        </Button>
      </div>
      <div className="max-h-60 overflow-y-auto">
        {error && <p className="p-3 text-sm text-danger">{error.message}</p>}
        {data?.dirs.length === 0 && <p className="p-3 text-sm text-faint">No subfolders</p>}
        {data?.dirs.map((d) => (
          <button
            type="button"
            key={d}
            onClick={() => setPath(join(d))}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-text/5"
          >
            <FolderOpen className="size-4 text-faint" /> {d}
          </button>
        ))}
      </div>
    </div>
  )
}

function Users({ me }: { me: User }) {
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
      <h2 className="text-lg font-semibold">People</h2>
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
        <Field label="Username">
          <input className={inputClass} value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label="Password">
          <input className={inputClass} type="password" minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required />
        </Field>
        <label className="flex h-11 items-center gap-2 text-sm text-muted">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} className="accent-accent" /> Admin
        </label>
        <Button variant="primary" icon={<UserPlus className="size-4" />} disabled={create.isPending}>
          Add
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
      setDone(`Password changed. ${user.username} was signed out everywhere.`)
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
          {isMe && <span className="text-muted"> (you)</span>}
        </span>
        {user.isAdmin && <span className="rounded bg-accent/15 px-2 py-0.5 text-xs text-accent">Admin</span>}
        {!isMe && mode === 'idle' && (
          <div className="grid w-full grid-cols-3 gap-1 sm:flex sm:w-auto">
            <Button variant="ghost" className={actionClass} disabled={admin.isPending} onClick={() => admin.mutate()}>
              {user.isAdmin ? 'Remove admin' : 'Make admin'}
            </Button>
            <Button
              variant="ghost"
              className={actionClass}
              icon={<KeyRound className="hidden size-4 sm:block" />}
              onClick={() => (setDone(''), setMode('reset'))}
            >
              Reset password
            </Button>
            <Button
              variant="ghost"
              className={`${actionClass} hover:text-danger`}
              icon={<Trash2 className="hidden size-4 sm:block" />}
              onClick={() => (setDone(''), setMode('remove'))}
            >
              Remove
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
          <Field label={`New password for ${user.username}`}>
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
            Cancel
          </Button>
          <Button variant="primary" disabled={reset.isPending}>
            Set password
          </Button>
        </form>
      )}
      {mode === 'remove' && (
        <div className="space-y-3 rounded-lg border border-danger/40 bg-danger/5 p-3">
          <p className="text-sm">
            Remove <strong>{user.username}</strong>? Their watch history, watch list, downloads and requests go too, and
            they're signed out everywhere. This can't be undone.
          </p>
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="ghost" onClick={close}>
              Cancel
            </Button>
            <Button variant="danger" icon={<Trash2 className="size-4" />} disabled={remove.isPending} onClick={() => remove.mutate()}>
              Remove {user.username}
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
      <h2 className="text-lg font-semibold">Your account</h2>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!mismatch) change.mutate()
        }}
        className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4"
      >
        <h3 className="font-medium">Change password</h3>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="Current password">
            <input
              className={inputClass}
              type="password"
              value={current}
              onChange={(e) => (setDone(false), setCurrent(e.target.value))}
              autoComplete="current-password"
              required
            />
          </Field>
          <Field label="New password">
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
          <Field label="New password again">
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
          <p className="text-xs text-muted">At least 8 characters. Your other devices will be signed out.</p>
          <Button variant="primary" icon={<KeyRound className="size-4" />} disabled={change.isPending || mismatch}>
            Change password
          </Button>
        </div>
        {mismatch && <p className="text-sm text-danger">The new passwords don't match.</p>}
        {change.error && <p className="text-sm text-danger">{change.error.message}</p>}
        {done && <p className="text-sm text-muted">Password changed. Your other devices were signed out.</p>}
      </form>
    </section>
  )
}

function lastSeen(unix: number) {
  if (!unix) return 'a while ago'
  const mins = Math.round((Date.now() / 1000 - unix) / 60)
  if (mins < 10) return 'just now'
  if (mins < 60) return `${mins} min ago`
  if (mins < 60 * 24) return `${Math.round(mins / 60)} h ago`
  return new Date(unix * 1000).toLocaleDateString()
}

function ServerVersion() {
  const { data } = useQuery({ queryKey: ['server'], queryFn: api.server, staleTime: Infinity })
  if (!data) return null
  return (
    <p className="text-xs text-faint">
      {/* Builds from main are "main-<commit>"; the short commit is enough. */}
      Matinee server {data.version.replace(/^main-([0-9a-f]{7})[0-9a-f]+$/, 'main-$1')} · API v{data.apiVersion}
    </p>
  )
}

function HomeNetwork() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const required = data?.requireSignInAtHome ?? false
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Home network</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">Ask for sign-in at home</span>
          <span className="mt-1 block text-sm text-muted">
            {required
              ? 'Everyone signs in, at home and away.'
              : "Phones, TVs and browsers on your home network can watch without signing in. They share one Home account and can't change anything. Away from home, everyone signs in."}
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
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
    // Opening the port and getting a certificate takes a little while.
    refetchInterval: (q) => (q.state.data?.remote?.state === 'working' ? 2000 : false),
  })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  if (!data) return null
  const remote = data.remote
  if (!remote && !data.publicURL) return null
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Remote access</h2>
      {data.publicURL ? (
        <p className="rounded-xl border border-line/60 bg-surface/50 p-4 text-sm text-muted">
          Away from home, the apps connect to <span className="font-medium text-text">{data.publicURL}</span>, the address this
          server was set up with.
        </p>
      ) : (
        <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
          <span className="min-w-0 flex-1">
            <span className="block font-medium">Watch away from home</span>
            <span className="mt-1 block text-sm text-muted">
              {data.remoteAccess
                ? <RemoteState status={remote} />
                : 'Matinee asks your router to open a port and gets a certificate so the apps connect securely from anywhere. Everyone signs in from outside.'}
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
    </section>
  )
}

function RemoteState({ status }: { status?: RemoteStatus }) {
  if (!status || status.state === 'working' || status.state === 'off') return <>Opening a port on your router and getting a certificate…</>
  if (status.state === 'ready')
    return (
      <>
        The apps reach this server at <span className="font-medium text-text">{status.url}</span> when they’re away from home.
        {status.router && ` Your router opened the port over ${status.router}.`} They pick this address up next time they’re
        on your home network.
      </>
    )
  return (
    <>
      <span className="block text-danger">{status.problem}</span>
      {status.sharedAddress
        ? ' Matinee can’t get around this by itself yet.'
        : ' Matinee tries again every half hour.'}
    </>
  )
}

// Each person's own auto-play choices, which follow them to every device.
function Playback() {
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
      <h2 className="text-lg font-semibold">Playback</h2>
      {toggle(
        'Play the next episode',
        'When the credits start, the next episode plays after a 10-second countdown you can cancel.',
        prefs.autoPlay,
        { autoPlay: !prefs.autoPlay },
      )}
      {toggle(
        'Skip the countdown',
        'Go straight into the next episode when the credits start. After three in a row without touching anything, you’re asked if you’re still watching.',
        prefs.autoPlay && prefs.binge,
        { binge: !prefs.binge },
        !prefs.autoPlay,
      )}
    </section>
  )
}

function Trailers() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const remote = data?.remoteTrailers ?? true
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Trailers</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">Trailers from YouTube</span>
          <span className="mt-1 block text-sm text-muted">
            {remote
              ? 'Movies and shows offer their trailers from TMDB, which play from YouTube. Trailer files next to your media always show too.'
              : 'Only trailer files next to your media show, like "Movie (2020)-trailer.mkv" or anything in a Trailers folder.'}
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
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({ mutationFn: api.saveSettings, onSuccess: (s) => qc.setQueryData(['settings'], s) })
  const on = data?.holidayRows ?? true
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Home</h2>
      <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">Holiday rows</span>
          <span className="mt-1 block text-sm text-muted">
            In the month before Valentine’s Day, Easter, Halloween, Thanksgiving and Christmas, and the week before New
            Year’s Eve, Home leads with a row of titles for the day, picked by their keywords, titles and descriptions.
          </span>
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
const accelerators: { id: string; name: string }[] = [
  { id: 'nvenc', name: 'NVIDIA NVENC' },
  { id: 'vaapi', name: 'VAAPI (AMD and Intel on Linux)' },
  { id: 'qsv', name: 'Intel Quick Sync' },
  { id: 'videotoolbox', name: 'VideoToolbox (Mac)' },
  { id: 'amf', name: 'AMD AMF (Windows)' },
  { id: 'v4l2m2m', name: 'V4L2 (Raspberry Pi)' },
]

function Transcoding() {
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
    status = `${active.name}${active.detail ? ` on ${active.detail}` : ''}. ${
      active.decode ? 'Video is decoded, resized and encoded on the GPU.' : 'Video is encoded on the GPU and decoded on the CPU.'
    } If the GPU can't handle a file, it plays through the CPU instead.`
  else if (hw.detecting) status = 'Testing this server’s GPU…'
  else if (data?.hardwareAcceleration === 'off') status = 'The CPU. Hardware transcoding is off.'
  else if (data?.hardwareAcceleration !== 'auto') status = 'The CPU, because the accelerator you chose didn’t work on this server.'
  else status = 'The CPU. No GPU on this server passed its test.'
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Transcoding</h2>
      <p className="text-sm text-muted">
        Files a device can’t play as they are get converted while you watch. A GPU does that faster and with less load on the
        server.
      </p>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <div>
          <span className="block text-sm font-medium">Converting with</span>
          <span className="mt-1 block text-sm text-muted">{status}</span>
          {!active && hw?.problem && <span className="mt-2 block text-sm text-danger">{hw.problem}</span>}
        </div>
        <Field label="Hardware acceleration">
          <select
            className={inputClass}
            value={data?.hardwareAcceleration ?? 'auto'}
            disabled={!data || save.isPending}
            onChange={(e) => save.mutate({ hardwareAcceleration: e.target.value })}
          >
            <option value="auto">Automatic: the best one that works</option>
            <option value="off">Off: always use the CPU</option>
            {accelerators.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
                {hw && !hw.detecting && !found.has(a.id) ? ' (not found on this server)' : ''}
              </option>
            ))}
          </select>
        </Field>
      </div>
    </section>
  )
}

function Devices() {
  const qc = useQueryClient()
  const { data: devices = [] } = useQuery({ queryKey: ['devices'], queryFn: api.devices })
  const navigate = useNavigate()
  const signOut = useMutation({ mutationFn: api.signOutDevice, onSuccess: () => qc.invalidateQueries({ queryKey: ['devices'] }) })
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Your devices</h2>
        <Button onClick={() => navigate('/link')}>Sign in a device</Button>
      </div>
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {devices.map((d) => (
          <div key={d.id} className="flex items-center gap-4 p-4">
            <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
              {d.platform === 'web' ? <Monitor className="size-5" /> : <DeviceIcon platform={d.platform} />}
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{d.deviceName || 'Unknown device'}</div>
              <div className="text-xs text-muted">{d.current ? 'This device' : `Last used ${lastSeen(d.lastSeenAt)}`}</div>
            </div>
            {!d.current && (
              <Button
                variant="ghost"
                disabled={signOut.isPending}
                onClick={() => confirm(`Sign out "${d.deviceName}"?`) && signOut.mutate(d.id)}
              >
                Sign out
              </Button>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
