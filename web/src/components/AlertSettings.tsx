import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, BellRing, CircleCheck, CircleX, Copy, Info, Plus, Send, Trash2 } from 'lucide-react'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Alert, type AlertConfig, type AlertSettings as Settings, type AlertWebhook, type RateRule, type WebhookKind } from '../api'
import { ago } from './ServerHealth'
import { Button, Field, inputClass } from './ui'

// Settings → Server health → Alerts: what the server emails or posts to
// webhooks about, where it sends them, the latest it sent, and Prometheus
// metrics. Alerts go straight from the server to the admin's own mail
// server and webhooks (internal/alerts).

const card = 'rounded-xl border border-line/60 bg-surface/50'
const kinds: WebhookKind[] = ['discord', 'ntfy', 'homeassistant', 'json']

/** A webhook in the form: a saved one shows a hint of its address. */
type FormWebhook = AlertWebhook & { key: string; hint?: string; hasToken?: boolean }
type Form = Omit<AlertConfig, 'webhooks'> & { webhooks: FormWebhook[] }

function toForm(s: Settings): Form {
  return {
    email: { ...s.email, password: '' },
    rules: structuredClone(s.rules),
    webhooks: s.webhooks.map((h) => ({ ...h, key: h.id, url: '', token: '' })),
  }
}

function toConfig(f: Form): AlertConfig {
  return {
    email: f.email,
    rules: f.rules,
    webhooks: f.webhooks.map(({ id, kind, name, url, token }) => ({ id, kind, name, url, token })),
  }
}

let nextKey = 0

