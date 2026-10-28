import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, Globe, Link2, Plus, Puzzle, Trash2, Upload } from 'lucide-react'
import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, pluginFileUrl, type CatalogPlugin, type Plugin } from '../api'
import { Button, Field, inputClass } from './ui'

// Plugins, for admins (docs/guide/server/plugins.md): install from the
// official list, a link or a file; see what each one adds and reaches; turn
// them on and off; fill in their settings.
export function Plugins() {
  const { t } = useTranslation()
  const { data: plugins = [] } = useQuery({ queryKey: ['plugins'], queryFn: api.plugins })
  const [adding, setAdding] = useState(false)
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold">{t('plugins.title')}</h2>
        {!adding && (
          <Button icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
            {t('plugins.add')}
          </Button>
        )}
      </div>
      <p className="text-sm text-muted">{t('plugins.hint')}</p>
      {adding && <AddPlugin installed={plugins} onDone={() => setAdding(false)} />}
      <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {plugins.length === 0 && !adding && <p className="p-5 text-sm text-muted">{t('plugins.none')}</p>}
        {plugins.map((p) => (
          <PluginRow key={p.id} plugin={p} />
        ))}
      </div>
    </section>
  )
}

function useRefreshPlugins() {
  const qc = useQueryClient()
  return (p?: Plugin) => {
    if (p) qc.setQueryData<Plugin[]>(['plugins'], (old = []) => (old.some((o) => o.id === p.id) ? old.map((o) => (o.id === p.id ? p : o)) : [...old, p]))
    qc.invalidateQueries({ queryKey: ['plugins'] })
    qc.invalidateQueries({ queryKey: ['pluginCatalog'] })
    qc.invalidateQueries({ queryKey: ['players'] })
    qc.invalidateQueries({ queryKey: ['libraryKinds'] })
  }
}

function PluginIcon({ plugin, className = 'size-10' }: { plugin: Pick<Plugin, 'id' | 'icon' | 'hasIcon'>; className?: string }) {
  return plugin.hasIcon && plugin.icon ? (
    <img src={pluginFileUrl(plugin.id, plugin.icon)} alt="" className={`${className} shrink-0 rounded-lg object-cover`} />
  ) : (
    <span className={`${className} grid shrink-0 place-items-center rounded-lg bg-text/5 text-accent`}>
      <Puzzle className="size-5" />
    </span>
  )
}

function AddPlugin({ installed, onDone }: { installed: Plugin[]; onDone: () => void }) {
  const { t } = useTranslation()
  const refresh = useRefreshPlugins()
  const catalog = useQuery({ queryKey: ['pluginCatalog'], queryFn: api.pluginCatalog, retry: false })
  const [url, setUrl] = useState('')
  const file = useRef<HTMLInputElement>(null)
  const install = useMutation({
    mutationFn: (from: string | File) => (typeof from === 'string' ? api.installPluginFrom(from) : api.installPluginFile(from)),
    onSuccess: (p) => {
      refresh(p)
      setUrl('')
      if (!installed.some((o) => o.id === p.id)) onDone()
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (url.trim()) install.mutate(url.trim())
  }
  const official = catalog.data ?? []

  return (
    <div className="space-y-5 rounded-xl border border-accent/30 bg-surface p-5">
      {(official.length > 0 || catalog.isError) && (
        <div className="space-y-2">
          <h3 className="text-xs font-medium tracking-wide text-muted uppercase">{t('plugins.official')}</h3>
          {catalog.isError && <p className="text-sm text-muted">{t('plugins.catalogFailed')}</p>}
          {official.map((c) => (
            <CatalogRow key={c.id} entry={c} busy={install.isPending} onInstall={() => install.mutate(c.url)} />
          ))}
        </div>
      )}
      <form onSubmit={submit}>
        <Field label={t('plugins.fromLink')}>
          <div className="flex gap-2">
            <input
              className={inputClass}
              type="url"
              inputMode="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/plugin.matinee-plugin"
            />
            <Button icon={<Link2 className="size-4" />} disabled={!url.trim() || install.isPending}>
              {t('plugins.install')}
            </Button>
          </div>
        </Field>
      </form>
      <div className="space-y-1.5">
        <span className="text-xs font-medium tracking-wide text-muted uppercase">{t('plugins.fromFile')}</span>
        <div>
          <input
            ref={file}
            type="file"
            accept=".matinee-plugin,.zip"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) install.mutate(f)
              e.target.value = ''
            }}
          />
          <Button type="button" icon={<Upload className="size-4" />} disabled={install.isPending} onClick={() => file.current?.click()}>
            {t('plugins.chooseFile')}
          </Button>
        </div>
      </div>
      {install.isPending && <p className="text-sm text-muted">{t('plugins.installing')}</p>}
      {install.error && <p className="text-sm text-danger">{install.error.message}</p>}
      <div className="flex justify-end">
        <Button type="button" variant="ghost" onClick={onDone}>
          {t('close')}
        </Button>
      </div>
    </div>
  )
}

function CatalogRow({ entry, busy, onInstall }: { entry: CatalogPlugin; busy: boolean; onInstall: () => void }) {
  const { t } = useTranslation()
  const current = entry.installed === entry.version
  return (
    <div className="flex items-center gap-4 rounded-lg bg-text/5 p-3">
      <div className="min-w-0 flex-1">
        <div className="font-medium">{entry.name}</div>
        <div className="text-sm text-muted">{entry.description}</div>
      </div>
      {current ? (
        <span className="text-sm text-muted">{t('plugins.installed')}</span>
      ) : (
        <Button variant="primary" disabled={busy} onClick={onInstall}>
          {entry.installed ? t('plugins.update', { version: entry.version }) : t('plugins.install')}
        </Button>
      )}
    </div>
  )
}

