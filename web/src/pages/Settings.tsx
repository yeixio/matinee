import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, Folder, FolderOpen, Monitor, Plus, RefreshCw, Trash2, UserPlus, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type User } from '../api'
import { MediaAnalysis } from '../components/MediaAnalysis'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, Field, inputClass } from '../components/ui'
import { Integrations } from './Integrations'
import { DeviceIcon } from './Link'

export default function Settings() {
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  return (
    <div className="max-w-3xl space-y-12">
      <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
      {me?.isAdmin && <Libraries />}
      {me?.isAdmin && <MediaAnalysis />}
      {me?.isAdmin && <Users />}
      {me?.isAdmin && <HomeNetwork />}
      {me?.isAdmin && <Transcoding />}
      {me?.isAdmin && <Integrations />}
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
            <span className="grid size-10 place-items-center rounded-lg bg-white/5 text-accent">
              <LibraryIcon kind={l.kind} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-medium">{l.name}</div>
              <div className="truncate text-xs text-muted">{l.paths.join(' · ')}</div>
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
            <div key={p} className="flex items-center gap-2 rounded-lg bg-white/5 px-3 py-2 text-sm">
              <Folder className="size-4 text-accent" />
              <span className="flex-1 truncate">{p}</span>
              <button type="button" onClick={() => setPaths(paths.filter((x) => x !== p))} aria-label={`Remove ${p}`}>
                <X className="size-4 text-muted hover:text-text" />
              </button>
            </div>
          ))}
        </div>
      </Field>
      {create.error && <p className="text-sm text-red-400">{create.error.message}</p>}
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
      <div className="flex items-center gap-2 border-b border-line bg-white/5 px-2 py-2">
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
        {error && <p className="p-3 text-sm text-red-400">{error.message}</p>}
        {data?.dirs.length === 0 && <p className="p-3 text-sm text-faint">No subfolders</p>}
        {data?.dirs.map((d) => (
          <button
            type="button"
            key={d}
            onClick={() => setPath(join(d))}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-white/5"
          >
            <FolderOpen className="size-4 text-faint" /> {d}
          </button>
        ))}
      </div>
    </div>
  )
}

function Users() {
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
          <div key={u.id} className="flex items-center gap-3 p-4">
            <span className="grid size-9 place-items-center rounded-full bg-white/10 text-sm font-semibold uppercase">{u.username[0]}</span>
            <span className="flex-1">{u.username}</span>
            {u.isAdmin && <span className="rounded bg-accent/15 px-2 py-0.5 text-xs text-accent">Admin</span>}
          </div>
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
      {create.error && <p className="text-sm text-red-400">{create.error.message}</p>}
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
          {!active && hw?.problem && <span className="mt-2 block text-sm text-red-400">{hw.problem}</span>}
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
            <span className="grid size-10 place-items-center rounded-lg bg-white/5 text-accent">
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