export function AlertSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['alerts'], queryFn: api.alerts, refetchInterval: 30_000 })
  const [form, setForm] = useState<Form>()
  const [dirty, setDirty] = useState(false)
  const [tested, setTested] = useState<Alert>()
  useEffect(() => {
    if (data && !dirty) setForm(toForm(data))
  }, [data, dirty])

  const save = useMutation({
    mutationFn: (f: Form) => api.saveAlerts(toConfig(f)),
    onSuccess: (v) => {
      qc.setQueryData(['alerts'], v)
      setForm(toForm(v))
      setDirty(false)
    },
  })
  const test = useMutation({
    mutationFn: (f: Form) => api.testAlerts(toConfig(f)),
    onSuccess: (a) => {
      setTested(a)
      qc.invalidateQueries({ queryKey: ['alerts'] })
    },
  })
  if (!data || !form) return null

  const change = (f: (draft: Form) => void) => {
    const next = structuredClone(form)
    f(next)
    setForm(next)
    setDirty(true)
    setTested(undefined)
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate(form)
  }
  const channels = (form.email.on ? 1 : 0) + form.webhooks.length
  const error = save.error ?? test.error

  return (
    <section className="max-w-3xl space-y-8">
      <div>
        <h2 className="text-lg font-semibold">{t('alerts.title')}</h2>
        <p className="mt-1 text-sm text-muted">{t('alerts.intro')}</p>
      </div>

      {data.firing.length > 0 && (
        <div className="space-y-3">
          <h3 className="font-semibold">{t('alerts.firing')}</h3>
          <AlertList alerts={data.firing} />
        </div>
      )}

      <form onSubmit={submit} className="space-y-8">
        <div className="space-y-3">
          <h3 className="font-semibold">{t('alerts.rules.title')}</h3>
          <div className={`${card} divide-y divide-line/60`}>
            <RuleRow
              title={t('alerts.rules.disk')}
              on={form.rules.disk.on}
              onToggle={(on) => change((f) => (f.rules.disk.on = on))}
            >
              <NumberField
                label={t('alerts.rules.percent')}
                value={form.rules.disk.percent}
                min={50}
                max={99}
                onChange={(v) => change((f) => (f.rules.disk.percent = v))}
              />
            </RuleRow>
            <RuleRow
              title={t('alerts.rules.folder')}
              hint={t('alerts.rules.folderHint')}
              on={form.rules.folder.on}
              onToggle={(on) => change((f) => (f.rules.folder.on = on))}
            />
            <RateRow
              title={t('alerts.rules.errors')}
              rule={form.rules.errors}
              onChange={(r) => change((f) => (f.rules.errors = r))}
            />
            <RateRow
              title={t('alerts.rules.transcodes')}
              rule={form.rules.transcodes}
              onChange={(r) => change((f) => (f.rules.transcodes = r))}
            />
            <RuleRow
              title={t('alerts.rules.restart')}
              hint={t('alerts.rules.restartHint')}
              on={form.rules.restart.on}
              onToggle={(on) => change((f) => (f.rules.restart.on = on))}
            />
            <RuleRow
              title={t('alerts.rules.backup')}
              hint={t('alerts.rules.backupHint')}
              on={form.rules.backup?.on ?? true}
              onToggle={(on) => change((f) => (f.rules.backup = { on }))}
            />
            <RuleRow
              title={t('alerts.rules.transcodeSpace')}
              hint={t('alerts.rules.transcodeSpaceHint')}
              on={form.rules.transcodeSpace?.on ?? true}
              onToggle={(on) => change((f) => (f.rules.transcodeSpace = { on }))}
            />
          </div>
        </div>

        <div className="space-y-3">
          <h3 className="font-semibold">{t('alerts.email.title')}</h3>
          <div className={`${card} space-y-4 p-4`}>
            <label className="flex cursor-pointer items-start gap-4">
              <span className="min-w-0 flex-1 font-medium">{t('alerts.email.on')}</span>
              <input
                type="checkbox"
                className="mt-1 size-5 accent-[var(--color-accent)]"
                checked={form.email.on}
                onChange={(e) => change((f) => (f.email.on = e.target.checked))}
              />
            </label>
            {form.email.on && (
              <>
                <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_6rem_10rem]">
                  <Field label={t('alerts.email.host')}>
                    <input
                      dir="ltr"
                      className={inputClass}
                      value={form.email.host}
                      placeholder={examples.host}
                      autoComplete="off"
                      spellCheck={false}
                      onChange={(e) => change((f) => (f.email.host = e.target.value))}
                    />
                  </Field>
                  <Field label={t('alerts.email.port')}>
                    <input
                      dir="ltr"
                      inputMode="numeric"
                      className={inputClass}
                      value={form.email.port || ''}
                      onChange={(e) => change((f) => (f.email.port = Number(e.target.value.replace(/\D/g, '')) || 0))}
                    />
                  </Field>
                  <Field label={t('alerts.email.security')}>
                    <select
                      className={inputClass}
                      value={form.email.security}
                      onChange={(e) =>
                        change((f) => {
                          const security = e.target.value as Form['email']['security']
                          // Move the port along with it while it's a usual one.
                          const usual = { starttls: 587, tls: 465, none: 25 }
                          if (Object.values(usual).includes(f.email.port)) f.email.port = usual[security]
                          f.email.security = security
                        })
                      }
                    >
                      <option value="starttls">{protocols.starttls}</option>
                      <option value="tls">{protocols.tls}</option>
                      <option value="none">{t('alerts.email.none')}</option>
                    </select>
                  </Field>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label={t('alerts.email.username')}>
                    <input
                      dir="ltr"
                      className={inputClass}
                      value={form.email.username}
                      autoComplete="off"
                      spellCheck={false}
                      onChange={(e) => change((f) => (f.email.username = e.target.value))}
                    />
                  </Field>
                  <Field label={t('alerts.email.password')}>
                    <input
                      type="password"
                      autoComplete="new-password"
                      className={inputClass}
                      value={form.email.password ?? ''}
                      placeholder={data.email.hasPassword && form.email.username === data.email.username && form.email.host === data.email.host ? '••••••••' : undefined}
                      onChange={(e) => change((f) => (f.email.password = e.target.value))}
                    />
                  </Field>
                  <Field label={t('alerts.email.from')}>
                    <input
                      dir="ltr"
                      type="email"
                      className={inputClass}
                      value={form.email.from}
                      placeholder={examples.from}
                      onChange={(e) => change((f) => (f.email.from = e.target.value))}
                    />
                  </Field>
                  <Field label={t('alerts.email.to')}>
                    <input
                      dir="ltr"
                      className={inputClass}
                      value={form.email.to}
                      placeholder={examples.to}
                      onChange={(e) => change((f) => (f.email.to = e.target.value))}
                    />
                  </Field>
                </div>
                <p className="text-xs text-muted">{t('alerts.email.toHint')}</p>
              </>
            )}
          </div>
        </div>

        <div className="space-y-3">
          <div>
            <h3 className="font-semibold">{t('alerts.webhooks.title')}</h3>
            <p className="mt-1 text-sm text-muted">{t('alerts.webhooks.intro')}</p>
          </div>
          {form.webhooks.map((h, i) => (
            <div key={h.key} className={`${card} space-y-3 p-4`}>
              <div className="grid gap-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
                <Field label={t('alerts.webhooks.kind')}>
                  <select
                    className={inputClass}
                    value={h.kind}
                    onChange={(e) => change((f) => (f.webhooks[i].kind = e.target.value as WebhookKind))}
                  >
                    {kinds.map((k) => (
                      <option key={k} value={k}>
                        {t(`alerts.webhooks.kinds.${k}`)}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label={t('alerts.webhooks.name')}>
                  <input
                    className={inputClass}
                    value={h.name ?? ''}
                    placeholder={t(`alerts.webhooks.kinds.${h.kind}`)}
                    onChange={(e) => change((f) => (f.webhooks[i].name = e.target.value))}
                  />
                </Field>
              </div>
              <Field label={t('alerts.webhooks.url')}>
                <input
                  dir="ltr"
                  className={inputClass}
                  value={h.url ?? ''}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={h.hint ? h.hint : placeholders[h.kind]}
                  onChange={(e) => change((f) => (f.webhooks[i].url = e.target.value))}
                />
              </Field>
              {h.hint && <p className="text-xs text-muted">{t('alerts.webhooks.saved')}</p>}
              {h.kind === 'ntfy' && (
                <Field label={t('alerts.webhooks.token')}>
                  <input
                    dir="ltr"
                    type="password"
                    autoComplete="new-password"
                    className={inputClass}
                    value={h.token ?? ''}
                    placeholder={h.hasToken && !h.url ? '••••••••' : 'tk_…'}
                    onChange={(e) => change((f) => (f.webhooks[i].token = e.target.value))}
                  />
                </Field>
              )}
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="min-w-0 flex-1 text-xs text-muted">{t(`alerts.webhooks.help.${h.kind}`)}</p>
                <Button type="button" variant="ghost" icon={<Trash2 className="size-4" />} onClick={() => change((f) => f.webhooks.splice(i, 1))}>
                  {t('alerts.webhooks.remove')}
                </Button>
              </div>
            </div>
          ))}
          <Button
            type="button"
            icon={<Plus className="size-4" />}
            disabled={form.webhooks.length >= 20}
            onClick={() => change((f) => f.webhooks.push({ key: `new${nextKey++}`, kind: 'discord', name: '', url: '', token: '' }))}
          >
            {t('alerts.webhooks.add')}
          </Button>
        </div>

        <div className="space-y-3">
          {channels === 0 && <p className="text-sm text-muted">{t('alerts.noChannels')}</p>}
          {error && <p className="text-sm text-danger">{error.message}</p>}
          {tested && <TestResult alert={tested} />}
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="primary" disabled={!dirty || save.isPending}>
              {dirty ? t('alerts.save') : t('alerts.saved')}
            </Button>
            <Button type="button" icon={<Send className="size-4" />} disabled={channels === 0 || test.isPending} onClick={() => test.mutate(form)}>
              {t('alerts.test')}
            </Button>
          </div>
        </div>
      </form>

      <div className="space-y-3">
        <h3 className="font-semibold">{t('alerts.history.title')}</h3>
        {data.history.length === 0 ? <p className={`${card} p-4 text-sm text-muted`}>{t('alerts.history.none')}</p> : <AlertList alerts={data.history} />}
      </div>

      <Metrics />
    </section>
  )
}

// Examples and protocol names read the same in every language.
const examples = { host: 'smtp.example.com', from: 'matinee@example.com', to: 'you@example.com' }
const protocols = { starttls: 'STARTTLS', tls: 'TLS' }

const placeholders: Record<WebhookKind, string> = {
  discord: 'https://discord.com/api/webhooks/…',
  ntfy: 'https://ntfy.sh/…',
  homeassistant: 'http://homeassistant.local:8123/api/webhook/…',
  json: 'https://…',
}

function RuleRow({ title, hint, on, onToggle, children }: { title: string; hint?: string; on: boolean; onToggle: (on: boolean) => void; children?: ReactNode }) {
  return (
    <div className="space-y-3 p-4">
      <label className="flex cursor-pointer items-start gap-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{title}</span>
          {hint && <span className="mt-1 block text-sm text-muted">{hint}</span>}
        </span>
        <input type="checkbox" className="mt-1 size-5 accent-[var(--color-accent)]" checked={on} onChange={(e) => onToggle(e.target.checked)} />
      </label>
      {on && children && <div className="flex flex-wrap gap-3">{children}</div>}
    </div>
  )
}

function RateRow({ title, rule, onChange }: { title: string; rule: RateRule; onChange: (r: RateRule) => void }) {
  const { t } = useTranslation()
  return (
    <RuleRow title={title} on={rule.on} onToggle={(on) => onChange({ ...rule, on })}>
      <NumberField label={t('alerts.rules.count')} value={rule.count} min={1} max={10000} onChange={(count) => onChange({ ...rule, count })} />
      <NumberField label={t('alerts.rules.minutes')} value={rule.minutes} min={1} max={1440} onChange={(minutes) => onChange({ ...rule, minutes })} />
    </RuleRow>
  )
}

function NumberField({ label, value, min, max, onChange }: { label: string; value: number; min: number; max: number; onChange: (v: number) => void }) {
  return (
    <div className="w-36">
      <Field label={label}>
        <input
          type="number"
          className={inputClass}
          value={value || ''}
          min={min}
          max={max}
          onChange={(e) => onChange(Number(e.target.value) || 0)}
        />
      </Field>
    </div>
  )
}

function AlertIcon({ a }: { a: Alert }) {
  const { t } = useTranslation()
  if (a.resolved) return <CircleCheck className="mt-0.5 size-4 shrink-0 text-accent" aria-label={t('alerts.history.resolved')} />
  if (a.level === 'critical') return <CircleX className="mt-0.5 size-4 shrink-0 text-danger" aria-hidden />
  if (a.level === 'warning') return <AlertTriangle className="mt-0.5 size-4 shrink-0 text-danger" aria-hidden />
  if (a.rule === 'test') return <BellRing className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
  return <Info className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
}

function AlertList({ alerts }: { alerts: Alert[] }) {
  const { t } = useTranslation()
  const [all, setAll] = useState(false)
  const shown = all ? alerts : alerts.slice(0, 8)
  return (
    <>
      <ul className={`${card} divide-y divide-line/60`}>
        {shown.map((a) => {
          const total = a.sent + (a.failed?.length ?? 0)
          return (
            <li key={a.id || a.key} className="flex gap-3 p-4 text-sm">
              <AlertIcon a={a} />
              <div className="min-w-0 flex-1">
                <p className="font-medium break-words">{a.title}</p>
                <p className="text-muted break-words">{a.message}</p>
                <p className="mt-1 text-xs text-faint">
                  {ago(a.time)}
                  {a.id && <> · {total ? t('alerts.history.delivered', { sent: a.sent, total }) : t('alerts.history.notSent')}</>}
                </p>
                {a.failed?.map((f) => (
                  <p key={f} className="mt-1 text-xs text-danger break-words">
                    {f}
                  </p>
                ))}
              </div>
            </li>
          )
        })}
      </ul>
      {!all && alerts.length > shown.length && (
        <button className="text-sm text-muted underline hover:text-text" onClick={() => setAll(true)}>
          {t('health.problems.more', { count: alerts.length - shown.length })}
        </button>
      )}
    </>
  )
}

function TestResult({ alert }: { alert: Alert }) {
  const { t } = useTranslation()
  const total = alert.sent + (alert.failed?.length ?? 0)
  const ok = !alert.failed?.length
  return (
    <div className={`${card} space-y-1 p-4 text-sm`} role="status">
      <p className={`flex items-center gap-2 font-medium ${ok ? '' : 'text-danger'}`}>
        {ok ? <CircleCheck className="size-4 text-accent" aria-hidden /> : <CircleX className="size-4" aria-hidden />}
        {t('alerts.testSent', { sent: alert.sent, total })}
      </p>
      {alert.failed?.map((f) => (
        <p key={f} className="text-xs text-danger break-words">
          {f}
        </p>
      ))}
    </div>
  )
}

// Prometheus metrics at /metrics, for the admin's own monitoring.
function Metrics() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['metricsSettings'], queryFn: api.metricsSettings })
  const [copied, setCopied] = useState('')
  const on = useMutation({ mutationFn: api.newMetricsToken, onSuccess: (v) => qc.setQueryData(['metricsSettings'], v) })
  const off = useMutation({
    mutationFn: api.metricsOff,
    onSuccess: () => qc.setQueryData(['metricsSettings'], { on: false, path: '/metrics' }),
  })
  if (!data) return null
  const url = `${window.location.origin}${data.path}`
  const u = new URL(url)
  const config = `scrape_configs:
  - job_name: matinee
    scheme: ${u.protocol.replace(':', '')}
    metrics_path: ${data.path}
    authorization:
      credentials: ${data.token ?? ''}
    static_configs:
      - targets: ['${u.host}']`
  const copy = (what: string, text: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopied(what)
      setTimeout(() => setCopied(''), 2000)
    })
  }
  const error = on.error ?? off.error
  return (
    <div className="space-y-3">
      <div>
        <h3 className="font-semibold">{t('alerts.metrics.title')}</h3>
        <p className="mt-1 text-sm text-muted">{t('alerts.metrics.intro')}</p>
      </div>
      <div className={`${card} space-y-4 p-4`}>
        {data.on && data.token ? (
          <>
            <Copyable label={t('alerts.metrics.address')} value={url} copied={copied === 'url'} onCopy={() => copy('url', url)} />
            <Copyable label={t('alerts.metrics.token')} value={data.token} copied={copied === 'token'} onCopy={() => copy('token', data.token!)} />
            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium tracking-wide text-muted uppercase">{t('alerts.metrics.example')}</span>
                <button type="button" className="inline-flex items-center gap-1 text-xs text-muted hover:text-text" onClick={() => copy('config', config)}>
                  <Copy className="size-3.5" aria-hidden /> {copied === 'config' ? t('alerts.metrics.copied') : t('alerts.metrics.copy')}
                </button>
              </div>
              <pre dir="ltr" className="overflow-x-auto rounded-lg bg-text/5 p-3 font-mono text-xs">{config}</pre>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" disabled={on.isPending} onClick={() => on.mutate()}>
                {t('alerts.metrics.newToken')}
              </Button>
              <Button type="button" variant="ghost" disabled={off.isPending} onClick={() => off.mutate()}>
                {t('alerts.metrics.turnOff')}
              </Button>
            </div>
          </>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted">{t('alerts.metrics.off')}</p>
            <Button type="button" disabled={on.isPending} onClick={() => on.mutate()}>
              {t('alerts.metrics.turnOn')}
            </Button>
          </div>
        )}
        {error && <p className="text-sm text-danger">{error.message}</p>}
      </div>
    </div>
  )
}

function Copyable({ label, value, copied, onCopy }: { label: string; value: string; copied: boolean; onCopy: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-1.5">
      <span className="text-xs font-medium tracking-wide text-muted uppercase">{label}</span>
      <div className="flex gap-2">
        <input dir="ltr" readOnly className={`${inputClass} font-mono`} value={value} onFocus={(e) => e.target.select()} />
        <Button type="button" icon={<Copy className="size-4" />} onClick={onCopy}>
          {copied ? t('alerts.metrics.copied') : t('alerts.metrics.copy')}
        </Button>
      </div>
    </div>
  )
}
