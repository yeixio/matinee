import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clapperboard, Compass, PackageSearch, PlugZap, Search, Tv, Unplug } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ArrApp, type BoxOfficeCheck, type BoxOfficeConnection, type ConnectionCheck, type Integration, type NotifyAdmins, type OverseerrCheck, type OverseerrConnection, type RequestMode } from '../api'
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
  const { data: boxOffice } = useQuery({ queryKey: ['boxoffice'], queryFn: api.boxOffice })
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('integrations.title')}</h2>
        <p className="mt-1 text-sm text-muted">
          {t('integrations.intro')}
        </p>
      </div>
      <div className="space-y-3">
        {boxOffice && <BoxOfficeCard saved={boxOffice} />}
        {apps.map((a) =>
          data ? <Connection key={a.app} {...a} saved={data[a.app]} throughBoxOffice={a.app === 'radarr' ? !!boxOffice?.movies : !!boxOffice?.shows} /> : null,
        )}
      </div>
      {data && (data.radarr.connected || data.sonarr.connected) && <BoxOfficeNotifications />}
      <div id="overseerr" className="scroll-mt-24 pt-4">
        <h2 className="text-lg font-semibold">Overseerr</h2>
        <p className="mt-1 text-sm text-muted">{t('integrations.overseerr.intro')}</p>
      </div>
      {overseerr && <OverseerrConnectionCard saved={overseerr} />}
      {((data && (data.radarr.connected || data.sonarr.connected)) || overseerr?.connected) && <RequestModeSetting />}
    </section>
  )
}

