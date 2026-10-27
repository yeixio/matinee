import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clapperboard, Compass, PlugZap, Tv } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ArrApp, type ConnectionCheck, type Integration, type OverseerrCheck, type OverseerrConnection, type RequestMode } from '../api'
import { Button, Field, inputClass } from '../components/ui'

const apps: {
  app: ArrApp
  name: string
  port: number
  icon: typeof Tv
}[] = [
  { app: 'radarr', name: 'Radarr', port: 7878, icon: Clapperboard },
  { app: 'sonarr', name: 'Sonarr', port: 8989, icon: Tv },
]

// Admins connect Radarr and Sonarr here. The address and key are kept on
// the server; this page only learns whether a key is saved.
export function Integrations() {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['integrations'],
    queryFn: api.integrations,
  })
  const { data: overseerr } = useQuery({ queryKey: ['overseerr'], queryFn: api.overseerr })
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('integrations.title')}</h2>
        <p className="mt-1 text-sm text-muted">
          {t('integrations.intro')}
        </p>
      </div>
      <div className="space-y-3">{apps.map((a) => (data ? <Connection key={a.app} {...a} saved={data[a.app]} /> : null))}</div>
      <div className="pt-4">
        <h2 className="text-lg font-semibold">Overseerr</h2>
        <p className="mt-1 text-sm text-muted">{t('integrations.overseerr.intro')}</p>
      </div>
      {overseerr && <OverseerrConnectionCard saved={overseerr} />}
      {((data && (data.radarr.connected || data.sonarr.connected)) || overseerr?.connected) && <RequestModeSetting />}
    </section>
  )
}

