import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ArrowUpCircle, CheckCircle2, ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { api, type UpdateChannel, type UpdateSettings as Settings, type Updates } from '../api'
import i18n from '../i18n'
import { Button } from './ui'

// Settings → Server health → Updates: newer versions of Matinee
// (internal/update), from tagged releases or, as a beta, from main, installed
// automatically or when an admin presses Update, where the server can.

const card = 'rounded-xl border border-line/60 bg-surface/50'
const code = <code className="rounded bg-text/10 px-1 py-0.5 font-mono text-[0.85em]" />

const when = (iso: string) => new Date(iso).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

/** The version as Settings shows it: main-<commit> keeps 7 characters of the commit. */
export const shortVersion = (v: string) => (/^main-[0-9a-f]{8,}$/.test(v) ? v.slice(0, 12) : v)

export function UpdateSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['updates'], queryFn: api.updates })
  const done = (u: Updates) => qc.setQueryData(['updates'], u)
  const save = useMutation({ mutationFn: (s: Settings) => api.saveUpdateSettings(s), onSuccess: done })
  const check = useMutation({ mutationFn: api.checkUpdates, onSuccess: done })
  const apply = useMutation({ mutationFn: api.applyUpdate, onSuccess: done })
  if (!data) return null

  const { install, settings, available } = data
  const channels = install.channels ?? []
  const busy = save.isPending || check.isPending || apply.isPending
  const error = save.error?.message ?? check.error?.message ?? apply.error?.message ?? data.lastError
  const choose = (channel: UpdateChannel) => {
    if (channel === settings.channel) return
    if (channel === 'main' && !confirm(t('settings.updates.mainConfirm'))) return
    save.mutate({ ...settings, channel })
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.updates.title')}</h2>
      <p className="text-sm text-muted">
        <Trans i18nKey="settings.updates.running" values={{ version: shortVersion(data.build.version) }} components={{ code }} />
      </p>

      {error && (
        <p role="alert" className="flex items-start gap-2 text-sm text-danger">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.updates.failed', { error })}
        </p>
      )}

      {/* What's new, and the button to install it. */}
      {channels.length > 0 && (
        <div role="status" className={`${card} flex flex-wrap items-center gap-3 p-4 text-sm ${available || data.applying ? 'border-accent/50' : ''}`}>
          {data.applying ? (
            <>
              <Loader2 className="size-5 shrink-0 animate-spin text-accent" />
              <span className="min-w-0 flex-1">{t('settings.updates.applying', { version: shortVersion(data.applying) })}</span>
            </>
          ) : available ? (
            <>
              <ArrowUpCircle className="size-5 shrink-0 text-accent" />
              <span className="min-w-0 flex-1">
                <span className="font-medium">{t('settings.updates.available', { version: shortVersion(available.version) })}</span>{' '}
                {data.waiting && <span className="text-muted">{t('settings.updates.waiting')} </span>}
                <a href={available.url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-accent underline">
                  {t('settings.updates.whatsNew')} <ExternalLink className="size-3" />
                </a>
              </span>
              {install.canApply && (
                <Button
                  variant="primary"
                  disabled={busy}
                  onClick={() => confirm(t('settings.updates.applyConfirm', { version: shortVersion(available.version) })) && apply.mutate()}
                >
                  {t('settings.updates.apply', { version: shortVersion(available.version) })}
                </Button>
              )}
            </>
          ) : data.following ? (
            <>
              <RefreshCw className="size-5 shrink-0 text-accent" />
              <span className="min-w-0 flex-1">{t('settings.updates.following')}</span>
            </>
          ) : (
            <>
              <CheckCircle2 className="size-5 shrink-0 text-muted" />
              <span className="min-w-0 flex-1">{data.checked ? t('settings.updates.upToDate') : t('settings.updates.notChecked')}</span>
            </>
          )}
          <Button icon={<RefreshCw className={`size-4 ${check.isPending ? 'animate-spin' : ''}`} />} disabled={busy} onClick={() => check.mutate()}>
            {check.isPending ? t('settings.updates.checking') : t('settings.updates.check')}
          </Button>
          {data.checked && <p className="w-full text-xs text-faint">{t('settings.updates.checked', { time: when(data.checked) })}</p>}
        </div>
      )}

      {/* The channel, and whether to install automatically. */}
      {channels.length > 0 && (
        <div className={`${card} space-y-4 p-4`}>
          <fieldset className="space-y-2">
            <legend className="mb-2 text-sm font-medium">{t('settings.updates.channel')}</legend>
            {(['release', 'main'] as const).map((c) => {
              const offered = channels.includes(c)
              return (
                <label key={c} className={`flex items-start gap-3 text-sm ${offered ? 'cursor-pointer' : 'opacity-50'}`}>
                  <input
                    type="radio"
                    name="update-channel"
                    className="mt-1 accent-[var(--color-accent)]"
                    checked={settings.channel === c}
                    disabled={!offered || busy}
                    onChange={() => choose(c)}
                  />
                  <span>
                    <span className="font-medium">{t(c === 'release' ? 'settings.updates.release' : 'settings.updates.main')}</span>
                    {c === 'main' && (
                      <span className="ms-2 inline-flex items-center gap-1 rounded-full bg-danger/15 px-2 py-0.5 text-xs font-medium text-danger">
                        <AlertTriangle className="size-3" /> {t('settings.updates.beta')}
                      </span>
                    )}
                    <span className="block text-muted">
                      {offered ? t(c === 'release' ? 'settings.updates.releaseHint' : 'settings.updates.mainHint') : t('settings.updates.mainImagesOnly')}
                    </span>
                  </span>
                </label>
              )
            })}
          </fieldset>

          {install.canApply && (
            <label className="flex cursor-pointer items-start gap-3 border-t border-line/60 pt-4 text-sm">
              <input
                type="checkbox"
                className="mt-1 size-5 accent-[var(--color-accent)]"
                checked={settings.auto}
                disabled={busy}
                onChange={(e) => save.mutate({ ...settings, auto: e.target.checked })}
              />
              <span>
                <span className="font-medium">{t('settings.updates.auto')}</span>
                <span className="block text-muted">{t(settings.channel === 'main' ? 'settings.updates.autoMainHint' : 'settings.updates.autoHint')}</span>
              </span>
            </label>
          )}
        </div>
      )}

      <p className="text-sm text-muted">
        <HowItUpdates data={data} />
      </p>
    </section>
  )
}

/** Who installs updates for this server, and how. */
function HowItUpdates({ data }: { data: Updates }) {
  const { install } = data
  switch (install.kind) {
    case 'argocd':
      return <Trans i18nKey="settings.updates.how.argocd" values={{ app: install.application }} components={{ code }} />
    case 'binary':
      return <Trans i18nKey={install.canApply ? 'settings.updates.how.binary' : 'settings.updates.how.binaryReadOnly'} components={{ code }} />
    case 'apt':
    case 'dnf':
    case 'homebrew':
      return <Trans i18nKey={install.canApply ? `settings.updates.how.${install.kind}Apply` : `settings.updates.how.${install.kind}`} components={{ code }} />
    case 'kubernetes':
    case 'docker':
    case 'app':
    case 'source':
      return <Trans i18nKey={`settings.updates.how.${install.kind}`} components={{ code }} />
  }
}