// Box Office finds and downloads movies and shows, and answers as Radarr and
// Sonarr, so connecting it points both of those at it. Matinee looks for it
// on its own, also on the home network, and fills in its address; only the
// API key has to be typed, and the steps say where it is.
function BoxOfficeCard({ saved }: { saved: BoxOfficeConnection }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState(saved.url)
  const [apiKey, setApiKey] = useState('')
  const [checked, setChecked] = useState<BoxOfficeCheck>()
  const linked = saved.movies || saved.shows
  const keyNeeded = !saved.hasApiKey || url.trim().replace(/\/+$/, '') !== saved.url

  const refresh = () => {
    for (const key of ['boxoffice', 'integrations', 'requestOptions', 'upcoming', 'notification-settings']) qc.invalidateQueries({ queryKey: [key] })
  }
  // Until it's connected, Matinee looks for Box Office as soon as Settings
  // opens, on the usual addresses and the home network, and fills in what
  // it finds. The admin can still change it.
  const find = useQuery({ queryKey: ['boxoffice-find'], queryFn: api.findBoxOffice, enabled: !linked, staleTime: 5 * 60_000, retry: false })
  const nearby = find.data?.found[0]?.url
  useEffect(() => {
    if (nearby) setUrl((u) => u || nearby)
  }, [nearby])
  const test = useMutation({
    mutationFn: () => api.testBoxOffice({ url, apiKey: apiKey || undefined }),
    onSuccess: (c) => {
      setChecked(c)
      setUrl(c.url)
    },
  })
  const save = useMutation({
    mutationFn: () => api.saveBoxOffice({ url, apiKey: apiKey || undefined }),
    onSuccess: (s) => {
      setApiKey('')
      setUrl(s.url)
      setOpen(false)
      refresh()
    },
  })
  const disconnect = useMutation({
    mutationFn: api.deleteBoxOffice,
    onSuccess: () => {
      setUrl('')
      setChecked(undefined)
      setOpen(false)
      refresh()
    },
  })

  // Opening it checks the saved one.
  const { mutate: runTest } = test
  useEffect(() => {
    if (open && linked) runTest()
  }, [open, linked, runTest])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }
  const error = save.error ?? test.error ?? disconnect.error
  const status = saved.connected
    ? t('integrations.connected', { url: saved.url })
    : saved.movies
      ? t('integrations.boxoffice.moviesOnly', { url: saved.url })
      : saved.shows
        ? t('integrations.boxoffice.showsOnly', { url: saved.url })
        : nearby
          ? t('integrations.boxoffice.foundNearby', { url: nearby })
          : t('integrations.boxoffice.notConnected')

  return (
    <div className="rounded-xl border border-line/60 bg-surface/50" id="boxoffice">
      <div className="flex items-center gap-4 p-4">
        <span className="grid size-10 place-items-center rounded-lg bg-text/5 text-accent">
          <PackageSearch className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="font-medium">Box Office</div>
          <div className="truncate text-xs text-muted">{status}</div>
        </div>
        {!open && (
          <Button variant={saved.connected ? 'ghost' : 'secondary'} icon={saved.connected ? undefined : <PlugZap className="size-4" />} onClick={() => setOpen(true)}>
            {saved.connected ? t('integrations.change') : t('integrations.connect')}
          </Button>
        )}
      </div>
      {open && (
        <form onSubmit={submit} className="space-y-4 border-t border-line/60 p-4">
          <p className="text-sm text-muted">{t('integrations.boxoffice.intro')}</p>
          {find.isFetching && !find.data && (
            <p className="flex items-center gap-2 text-sm text-muted">
              <Search className="size-4 animate-pulse" /> {t('integrations.boxoffice.looking')}
            </p>
          )}
          {!linked &&
            find.data &&
            (find.data.found.length > 0 ? (
              <div className="flex flex-wrap items-center gap-2 text-sm">
                {find.data.found.map((f) => (
                  <button
                    key={f.url}
                    type="button"
                    onClick={() => {
                      setUrl(f.url)
                      setChecked(undefined)
                    }}
                    className={`flex items-center gap-1.5 rounded-full border px-3 py-1 ${url === f.url ? 'border-accent text-accent' : 'border-line/60 text-muted hover:text-text'}`}
                  >
                    {url === f.url && <Check className="size-3.5" />}
                    {t('integrations.boxoffice.found', { url: f.url, version: f.version })}
                  </button>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted">{t('integrations.boxoffice.notFound')}</p>
            ))}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('integrations.address')}>
              <input
                className={inputClass}
                value={url}
                onChange={(e) => {
                  setUrl(e.target.value)
                  setChecked(undefined)
                }}
                placeholder="http://boxoffice:7272"
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
                placeholder={keyNeeded ? t('integrations.boxoffice.keyWhere') : t('integrations.saved')}
                required={keyNeeded}
              />
            </Field>
          </div>
          {keyNeeded && !checked && (
            <div className="rounded-lg bg-text/5 p-3 text-sm">
              <div className="mb-1 font-medium">{t('integrations.boxoffice.keySteps')}</div>
              <ol className="list-decimal space-y-0.5 ps-5 text-muted">
                <li>{t('integrations.boxoffice.step1')}</li>
                <li>{t('integrations.boxoffice.step2')}</li>
                <li>{t('integrations.boxoffice.step3')}</li>
              </ol>
            </div>
          )}
          {checked && (
            <>
              <p className="flex items-start gap-2 text-sm text-accent">
                <Check className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 break-all">{t('integrations.answers', { app: 'Box Office', version: checked.version, url: checked.url })}</span>
              </p>
              <p className="text-sm text-muted">{t('integrations.boxoffice.replaces')}</p>
              {checked.movies.rootFolders.length > 0 && checked.shows.rootFolders.length === 0 && <p className="text-sm text-danger">{t('integrations.boxoffice.noShowsFolder')}</p>}
              {checked.shows.rootFolders.length > 0 && checked.movies.rootFolders.length === 0 && <p className="text-sm text-danger">{t('integrations.boxoffice.noMoviesFolder')}</p>}
            </>
          )}
          {error && <p className="text-sm text-danger">{error.message}</p>}
          <div className="flex flex-wrap justify-end gap-2">
            {linked && (
              <Button
                type="button"
                variant="danger"
                className="me-auto"
                disabled={disconnect.isPending}
                onClick={() => confirm(t('integrations.boxoffice.confirmDisconnect')) && disconnect.mutate()}
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

// throughBoxOffice says this app's connection is Box Office's: it's shown as
// such, and disconnected with Box Office, so movies or shows don't drop out
// of Box Office by accident.
function Connection({ app, name, port, icon: Icon, saved, throughBoxOffice }: (typeof apps)[number] & { saved: Integration; throughBoxOffice: boolean }) {
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
    qc.invalidateQueries({ queryKey: ['boxoffice'] })
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
      setApiKey('')
      setChecked(undefined)
      setOpen(false)
      refresh()
    },
  })
  const askDisconnect = () => {
    if (confirm(t('integrations.confirmDisconnect', { app: name }))) disconnect.mutate()
  }

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
          <div className="truncate text-xs text-muted">{throughBoxOffice
              ? t(`integrations.throughBoxOffice.${app}`, { url: saved.url })
              : saved.connected
                ? t('integrations.connected', { url: saved.url })
                : t(`integrations.notConnected.${app}`)}</div>
        </div>
        {!open && (
          <div className="flex shrink-0 flex-wrap justify-end gap-1">
            {saved.connected && !throughBoxOffice && (
              <Button
                variant="ghost"
                className="px-3 text-danger! hover:bg-danger/10!"
                icon={<Unplug className="size-4" />}
                aria-label={t('integrations.disconnect')}
                title={t('integrations.disconnect')}
                disabled={disconnect.isPending}
                onClick={askDisconnect}
              >
                <span className="hidden sm:inline">{t('integrations.disconnect')}</span>
              </Button>
            )}
            <Button
              variant={saved.connected ? 'ghost' : 'secondary'}
              icon={saved.connected ? undefined : <PlugZap className="size-4" />}
              onClick={() => setOpen(true)}
            >
              {saved.connected ? t('integrations.change') : t('integrations.connect')}
            </Button>
          </div>
        )}
      </div>
      {!open && disconnect.error && <p className="px-4 pb-4 text-sm text-danger">{disconnect.error.message}</p>}
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
            {saved.connected && !throughBoxOffice && (
              <Button
                type="button"
                variant="danger"
                className="me-auto"
                disabled={disconnect.isPending}
                onClick={askDisconnect}
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
      setOpen(false)
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

const adminModes: NotifyAdmins[] = ['problems', 'all', 'off']

// When Radarr or Sonarr is really Box Office, it tells people about what
// they asked for; admins pick what they hear about.
function BoxOfficeNotifications() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['notification-settings'],
    queryFn: api.notificationSettings,
    // Matinee checks within half a minute of a connection changing.
    refetchInterval: (q) => (q.state.data?.boxOffice.length ? false : 15_000),
  })
  const save = useMutation({
    mutationFn: api.saveNotificationSettings,
    onSuccess: (s) => qc.setQueryData(['notification-settings'], s),
  })
  if (!data?.boxOffice.some((b) => b.id)) return null
  return (
    <fieldset className="space-y-2 pt-2">
      <legend className="mb-1 text-sm font-medium">{t('notifications.admins.legend')}</legend>
      <p className="mb-2 text-sm text-muted">{t('notifications.admins.intro')}</p>
      {adminModes.map((m) => (
        <label key={m} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
          <input
            type="radio"
            name="notify-admins"
            className="mt-1 accent-[var(--color-accent)]"
            checked={data.admins === m}
            disabled={save.isPending}
            onChange={() => save.mutate(m)}
          />
          <span>
            <span className="block text-sm font-medium">{t(`notifications.admins.${m}.label`)}</span>
            <span className="block text-sm text-muted">{t(`notifications.admins.${m}.detail`)}</span>
          </span>
        </label>
      ))}
    </fieldset>
  )
}
