import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronLeft, Folder, FolderOpen, Plus, RefreshCw, Trash2, UserPlus, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { api } from '../api'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, Field, inputClass } from '../components/ui'

export default function Settings() {
  return (
    <div className="max-w-3xl space-y-12">
      <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
      <Libraries />
      <Users />
      {/* TMDB's API terms ask apps that use it for this notice. */}
      <p className="text-xs text-faint">
        Posters and descriptions come from{' '}
        <a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer" className="underline hover:text-muted">
          TMDB
        </a>
        . This product uses the TMDB API but is not endorsed or certified by TMDB.
      </p>
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
