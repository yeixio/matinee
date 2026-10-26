import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clapperboard, PlugZap, Tv } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { api, type ArrApp, type ConnectionCheck, type Integration, type RequestMode } from '../api'
import { Button, Field, inputClass } from '../components/ui'

const apps: {
  app: ArrApp
  name: string
  port: number
  icon: typeof Tv
  what: string
}[] = [
  {
    app: 'radarr',
    name: 'Radarr',
    port: 7878,
    icon: Clapperboard,
    what: 'movies',
  },
  { app: 'sonarr', name: 'Sonarr', port: 8989, icon: Tv, what: 'shows' },
]

// Admins connect Radarr and Sonarr here. The address and key are kept on
// the server; this page only learns whether a key is saved.
export function Integrations() {
  const { data } = useQuery({
    queryKey: ['integrations'],
    queryFn: api.integrations,
  })
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Radarr and Sonarr</h2>
        <p className="mt-1 text-sm text-muted">
          Connect them so people can ask for movies and shows, see what's coming soon, and report problems with what they watch.
        </p>
      </div>
      <div className="space-y-3">{apps.map((a) => (data ? <Connection key={a.app} {...a} saved={data[a.app]} /> : null))}</div>
      {data && (data.radarr.connected || data.sonarr.connected) && <RequestModeSetting />}
    </section>
  )
}

function Connection({ app, name, port, icon: Icon, what, saved }: (typeof apps)[number] & { saved: Integration }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState(saved.url)
  const [apiKey, setApiKey] = useState('')
  const [profile, setProfile] = useState(saved.qualityProfileId)
  const [root, setRoot] = useState(saved.rootFolder)
  const [checked, setChecked] = useState<ConnectionCheck>()
  // A new address needs the key typed again; the saved key only goes to the saved address.
  const keyNeeded = !saved.hasApiKey || url.trim().replace(/\/+$/, '') !== saved.url

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['integrations'] })
    qc.invalidateQueries({ queryKey: ['requestOptions'] })
    qc.invalidateQueries({ queryKey: ['upcoming'] })
  }
  const test = useMutation({
    mutationFn: () => api.testIntegration(app, { url, apiKey: apiKey || undefined }),
    onSuccess: (c) => {
      setChecked(c)
      setUrl(c.url)
      if (!c.qualityProfiles.some((p) => p.id === profile)) setProfile(c.qualityProfiles[0]?.id ?? 0)
      if (!c.rootFolders.some((f) => f.path === root)) setRoot(c.rootFolders[0]?.path ?? '')
    },
  })
  const save = useMutation({
    mutationFn: () =>
      api.saveIntegration(app, {
        url,
        apiKey: apiKey || undefined,
        qualityProfileId: profile,
        rootFolder: root,
      }),
    onSuccess: (s) => {
      setApiKey('')
      setUrl(s.url)
      setOpen(false)
      refresh()
    },
  })
  const disconnect = useMutation({
    mutationFn: () => api.deleteIntegration(app),
    onSuccess: () => {
      setUrl('')
      setChecked(undefined)
      refresh()
    },
  })

  // Opening a saved connection lists its profiles and folders to choose from.
  const { mutate: runTest } = test
  useEffect(() => {
    if (open && saved.connected) runTest()
  }, [open, saved.connected, runTest])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }
  const error = save.error ?? test.error ?? disconnect.error

  return (
    <div className="rounded-xl border border-line/60 bg-surface/50">
      <div className="flex items-center gap-4 p-4">
        <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="font-medium">{name}</div>
          <div className="truncate text-xs text-muted">{saved.connected ? `Connected · ${saved.url}` : `Not connected · for ${what}`}</div>
        </div>
        {!open && (
          <Button
            variant={saved.connected ? 'ghost' : 'secondary'}
            icon={saved.connected ? undefined : <PlugZap className="size-4" />}
            onClick={() => setOpen(true)}
          >
            {saved.connected ? 'Change' : 'Connect'}
          </Button>
        )}
      </div>
      {open && (
        <form onSubmit={submit} className="space-y-4 border-t border-line/60 p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Address">
              <input
                className={inputClass}
                value={url}
                onChange={(e) => {
                  setUrl(e.target.value)
                  setChecked(undefined)
                }}
                placeholder={`http://${app}:${port}`}
                required
              />
            </Field>
            <Field label="API key">
              <input
                className={inputClass}
                type="password"
                autoComplete="off"
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value)
                  setChecked(undefined)
                }}
                placeholder={keyNeeded ? `${name} → Settings → General` : 'Saved'}
                required={keyNeeded}
              />
            </Field>
          </div>
          {checked && (
            <>
              <p className="flex items-start gap-2 text-sm text-accent">
                <Check className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 break-all">
                  {checked.appName} {checked.version} answers at {checked.url}
                </span>
              </p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={`Quality for new ${what}`}>
                  <select className={inputClass} value={profile} onChange={(e) => setProfile(Number(e.target.value))}>
                    {checked.qualityProfiles.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Folder">
                  <select className={inputClass} value={root} onChange={(e) => setRoot(e.target.value)}>
                    {checked.rootFolders.map((f) => (
                      <option key={f.path} value={f.path}>
                        {f.path}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
            </>
          )}
          {error && <p className="text-sm text-danger">{error.message}</p>}
          <div className="flex flex-wrap justify-end gap-2">
            {saved.connected && (
              <Button
                type="button"
                variant="danger"
                className="mr-auto"
                disabled={disconnect.isPending}
                onClick={() => confirm(`Disconnect ${name}? Titles already in it stay there.`) && disconnect.mutate()}
              >
                Disconnect
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="button" disabled={!url || (keyNeeded && !apiKey) || test.isPending} onClick={() => test.mutate()}>
              {test.isPending ? 'Testing…' : 'Test'}
            </Button>
            <Button variant="primary" disabled={!checked || save.isPending}>
              Save
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}

const modes: { value: RequestMode; label: string; detail: string }[] = [
  {
    value: 'approve',
    label: 'Ask, then an admin approves',
    detail: 'Anyone with an account can ask for a title. It waits for an admin.',
  },
  {
    value: 'auto',
    label: 'Add right away',
    detail: 'What people ask for goes straight to Radarr or Sonarr.',
  },
  {
    value: 'off',
    label: 'Only admins add titles',
    detail: 'Everyone else can still see what’s coming soon and report problems.',
  },
]

function RequestModeSetting() {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => qc.setQueryData(['settings'], s),
  })
  return (
    <fieldset className="space-y-2 pt-2">
      <legend className="mb-2 text-sm font-medium">Who can add movies and shows</legend>
      {modes.map((m) => (
        <label key={m.value} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
          <input
            type="radio"
            name="requests"
            className="mt-1 accent-[var(--color-accent)]"
            checked={data?.requests === m.value}
            disabled={!data || save.isPending}
            onChange={() => save.mutate({ requests: m.value })}
          />
          <span>
            <span className="block text-sm font-medium">{m.label}</span>
            <span className="block text-sm text-muted">{m.detail}</span>
          </span>
        </label>
      ))}
    </fieldset>
  )
}