function PluginRow({ plugin: p }: { plugin: Plugin }) {
  const { t } = useTranslation()
  const refresh = useRefreshPlugins()
  const [open, setOpen] = useState(false)
  const toggle = useMutation({ mutationFn: (enabled: boolean) => api.updatePlugin(p.id, { enabled }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: () => api.removePlugin(p.id), onSuccess: () => refresh() })
  const hosts = p.permissions?.network ?? []
  const turnOn = () => {
    if (hosts.length === 0 || confirm(t('plugins.confirmOn', { name: p.name, hosts: hosts.join(', ') }))) toggle.mutate(true)
  }
  const status = p.running
    ? { label: t('plugins.on'), className: 'bg-accent/15 text-accent' }
    : p.enabled
      ? { label: t('plugins.notRunning'), className: 'bg-danger/15 text-danger' }
      : { label: t('plugins.off'), className: 'bg-text/10 text-muted' }

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-start gap-4">
        <PluginIcon plugin={p} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <button onClick={() => setOpen(!open)} className="font-medium hover:underline" aria-expanded={open}>
              {p.name}
            </button>
            <span className={`rounded-full px-2 py-0.5 text-xs ${status.className}`}>{status.label}</span>
          </div>
          <div className="text-xs text-faint">
            {[t('plugins.version', { version: p.version }), p.author && t('plugins.by', { author: p.author })].filter(Boolean).join(' · ')}
          </div>
          {p.description && <p className="mt-1 text-sm text-muted">{p.description}</p>}
          {!p.supported ? (
            <p className="mt-1 text-sm text-danger">{t('plugins.unsupported')}</p>
          ) : (
            p.enabled && p.problem && <p className="mt-1 text-sm text-danger">{p.problem}</p>
          )}
        </div>
        {p.enabled ? (
          <Button disabled={toggle.isPending} onClick={() => toggle.mutate(false)}>
            {t('plugins.turnOff')}
          </Button>
        ) : (
          <Button variant="primary" disabled={toggle.isPending || !p.supported} onClick={turnOn}>
            {t('plugins.turnOn')}
          </Button>
        )}
      </div>
      {toggle.error && <p className="text-sm text-danger">{toggle.error.message}</p>}
      {open && (
        <div className="space-y-4 ps-14">
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
            <dt className="text-muted">{t('plugins.adds')}</dt>
            <dd>
              {[
                ...(p.libraryKinds ?? []).map((k) => t('plugins.libraryKind', { label: k.label })),
                ...(p.players?.length ? [t('plugins.player')] : []),
                ...(p.events?.length ? [t('plugins.events')] : []),
              ].join(', ') || '—'}
            </dd>
            <dt className="text-muted">{t('plugins.reaches')}</dt>
            <dd className="space-y-1">
              {p.libraryKinds?.length ? <div>{t('plugins.libraryFolders')}</div> : null}
              {hosts.length > 0 ? (
                hosts.map((h) => (
                  <div key={h} className="flex items-center gap-1.5">
                    <Globe className="size-3.5 text-muted" /> {h}
                  </div>
                ))
              ) : (
                <div>{t('plugins.reachesNothing')}</div>
              )}
            </dd>
            {p.homepage && (
              <>
                <dt className="text-muted">{t('plugins.homepage')}</dt>
                <dd>
                  <a href={p.homepage} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 underline-offset-4 hover:underline">
                    {p.homepage.replace(/^https?:\/\//, '')} <ExternalLink className="size-3.5" />
                  </a>
                </dd>
              </>
            )}
          </dl>
          {p.settings && p.settings.length > 0 && <PluginConfig plugin={p} />}
          <Button
            variant="danger"
            icon={<Trash2 className="size-4" />}
            disabled={remove.isPending}
            onClick={() => confirm(t('plugins.confirmRemove', { name: p.name })) && remove.mutate()}
          >
            {t('plugins.remove')}
          </Button>
          {remove.error && <p className="text-sm text-danger">{remove.error.message}</p>}
        </div>
      )}
    </div>
  )
}

function PluginConfig({ plugin: p }: { plugin: Plugin }) {
  const { t } = useTranslation()
  const refresh = useRefreshPlugins()
  const [values, setValues] = useState<Record<string, string>>(p.config)
  const save = useMutation({ mutationFn: () => api.updatePlugin(p.id, { config: values }), onSuccess: refresh })
  const changed = (p.settings ?? []).some((s) => (values[s.key] ?? '') !== (p.config[s.key] ?? ''))
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      {p.settings?.map((s) => (
        <Field key={s.key} label={s.label}>
          <input
            className={inputClass}
            type={s.secret ? 'password' : 'text'}
            autoComplete="off"
            value={values[s.key] ?? ''}
            onChange={(e) => setValues({ ...values, [s.key]: e.target.value })}
          />
          {s.description && <span className="block text-xs text-muted">{s.description}</span>}
        </Field>
      ))}
      {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      <Button variant="primary" disabled={!changed || save.isPending}>
        {save.isSuccess && !changed ? t('plugins.saved') : t('save')}
      </Button>
    </form>
  )
}