function Connection({ app, name, port, icon: Icon, saved }: (typeof apps)[number] & { saved: Integration }) {
  const { t } = useTranslation()
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
          <div className="truncate text-xs text-muted">{saved.connected ? t('integrations.connected', { url: saved.url }) : t(`integrations.notConnected.${app}`)}</div>
        </div>
        {!open && (
          <Button
            variant={saved.connected ? 'ghost' : 'secondary'}
            icon={saved.connected ? undefined : <PlugZap className="size-4" />}
            onClick={() => setOpen(true)}
          >
            {saved.connected ? t('integrations.change') : t('integrations.connect')}
          </Button>
        )}
      </div>
      {open && (
        <form onSubmit={submit} className="space-y-4 border-t border-line/60 p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('integrations.address')}>
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
            <Field label={t('integrations.apiKey')}>
              <input
                className={inputClass}
                type="password"
                autoComplete="off"
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value)
                  setChecked(undefined)
                }}
                placeholder={keyNeeded ? t('integrations.keyWhere', { app: name }) : t('integrations.saved')}
                required={keyNeeded}
              />
            </Field>
          </div>
          {checked && (
            <>
              <p className="flex items-start gap-2 text-sm text-accent">
                <Check className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 break-all">
                  {t('integrations.answers', { app: checked.appName, version: checked.version, url: checked.url })}
                </span>
              </p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={t(`integrations.quality.${app}`)}>
                  <select className={inputClass} value={profile} onChange={(e) => setProfile(Number(e.target.value))}>
                    {checked.qualityProfiles.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label={t('integrations.folder')}>
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
                className="me-auto"
                disabled={disconnect.isPending}
                onClick={() => confirm(t('integrations.confirmDisconnect', { app: name })) && disconnect.mutate()}
              >
                {t('integrations.disconnect')}
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              {t('cancel')}
            </Button>
            <Button type="button" disabled={!url || (keyNeeded && !apiKey) || test.isPending} onClick={() => test.mutate()}>
              {test.isPending ? t('integrations.testing') : t('integrations.test')}
            </Button>
            <Button variant="primary" disabled={!checked || save.isPending}>
              {t('save')}
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}

// Overseerr, for the Discover tab. Requests go to Overseerr as the person's
// own Overseerr account when the names match, else as the user picked here.
function OverseerrConnectionCard({ saved }: { saved: OverseerrConnection }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState(saved.url)
  const [apiKey, setApiKey] = useState('')
  const [requestAs, setRequestAs] = useState(saved.requestAs)
  const [checked, setChecked] = useState<OverseerrCheck>()
  const keyNeeded = !saved.hasApiKey || url.trim().replace(/\/+$/, '') !== saved.url

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['overseerr'] })
    qc.invalidateQueries({ queryKey: ['requestOptions'] })
    qc.invalidateQueries({ queryKey: ['discover'] })
  }
  const test = useMutation({
    mutationFn: () => api.testOverseerr({ url, apiKey: apiKey || undefined }),
    onSuccess: (c) => {
      setChecked(c)
      setUrl(c.url)
      if (requestAs && !c.users.some((u) => u.id === requestAs)) setRequestAs(0)
    },
  })
  const save = useMutation({
    mutationFn: () => api.saveOverseerr({ url, apiKey: apiKey || undefined, requestAs }),
    onSuccess: (s) => {
      setApiKey('')
      setUrl(s.url)
      setOpen(false)
      refresh()
    },
  })
  const disconnect = useMutation({
    mutationFn: api.deleteOverseerr,
    onSuccess: () => {
      setUrl('')
      setChecked(undefined)
      refresh()
    },
  })

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
          <Compass className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="font-medium">Overseerr</div>
          <div className="truncate text-xs text-muted">{saved.connected ? t('integrations.connected', { url: saved.url }) : t('integrations.overseerr.notConnected')}</div>
        </div>
        {!open && (
          <Button
            variant={saved.connected ? 'ghost' : 'secondary'}
            icon={saved.connected ? undefined : <PlugZap className="size-4" />}
            onClick={() => setOpen(true)}
          >
            {saved.connected ? t('integrations.change') : t('integrations.connect')}
          </Button>
        )}
      </div>
      {open && (
        <form onSubmit={submit} className="space-y-4 border-t border-line/60 p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('integrations.address')}>
              <input
                className={inputClass}
                value={url}
                onChange={(e) => {
                  setUrl(e.target.value)
                  setChecked(undefined)
                }}
                placeholder="http://overseerr:5055"
                required
              />
            </Field>
            <Field label={t('integrations.apiKey')}>
              <input
                className={inputClass}
                type="password"
                autoComplete="off"
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value)
                  setChecked(undefined)
                }}
                placeholder={keyNeeded ? t('integrations.keyWhere', { app: 'Overseerr' }) : t('integrations.saved')}
                required={keyNeeded}
              />
            </Field>
          </div>
          {checked && (
            <>
              <p className="flex items-start gap-2 text-sm text-accent">
                <Check className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 break-all">
                  {t('integrations.answers', { app: 'Overseerr', version: checked.version, url: checked.url })}
                </span>
              </p>
              <Field label={t('integrations.overseerr.requestAs')}>
                <select className={inputClass} value={requestAs} onChange={(e) => setRequestAs(Number(e.target.value))}>
                  <option value={0}>{t('integrations.overseerr.madeAsOwner', { name: checked.owner.name })}</option>
                  {checked.users
                    .filter((u) => u.id !== checked.owner.id)
                    .map((u) => (
                      <option key={u.id} value={u.id}>
                        {t('integrations.overseerr.madeAs', { name: u.name })}
                      </option>
                    ))}
                </select>
              </Field>
              <p className="text-sm text-muted">{t('integrations.overseerr.sameName')}</p>
            </>
          )}
          {error && <p className="text-sm text-danger">{error.message}</p>}
          <div className="flex flex-wrap justify-end gap-2">
            {saved.connected && (
              <Button
                type="button"
                variant="danger"
                className="me-auto"
                disabled={disconnect.isPending}
                onClick={() => confirm(t('integrations.overseerr.confirmDisconnect')) && disconnect.mutate()}
              >
                {t('integrations.disconnect')}
              </Button>
            )}
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              {t('cancel')}
            </Button>
            <Button type="button" disabled={!url || (keyNeeded && !apiKey) || test.isPending} onClick={() => test.mutate()}>
              {test.isPending ? t('integrations.testing') : t('integrations.test')}
            </Button>
            <Button variant="primary" disabled={!checked || save.isPending}>
              {t('save')}
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}

const modes: RequestMode[] = ['approve', 'auto', 'off']

function RequestModeSetting() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => qc.setQueryData(['settings'], s),
  })
  return (
    <fieldset className="space-y-2 pt-2">
      <legend className="mb-2 text-sm font-medium">{t('integrations.whoCanAdd')}</legend>
      {modes.map((m) => (
        <label key={m} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
          <input
            type="radio"
            name="requests"
            className="mt-1 accent-[var(--color-accent)]"
            checked={data?.requests === m}
            disabled={!data || save.isPending}
            onChange={() => save.mutate({ requests: m })}
          />
          <span>
            <span className="block text-sm font-medium">{t(`integrations.mode.${m}.label`)}</span>
            <span className="block text-sm text-muted">{t(`integrations.mode.${m}.detail`)}</span>
          </span>
        </label>
      ))}
    </fieldset>
  )
}
