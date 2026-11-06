import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Copy, ExternalLink, FlaskConical, RefreshCw, Sparkles } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Updates, type UpdateSettings as Settings } from '../api'
import i18n from '../i18n'
import { Button, Spinner, inputClass } from './ui'

// Settings → Updates: newer versions from GitHub (internal/update), on the
// stable or beta channel, installed here where the server can replace
// itself, or the steps to install them where it can't (containers, Git).

const card = 'rounded-xl border border-line/60 bg-surface/50'
const when = (ms: number) => new Date(ms).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

/** Display names for what deploys a server from Git. */
const tools: Record<string, string> = { argocd: 'Argo CD', 'argo-cd': 'Argo CD', argo: 'Argo CD', flux: 'Flux', fluxcd: 'Flux' }

export function UpdateSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  // The version an install is restarting into, kept across the restart.
  const [target, setTarget] = useState<string>()
  const { data, isError } = useQuery({
    queryKey: ['updates'],
    queryFn: api.updates,
    // While installing, follow it; the server goes away for a moment when it restarts.
    refetchInterval: (q) => (target || (q.state.data?.apply.state && q.state.data.apply.state !== 'failed') ? 2000 : false),
    retry: target ? true : 3,
  })
  const done = (u: Updates) => qc.setQueryData(['updates'], u)
  const save = useMutation({ mutationFn: (s: Settings) => api.saveUpdateSettings(s), onSuccess: done })
  const check = useMutation({ mutationFn: api.checkUpdates, onSuccess: done })
  const apply = useMutation({
    mutationFn: api.applyUpdate,
    onSuccess: (u) => {
      done(u)
      setTarget(u.apply.version)
    },
  })
  const updated = target && data?.current === target
  useEffect(() => {
    if (updated) qc.invalidateQueries({ queryKey: ['server'] })
  }, [updated, qc])
  if (!data) return null

  const { install, settings, latest } = data
  const state = data.apply.state
  const busy = state === 'downloading' || state === 'installing' || state === 'restarting'
  const restarting = !!target && !updated && (busy || isError)
  const tool = install.managedBy ? (tools[install.managedBy] ?? install.managedBy) : ''
  const how = howTo(install.kind, install.why)

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.updates.title')}</h2>
      <p className="text-sm text-muted">{t('settings.updates.intro')}</p>

      <div className={`${card} flex flex-wrap items-end gap-4 p-4`}>
        <label className="space-y-1 text-sm">
          <span className="block font-medium">{t('settings.updates.channel')}</span>
          <select
            className={inputClass}
            value={settings.channel}
            disabled={save.isPending || !install.beta}
            onChange={(e) => {
              const channel = e.target.value as Settings['channel']
              if (channel === 'beta' && !confirm(t('settings.updates.betaConfirm'))) return
              save.mutate({ ...settings, channel })
            }}
          >
            <option value="stable">{t('settings.updates.stable')}</option>
            {install.beta && <option value="beta">{t('settings.updates.beta')}</option>}
          </select>
        </label>
        <label className="space-y-1 text-sm">
          <span className="block font-medium">{t('settings.updates.mode')}</span>
          <select className={inputClass} value={settings.mode} disabled={save.isPending} onChange={(e) => save.mutate({ ...settings, mode: e.target.value as Settings['mode'] })}>
            {(install.canApply || settings.mode === 'install') && <option value="install">{t('settings.updates.modeInstall')}</option>}
            <option value="notify">{t('settings.updates.modeNotify')}</option>
            <option value="off">{t('settings.updates.modeOff')}</option>
          </select>
        </label>
        <Button className="ms-auto" icon={check.isPending ? <Spinner className="size-4" /> : <RefreshCw className="size-4" />} disabled={check.isPending || busy} onClick={() => check.mutate()}>
          {check.isPending ? t('settings.updates.checking') : t('settings.updates.checkNow')}
        </Button>
      </div>
      <p className="text-sm text-muted">
        {settings.channel === 'beta' ? (
          <span className="inline-flex items-start gap-1.5">
            <FlaskConical className="mt-0.5 size-4 shrink-0 text-accent" />
            {t('settings.updates.betaHint')}
          </span>
        ) : install.beta ? (
          t('settings.updates.stableHint')
        ) : (
          t('settings.updates.betaUnavailable')
        )}
      </p>

      <div className={`${card} space-y-4 p-4`}>
        <p className="text-sm">
          {t('settings.updates.running', { version: data.current })}{' '}
          <span className="text-muted">{data.checked ? t('settings.updates.checked', { time: when(data.checked) }) : settings.mode === 'off' ? '' : t('settings.updates.notChecked')}</span>
        </p>

        {updated ? (
          <div role="status" className="flex flex-wrap items-center gap-3 text-sm">
            <CheckCircle2 className="size-4 shrink-0 text-accent" />
            <span className="min-w-0 flex-1 font-medium">{t('settings.updates.updated', { version: target })}</span>
            <Button onClick={() => location.reload()}>{t('settings.updates.reload')}</Button>
          </div>
        ) : restarting ? (
          <p role="status" className="flex items-center gap-2 text-sm">
            <Spinner className="size-4" /> {t('settings.updates.restarting', { version: target })}
          </p>
        ) : busy ? (
          <p role="status" className="flex items-center gap-2 text-sm">
            <Spinner className="size-4" /> {t(`settings.updates.${state}`, { version: data.apply.version })}
          </p>
        ) : null}
        {state === 'failed' && (
          <p className="flex items-start gap-2 text-sm text-danger">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.updates.failed', { version: data.apply.version, error: data.apply.error })}
          </p>
        )}
        {(data.error || check.error) && (
          <p className="flex items-start gap-2 text-sm text-danger">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.updates.checkFailed', { error: data.error ?? check.error?.message })}
          </p>
        )}
        {install.why === 'dev' && <p className="text-sm text-muted">{t('settings.updates.dev')}</p>}

        {!updated && latest ? (
          <div className="space-y-3 border-t border-line/60 pt-4">
            <div className="flex flex-wrap items-center gap-3">
              <Sparkles className="size-5 shrink-0 text-accent" />
              <div className="min-w-0 flex-1">
                <div className="font-semibold">{t('settings.updates.available', { version: latest.version })}</div>
                {latest.published ? <div className="text-sm text-muted">{t('settings.updates.published', { time: when(latest.published) })}</div> : null}
              </div>
              {install.canApply && (
                <Button
                  variant="primary"
                  disabled={busy || apply.isPending || restarting}
                  onClick={() => confirm(t('settings.updates.installConfirm', { version: latest.version })) && apply.mutate()}
                >
                  {t('settings.updates.install', { version: latest.version })}
                </Button>
              )}
            </div>
            {apply.error && <p className="text-sm text-danger">{apply.error.message}</p>}

            {latest.notes && (
              <details className="text-sm">
                <summary className="cursor-pointer font-medium">{t('settings.updates.notes')}</summary>
                <div className="mt-2 max-h-80 space-y-2 overflow-auto rounded-lg bg-text/5 p-3 text-muted">
                  <Notes text={latest.notes} />
                </div>
              </details>
            )}
            {latest.commits && latest.commits.length > 0 && (
              <details className="text-sm" open>
                <summary className="cursor-pointer font-medium">{t('settings.updates.changes')}</summary>
                <ul className="mt-2 max-h-80 space-y-1 overflow-auto">
                  {latest.commits.map((c) => (
                    <li key={c.sha} className="flex gap-2">
                      <a href={c.url} target="_blank" rel="noreferrer" className="shrink-0 font-mono text-xs leading-5 text-faint hover:text-text">
                        {c.sha}
                      </a>
                      <span className="min-w-0 text-muted">{c.title}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
            <a href={latest.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-muted underline hover:text-text">
              {latest.commits ? t('settings.updates.allChanges') : t('settings.updates.releasePage')} <ExternalLink className="size-3.5" />
            </a>

            {!install.canApply && (
              <div className="space-y-2 border-t border-line/60 pt-4 text-sm">
                <p className="font-medium">{t('settings.updates.howTo')}</p>
                <p className="text-muted">{tool ? t('settings.updates.how.managed', { tool }) : t(how)}</p>
                {data.steps?.map((step) => <Command key={step} text={step} />)}
              </div>
            )}
          </div>
        ) : !updated && data.checked && !data.error ? (
          <p className="flex items-center gap-2 text-sm">
            <CheckCircle2 className="size-4 shrink-0 text-accent" />
            {data.ahead ? t('settings.updates.ahead') : t('settings.updates.upToDate')}
          </p>
        ) : null}

        {settings.mode === 'install' && install.canApply && <p className="text-sm text-muted">{t('settings.updates.autoHint')}</p>}
        {!latest && !install.canApply && install.why !== 'dev' && <p className="text-sm text-muted">{tool ? t('settings.updates.how.managed', { tool }) : t(how)}</p>}
      </div>
    </section>
  )
}

/** What the admin does to install an update on this kind of install. */
function howTo(kind: Updates['install']['kind'], why?: Updates['install']['why']) {
  if (why === 'read-only') return 'settings.updates.how.readOnly' as const
  if (why === 'windows') return 'settings.updates.how.windows' as const
  switch (kind) {
    case 'docker':
      return 'settings.updates.how.docker' as const
    case 'kubernetes':
      return 'settings.updates.how.kubernetes' as const
    case 'deb':
    case 'rpm':
      return 'settings.updates.how.package' as const
    case 'homebrew':
      return 'settings.updates.how.homebrew' as const
    case 'macos-app':
      return 'settings.updates.how.macApp' as const
    case 'windows-app':
      return 'settings.updates.how.windowsApp' as const
  }
  return 'settings.updates.how.binary' as const
}

/**
 * Release notes are Markdown; this shows the parts they use (headings,
 * lists, bold and links) as plain elements, never as HTML.
 */
function Notes({ text }: { text: string }) {
  const blocks: ReactNode[] = []
  let items: ReactNode[] = []
  const flush = () => {
    if (items.length) blocks.push(<ul key={blocks.length} className="list-disc space-y-1 ps-5">{items}</ul>)
    items = []
  }
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim()
    const heading = /^#{1,6}\s+(.*)$/.exec(line)
    const item = /^[-*]\s+(.*)$/.exec(line)
    if (item) {
      items.push(<li key={items.length}>{inline(item[1])}</li>)
      continue
    }
    flush()
    if (heading) blocks.push(<p key={blocks.length} className="pt-1 font-semibold text-text">{inline(heading[1])}</p>)
    else if (line) blocks.push(<p key={blocks.length}>{inline(line)}</p>)
  }
  flush()
  return <>{blocks}</>
}

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  const re = /\*\*(.+?)\*\*|\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)|`([^`]+)`/g
  let last = 0
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[1] !== undefined) out.push(<strong key={m.index} className="font-semibold text-text">{inline(m[1])}</strong>)
    else if (m[2] !== undefined)
      out.push(
        <a key={m.index} href={m[3]} target="_blank" rel="noreferrer" className="underline hover:text-text">
          {m[2]}
        </a>,
      )
    else out.push(<code key={m.index} className="font-mono text-xs">{m[4]}</code>)
    last = m.index + m[0].length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function Command({ text }: { text: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const link = /^https?:\/\//.test(text)
  if (link) {
    return (
      <a href={text} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 break-all underline">
        {text} <ExternalLink className="size-3.5 shrink-0" />
      </a>
    )
  }
  return (
    <div className="flex items-start gap-2 rounded-lg bg-text/5 p-3">
      <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre font-mono text-xs leading-5">{text}</pre>
      <button
        className="rounded-full p-1.5 text-muted hover:bg-text/10 hover:text-text"
        aria-label={t('settings.updates.copy')}
        title={copied ? t('settings.updates.copied') : t('settings.updates.copy')}
        onClick={() =>
          navigator.clipboard?.writeText(text).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
          })
        }
      >
        {copied ? <CheckCircle2 className="size-4" /> : <Copy className="size-4" />}
      </button>
    </div>
  )
}
