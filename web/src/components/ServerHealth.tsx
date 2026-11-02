import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CircleCheck, CircleX, Download, HardDrive, Pause, Radio, Search, Square, Wifi } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Health, type HealthPoint, type HealthProblem, type LogLevel, type LogLine } from '../api'
import i18n, { formatLocale } from '../i18n'
import { Button, inputClass } from './ui'

// Settings → Server health: how the server is doing, for an admin working
// out why something isn't. Everything comes from GET /admin/health and
// /admin/logs on this server.

const REFRESH_MS = 5000
const HOUR_MS = 3600_000

export function ServerHealth() {
  const { t } = useTranslation()
  const [logSearch, setLogSearch] = useState<{ q: string; level?: LogLevel; n: number }>({ q: '', n: 0 })
  const { data: h, error } = useQuery({ queryKey: ['health'], queryFn: api.health, refetchInterval: REFRESH_MS })
  const logRef = useRef<HTMLDivElement>(null)

  if (!h) {
    return (
      <section className="space-y-4">
        <Header />
        {error && <p className="text-sm text-danger">{t('health.cantLoad', { error: String((error as Error).message ?? error) })}</p>}
      </section>
    )
  }
  const showInLog = (p: HealthProblem) => {
    setLogSearch((s) => ({ q: p.detail || p.message, level: p.level, n: s.n + 1 }))
    logRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <section className="space-y-10">
      <div className="space-y-4">
        <Header />
        <StatusLine h={h} />
        <Tiles h={h} />
      </div>
      <Streams h={h} />
      <div className="grid gap-10 lg:grid-cols-2">
        <Problems h={h} onShow={showInLog} />
        <Jobs h={h} />
      </div>
      <div className="grid gap-10 lg:grid-cols-2">
        <Disks h={h} />
        <Details h={h} />
      </div>
      <div ref={logRef} className="scroll-mt-24">
        <Log h={h} search={logSearch} />
      </div>
    </section>
  )
}

function Header() {
  const { t } = useTranslation()
  return (
    <div>
      <h2 className="text-lg font-semibold">{t('health.title')}</h2>
      <p className="mt-1 text-sm text-muted">{t('health.intro')}</p>
    </div>
  )
}

// ---- formatting ----

const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte', 'petabyte']

export function bytes(n: number): string {
  let i = 0
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000
    i++
  }
  return new Intl.NumberFormat(formatLocale(), { style: 'unit', unit: units[i], unitDisplay: 'short', maximumFractionDigits: n < 10 && i > 0 ? 1 : 0 }).format(n)
}

/** Bytes per second as bits per second, the way connections are measured. */
export function bitrate(bytesPerSecond: number): string {
  let bits = bytesPerSecond * 8
  const steps = ['bit', 'kilobit', 'megabit', 'gigabit']
  let i = 0
  while (bits >= 1000 && i < steps.length - 1) {
    bits /= 1000
    i++
  }
  return new Intl.NumberFormat(formatLocale(), { style: 'unit', unit: `${steps[i]}-per-second`, unitDisplay: 'short', maximumFractionDigits: bits < 10 && i > 0 ? 1 : 0 }).format(bits)
}

const percent = (v: number) => new Intl.NumberFormat(formatLocale(), { style: 'percent', maximumFractionDigits: 0 }).format(v / 100)
const number = (v: number, digits = 0) => new Intl.NumberFormat(formatLocale(), { maximumFractionDigits: digits }).format(v)

/** How long, as "3d 4h", "2h 5m" or "12m". */
export function span(ms: number): string {
  const mins = Math.max(0, Math.round(ms / 60000))
  if (mins < 1) return i18n.t('health.seconds', { seconds: Math.max(0, Math.round(ms / 1000)) })
  if (mins < 60) return i18n.t('common:duration.minutes', { minutes: mins })
  const hours = Math.floor(mins / 60)
  if (hours < 24) return i18n.t('common:duration.hoursMinutes', { hours, minutes: mins % 60 })
  return i18n.t('health.days', { days: Math.floor(hours / 24), hours: hours % 24 })
}

/** When, as "5 minutes ago". */
export function ago(ms: number, now = Date.now()): string {
  const rtf = new Intl.RelativeTimeFormat(formatLocale(), { numeric: 'auto' })
  const s = Math.round((ms - now) / 1000)
  if (Math.abs(s) < 60) return rtf.format(Math.abs(s) < 10 ? 0 : s, 'second')
  const m = Math.round(s / 60)
  if (Math.abs(m) < 60) return rtf.format(m, 'minute')
  const hr = Math.round(m / 60)
  if (Math.abs(hr) < 24) return rtf.format(hr, 'hour')
  return rtf.format(Math.round(hr / 24), 'day')
}

const timeOfDay = (ms: number) => new Intl.DateTimeFormat(formatLocale(), { hour: 'numeric', minute: '2-digit', second: '2-digit' }).format(ms)

const card = 'rounded-xl border border-line/60 bg-surface/50'

// ---- status and tiles ----

function StatusLine({ h }: { h: Health }) {
  const { t } = useTranslation()
  const recent = h.system.history.reduce((n, p) => n + p.problems, 0)
  const errors = h.problems.filter((p) => p.level === 'error' && Date.now() - p.last < HOUR_MS).length
  const restarting = h.server.state === 'restarting'
  const bad = restarting || !!h.databaseError || h.disks.some((d) => !d.reachable)
  const Icon = bad || errors ? CircleX : recent ? AlertTriangle : CircleCheck
  return (
    <div className={`flex flex-wrap items-center gap-x-4 gap-y-1 ${card} px-4 py-3 text-sm`}>
      <span className="flex items-center gap-2 font-medium">
        <Icon className={`size-4 ${bad || errors ? 'text-danger' : recent ? 'text-text' : 'text-accent'}`} aria-hidden />
        {restarting
          ? t('health.status.restarting')
          : h.databaseError
            ? t('health.status.database')
            : h.disks.some((d) => !d.reachable)
              ? t('health.status.disk')
              : recent
                ? t('health.status.problems', { count: recent })
                : t('health.status.ok')}
      </span>
      <span className="text-muted">
        {h.server.name} · {t('health.upFor', { time: span(Date.now() - h.system.started) })}{` · Matinee ${shortVersion(h.server.version)}`}
      </span>
      <span className="ms-auto text-xs text-faint">{t('health.updates')}</span>
    </div>
  )
}

const shortVersion = (v: string) => v.replace(/^main-([0-9a-f]{7})[0-9a-f]+$/, 'main-$1')

function Tiles({ h }: { h: Health }) {
  const { t } = useTranslation()
  const s = h.system
  const history = s.history
  const memCap = s.memLimit || s.memTotal || 0
  const out = history.length ? history[history.length - 1].out : 0
  const playing = h.streams.filter((x) => !x.playing.paused).length
  const scope = t(`health.scope.${s.scope}`)
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <Tile
        label={t('health.tiles.cpu')}
        value={percent(s.cpu)}
        detail={[s.hostCpu !== undefined && t('health.tiles.machine', { value: percent(s.hostCpu) }), s.load?.length && t('health.tiles.load', { load: s.load.map((l) => number(l, 2)).join(' ') })]
          .filter(Boolean)
          .join(' · ')}
        hint={s.cpuLimit ? `${scope} ${t('health.tiles.cpuLimit', { cores: number(s.cpuLimit, 1) })}` : scope}
        points={history}
        value_={(p) => p.cpu}
        max={100}
        format={percent}
      />
      <Tile
        label={t('health.tiles.memory')}
        value={bytes(s.memory)}
        detail={memCap ? t(s.memLimit ? 'health.tiles.ofLimit' : 'health.tiles.of', { total: bytes(memCap) }) : ''}
        hint={scope}
        points={history}
        value_={(p) => p.mem}
        max={Math.max(...history.map((p) => p.mem), 1) * 1.25}
        format={bytes}
      />
      <Tile
        label={t('health.tiles.sending')}
        value={bitrate(out)}
        detail={t('health.tiles.sentTotal', { total: bytes(s.bytesOut) })}
        points={history}
        value_={(p) => p.out}
        max={Math.max(...history.map((p) => p.out), 125_000) * 1.15}
        format={bitrate}
      />
      <Tile
        label={t('health.tiles.playing')}
        value={number(playing)}
        detail={t('health.tiles.transcodes', { count: h.transcodes.length })}
        points={history}
        value_={(p) => p.streams}
        max={Math.max(...history.map((p) => p.streams), 2) * 1.2}
        format={(v) => number(v)}
      />
    </div>
  )
}

function Tile({
  label,
  value,
  detail,
  hint,
  points,
  value_,
  max,
  format,
}: {
  label: string
  value: string
  detail?: string | false | 0
  hint?: string
  points: HealthPoint[]
  value_: (p: HealthPoint) => number
  max: number
  format: (v: number) => string
}) {
  return (
    <div className={`${card} flex min-w-0 flex-col p-4`} title={hint}>
      <div className="text-xs font-medium uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      <div className="min-h-5 truncate text-xs text-muted">{detail || ''}</div>
      <Sparkline label={label} points={points} value={value_} max={max} format={format} />
    </div>
  )
}

/** The last hour of one reading, with the value under the pointer. */
function Sparkline({ label, points, value, max, format }: { label: string; points: HealthPoint[]; value: (p: HealthPoint) => number; max: number; format: (v: number) => string }) {
  const { t } = useTranslation()
  const [hover, setHover] = useState<number | null>(null)
  const W = 240
  const H = 48
  const end = points.length ? points[points.length - 1].t : Date.now()
  // The last hour, or less while the server hasn't been up that long.
  const start = Math.min(points.length ? points[0].t : end, end - 60_000)
  const range = end - start
  const x = (ms: number) => ((ms - start) / range) * W
  const y = (v: number) => H - 2 - (Math.min(v, max) / max) * (H - 4)
  const line = points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(value(p)).toFixed(1)}`).join('')
  const area = points.length ? `${line}L${x(points[points.length - 1].t).toFixed(1)},${H}L${x(points[0].t).toFixed(1)},${H}Z` : ''
  const hp = hover !== null ? points[hover] : undefined
  if (points.length < 2) return <p className="mt-3 flex h-12 items-end text-xs text-faint">{t('health.noHistory')}</p>
  return (
    <div className="relative mt-3">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="block h-12 w-full overflow-visible"
        role="img"
        aria-label={t('health.chartLabel', { name: label })}
        onPointerMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect()
          const ms = start + ((e.clientX - r.left) / r.width) * range
          let best = 0
          for (let i = 1; i < points.length; i++) if (Math.abs(points[i].t - ms) < Math.abs(points[best].t - ms)) best = i
          setHover(best)
        }}
        onPointerLeave={() => setHover(null)}
      >
        <line x1={0} x2={W} y1={H - 0.5} y2={H - 0.5} className="stroke-line" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        <path d={area} className="fill-accent/15" />
        <path d={line} className="fill-none stroke-accent" strokeWidth={2} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
        {hp && <line x1={x(hp.t)} x2={x(hp.t)} y1={0} y2={H} className="stroke-muted" strokeWidth={1} vectorEffect="non-scaling-stroke" />}
      </svg>
      {hp && (
        <div
          className="pointer-events-none absolute bottom-full z-10 mb-1 -translate-x-1/2 whitespace-nowrap rounded-md border border-line bg-raised px-2 py-1 text-xs shadow-lg"
          style={{ left: `${Math.min(85, Math.max(15, (x(hp.t) / W) * 100))}%` }}
        >
          <span className="font-medium tabular-nums">{format(value(hp))}</span> <span className="text-muted">{timeOfDay(hp.t)}</span>
        </div>
      )}
      <div className="mt-1 flex justify-between text-[10px] text-faint">
        <span>{range > HOUR_MS - 60_000 ? t('health.hourAgo') : ago(start, end)}</span>
        <span>{t('health.now')}</span>
      </div>
    </div>
  )
}

// ---- what's playing ----

function Streams({ h }: { h: Health }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const stop = useMutation({ mutationFn: api.stopTranscode, onSuccess: () => qc.invalidateQueries({ queryKey: ['health'] }) })
  const title = (it: { title: string; showTitle?: string; season?: number; episode?: number }) =>
    it.showTitle ? `${it.showTitle} · ${it.season ? `S${it.season}E${it.episode ?? ''} · ` : ''}${it.title}` : it.title
  return (
    <div className="grid gap-10 lg:grid-cols-2">
      <div className="space-y-3">
        <h3 className="font-semibold">{t('health.streams.title')}</h3>
        <div className={`${card} divide-y divide-line/60`}>
          {h.streams.length === 0 && <p className="p-4 text-sm text-muted">{t('health.streams.none')}</p>}
          {h.streams.map((s) => {
            const p = s.playing
            const pct = p.durationMs ? Math.min(100, (p.positionMs / p.durationMs) * 100) : 0
            return (
              <div key={s.id} className="space-y-2 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-medium">{title(p.item)}</div>
                    <div className="truncate text-sm text-muted">
                      {s.user.name} · {s.name || s.platform}
                      {s.app && s.version ? ` · ${s.app} ${s.version}` : ''}
                    </div>
                  </div>
                  <div className="flex shrink-0 flex-wrap justify-end gap-1.5 text-xs">
                    {p.paused && <Badge icon={<Pause className="size-3" />}>{t('health.streams.paused')}</Badge>}
                    {p.buffering && <Badge>{t('health.streams.buffering')}</Badge>}
                    {s.remote && <Badge icon={<Wifi className="size-3" />}>{t('health.streams.away')}</Badge>}
                    <Badge strong={p.transcoding}>{p.transcoding ? t('health.streams.transcoding') : t('health.streams.direct')}</Badge>
                  </div>
                </div>
                {p.durationMs ? (
                  <div className="h-1 overflow-hidden rounded-full bg-text/10">
                    <div className="h-full bg-accent" style={{ width: `${pct}%` }} />
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      </div>
      <div className="space-y-3">
        <h3 className="font-semibold">{t('health.transcodes.title')}</h3>
        <div className={`${card} divide-y divide-line/60`}>
          {h.transcodes.length === 0 && <p className="p-4 text-sm text-muted">{t('health.transcodes.none')}</p>}
          {h.transcodes.map((x) => (
            <div key={x.id} className="flex items-start justify-between gap-3 p-4">
              <div className="min-w-0 space-y-0.5">
                <div className="truncate font-medium">{x.title ? title({ title: x.title, showTitle: x.showTitle }) : `#${x.itemId}`}</div>
                <div className="text-sm text-muted">
                  {[
                    x.user,
                    t(`health.transcodes.kind.${x.kind}`),
                    x.kind === 'video' && (x.accel ? t(`health.transcodes.mode.${x.mode}`, { name: x.accel }) : t('health.transcodes.mode.cpu')),
                    x.quality?.length && x.quality.join(', '),
                    x.burnSub && t('health.transcodes.burnSub'),
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </div>
                <div className="text-xs text-faint">
                  {t('health.transcodes.started', { time: ago(x.started) })}
                  {x.pid ? ` · PID ${x.pid}` : ''}
                </div>
              </div>
              <Button
                icon={<Square className="size-3.5" />}
                title={t('health.transcodes.stopHint')}
                disabled={stop.isPending}
                onClick={() => stop.mutate(x.id)}
              >
                {t('health.transcodes.stop')}
              </Button>
            </div>
          ))}
        </div>
        <HWLine h={h} />
      </div>
    </div>
  )
}

function HWLine({ h }: { h: Health }) {
  const { t } = useTranslation()
  if (!h.hw) return null
  const a = h.hw.active
  return (
    <p className="text-xs text-muted">
      {h.hw.detecting
        ? t('settings.transcoding.testing')
        : a
          ? `${t('settings.transcoding.convertingWith')} ${a.detail ? t('settings.transcoding.activeOn', { name: a.name, detail: a.detail }) : t('settings.transcoding.active', { name: a.name })}`
          : h.hw.problem || t('health.transcodes.cpuOnly')}
    </p>
  )
}

function Badge({ children, icon, strong }: { children: ReactNode; icon?: ReactNode; strong?: boolean }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 ${strong ? 'border-accent/60 text-text' : 'border-line text-muted'}`}>
      {icon}
      {children}
    </span>
  )
}

// ---- problems ----

function Problems({ h, onShow }: { h: Health; onShow: (p: HealthProblem) => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<string | null>(null)
  const [all, setAll] = useState(false)
  const shown = all ? h.problems : h.problems.slice(0, 8)
  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{t('health.problems.title')}</h3>
      <div className={`${card} divide-y divide-line/60`}>
        {h.problems.length === 0 && <p className="p-4 text-sm text-muted">{t('health.problems.none')}</p>}
        {shown.map((p) => {
          const key = `${p.level}${p.message}${p.detail}`
          const Icon = p.level === 'error' ? CircleX : AlertTriangle
          return (
            <div key={key} className="p-4 text-sm">
              <button className="flex w-full items-start gap-3 text-start" aria-expanded={open === key} onClick={() => setOpen(open === key ? null : key)}>
                <Icon className={`mt-0.5 size-4 shrink-0 ${p.level === 'error' ? 'text-danger' : 'text-muted'}`} aria-label={t(`health.level.${p.level}`)} />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium break-words">{p.message}</span>
                  {p.detail && <span className={`block text-muted break-words ${open === key ? '' : 'line-clamp-2'}`}>{p.detail}</span>}
                  <span className="mt-1 block text-xs text-faint">
                    {t(`health.level.${p.level}`)} · {t('health.problems.times', { count: p.count })} · {t('health.problems.last', { time: ago(p.last) })}
                  </span>
                </span>
              </button>
              {open === key && (
                <div className="mt-3 ms-7 space-y-3">
                  {p.attrs && p.attrs.length > 0 && (
                    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 rounded-lg bg-text/5 p-3 font-mono text-xs">
                      {p.attrs.map((a, i) => (
                        <Attr key={i} k={a.key} v={a.value} />
                      ))}
                    </dl>
                  )}
                  <Button onClick={() => onShow(p)} icon={<Search className="size-3.5" />}>
                    {t('health.problems.showInLog')}
                  </Button>
                </div>
              )}
            </div>
          )
        })}
      </div>
      {!all && h.problems.length > shown.length && (
        <button className="text-sm text-muted underline hover:text-text" onClick={() => setAll(true)}>
          {t('health.problems.more', { count: h.problems.length - shown.length })}
        </button>
      )}
    </div>
  )
}

function Attr({ k, v }: { k: string; v: string }) {
  return (
    <>
      <dt className="text-muted">{k}</dt>
      <dd className="whitespace-pre-wrap break-all">{v}</dd>
    </>
  )
}

// ---- background jobs ----

function Jobs({ h }: { h: Health }) {
  const { t } = useTranslation()
  const a = h.jobs.analysis
  const dl = h.jobs.downloads
  const preparing = (dl.queued ?? 0) + (dl.preparing ?? 0)
  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{t('health.jobs.title')}</h3>
      <div className={`${card} divide-y divide-line/60`}>
        {h.jobs.scans.map((s) => (
          <Job
            key={s.libraryId}
            label={t('health.jobs.scan', { library: s.name })}
            busy={s.scanning}
            status={
              s.scanning && s.since
                ? t('health.jobs.scanning', { time: span(Date.now() - s.since) })
                : s.last
                  ? s.last.error
                    ? t('health.jobs.scanFailed', { error: s.last.error })
                    : t('health.jobs.lastScan', { time: ago(s.last.started + s.last.tookMs), took: span(s.last.tookMs) })
                  : t('health.jobs.notScanned')
            }
            detail={
              !s.scanning && s.last && !s.last.error
                ? t('health.jobs.scanCounts', { added: s.last.added, updated: s.last.updated, removed: s.last.removed, failed: s.last.failed + s.last.unplayable })
                : undefined
            }
            bad={!!s.last?.error}
          />
        ))}
        {a && (
          <>
            <Job
              label={t('analysis.title')}
              busy={a.state === 'analyzing'}
              status={a.mode === 'off' ? t('analysis.state.paused') : t('analysis.progress', { done: a.done, total: a.total })}
              progress={a.total ? a.done / a.total : 1}
              detail={a.state === 'waiting' ? t('analysis.state.waiting') : a.state === 'slowDisk' ? t('analysis.state.slowDisk') : a.current}
            />
            {a.intros && (
              <Job
                label={t('analysis.intros.label')}
                busy={a.state === 'intros'}
                status={a.intros.on ? t('analysis.progress', { done: a.intros.done, total: a.intros.total }) : t('analysis.mode.off.label')}
                progress={a.intros.on && a.intros.total ? a.intros.done / a.intros.total : undefined}
              />
            )}
            {a.thumbnails && (
              <Job
                label={t('analysis.thumbnails.label')}
                busy={a.state === 'thumbnails'}
                status={a.thumbnails.mode === 'off' ? t('analysis.mode.off.label') : t('analysis.progress', { done: a.thumbnails.done, total: a.thumbnails.total })}
                progress={a.thumbnails.mode !== 'off' && a.thumbnails.total ? a.thumbnails.done / a.thumbnails.total : undefined}
              />
            )}
          </>
        )}
        <Job
          label={t('health.jobs.downloads')}
          busy={!!dl.preparing}
          status={preparing || dl.failed ? t('health.jobs.downloadCounts', { waiting: dl.queued ?? 0, converting: dl.preparing ?? 0, failed: dl.failed ?? 0 }) : t('health.jobs.nothing')}
          bad={!!dl.failed}
        />
        {h.jobs.importing && <Job label={t('health.jobs.importing')} busy status="" />}
      </div>
    </div>
  )
}

function Job({ label, status, detail, busy, bad, progress }: { label: string; status: string; detail?: string; busy?: boolean; bad?: boolean; progress?: number }) {
  return (
    <div className="space-y-1.5 p-4 text-sm">
      <div className="flex items-baseline justify-between gap-3">
        <span className="flex min-w-0 items-center gap-2 font-medium">
          {busy && <Radio className="size-3.5 shrink-0 animate-pulse text-accent" aria-hidden />}
          <span className="truncate">{label}</span>
        </span>
        <span className={`shrink-0 text-end tabular-nums ${bad ? 'text-danger' : 'text-muted'}`}>{status}</span>
      </div>
      {progress !== undefined && progress < 1 && (
        <div className="h-1 overflow-hidden rounded-full bg-text/10">
          <div className="h-full bg-accent" style={{ width: `${Math.floor(progress * 100)}%` }} />
        </div>
      )}
      {detail && <div className="truncate text-xs text-faint">{detail}</div>}
    </div>
  )
}

// ---- disks ----

function Disks({ h }: { h: Health }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{t('health.disks.title')}</h3>
      <div className={`${card} divide-y divide-line/60`}>
        {h.disks.map((d, i) => {
          const used = d.total ? (d.total - d.free) / d.total : 0
          const low = d.reachable && d.total > 0 && (d.free / d.total < 0.05 || d.free < 2e9)
          return (
            <div key={i} className="space-y-2 p-4 text-sm">
              <div className="flex items-baseline justify-between gap-3">
                <span className="flex items-center gap-2 font-medium">
                  <HardDrive className="size-4 text-muted" aria-hidden />
                  {d.reachable ? t('health.disks.free', { free: bytes(d.free), total: bytes(d.total) }) : t('health.disks.unreachableTitle')}
                </span>
                {low && <span className="text-danger">{t('health.disks.low')}</span>}
              </div>
              {d.reachable ? (
                <div className="h-1.5 overflow-hidden rounded-full bg-text/10" role="meter" aria-valuenow={Math.round(used * 100)} aria-valuemin={0} aria-valuemax={100}>
                  <div className={`h-full ${low ? 'bg-danger' : 'bg-accent'}`} style={{ width: `${used * 100}%` }} />
                </div>
              ) : (
                <p className="text-danger">{t('health.disks.unreachable')}</p>
              )}
              <ul className="space-y-0.5 text-xs">
                {d.uses.map((u, j) => (
                  <li key={j} className="flex min-w-0 gap-2">
                    <span className="shrink-0 text-muted">{u.use === 'library' ? t('health.disks.use.library', { library: u.library }) : t(`health.disks.use.${u.use}`)}</span>
                    <span className="min-w-0 truncate font-mono text-faint" title={u.path}>
                      {u.path}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ---- details ----

function Details({ h }: { h: Health }) {
  const { t } = useTranslation()
  const s = h.system
  const db = h.database
  const remote = h.server.remote
  const rows: [string, ReactNode][] = [
    [t('health.details.version'), `Matinee ${shortVersion(h.server.version)} · API v${h.server.apiVersion}`],
    [t('health.details.ffmpeg'), h.server.ffmpegVersion ? `${h.server.ffmpegVersion} (${h.server.ffmpeg})` : t('health.details.ffmpegMissing', { path: h.server.ffmpeg ?? 'ffmpeg' })],
    [t('health.details.system'), `${s.os}/${s.arch} · ${t('health.details.cores', { count: s.cores })}${s.hostname ? ` · ${s.hostname}` : ''}`],
    [t('health.details.started'), `${new Intl.DateTimeFormat(formatLocale(), { dateStyle: 'medium', timeStyle: 'short' }).format(s.started)} · PID ${s.pid}`],
    [
      t('health.details.database'),
      db ? (
        <>
          {db.kind === 'postgres' ? 'PostgreSQL' : 'SQLite'} {db.version} · {bytes(db.size)} · {t('health.details.answers', { ms: number(db.pingMs, 1) })}
          <span className="block text-xs text-faint">
            {t('health.details.dbCounts', { items: db.items, users: db.users, libraries: db.libraries })} · {t('health.details.connections', { open: db.openConns, busy: db.inUse })}
          </span>
        </>
      ) : (
        <span className="text-danger">{t('health.details.dbError', { error: h.databaseError })}</span>
      ),
    ],
    [t('health.details.requests'), t('health.details.requestCounts', { requests: s.requests, errors: s.serverErrors })],
    [t('health.details.logged'), t('health.details.loggedCounts', { warnings: s.warnings, errors: s.errors })],
    [t('health.details.rescans'), h.server.scanIntervalMs ? t('health.details.every', { time: span(h.server.scanIntervalMs) }) : t('analysis.mode.off.label')],
    ...(remote ? ([[t('health.details.remote'), remote.enabled ? t(`health.details.remoteState.${remote.state}`, { defaultValue: remote.state }) + (remote.problem ? ` · ${remote.problem}` : '') : t('analysis.mode.off.label')]] as [string, ReactNode][]) : []),
    [t('health.details.runtime'), `${s.goVersion} · ${t('health.details.heap', { heap: bytes(s.goHeap), goroutines: s.goroutines })}`],
  ]
  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{t('health.details.title')}</h3>
      <dl className={`${card} divide-y divide-line/60 text-sm`}>
        {rows.map(([k, v]) => (
          <div key={k} className="grid gap-1 p-4 sm:grid-cols-[10rem_minmax(0,1fr)] sm:gap-4">
            <dt className="text-muted">{k}</dt>
            <dd className="min-w-0 break-words">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

// ---- log ----

const levels: (LogLevel | 'all')[] = ['all', 'warn', 'error']

function Log({ h, search }: { h: Health; search: { q: string; level?: LogLevel; n: number } }) {
  const { t } = useTranslation()
  const [text, setText] = useState(search.q)
  const [q, setQ] = useState(search.q)
  const [level, setLevel] = useState<LogLevel | 'all'>(search.level ?? 'all')
  const [live, setLive] = useState(true)
  useEffect(() => {
    if (!search.n) return
    setText(search.q)
    setQ(search.q)
    setLevel(search.level ?? 'all')
  }, [search])
  // Search a moment after typing stops.
  useEffect(() => {
    const id = setTimeout(() => setQ(text.trim()), 300)
    return () => clearTimeout(id)
  }, [text])
  const query = { q, level: level === 'all' ? undefined : level, limit: 200 }
  const logs = useInfiniteQuery({
    queryKey: ['logs', query.q, query.level],
    queryFn: ({ pageParam }) => api.logs({ ...query, before: pageParam || undefined }),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.more && last.lines.length ? last.lines[last.lines.length - 1].seq : undefined),
    refetchInterval: live ? REFRESH_MS : false,
  })
  const lines = useMemo(() => logs.data?.pages.flatMap((p) => p.lines) ?? [], [logs.data])
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h3 className="font-semibold">{t('health.log.title')}</h3>
          <p className="mt-1 text-sm text-muted">{t('health.log.intro', { count: h.logLines })}</p>
        </div>
        <a href={api.logsDownloadURL(query)} download>
          <Button icon={<Download className="size-4" />} tabIndex={-1}>
            {t('health.log.download')}
          </Button>
        </a>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <label className="relative min-w-0 flex-1 basis-64">
          <span className="sr-only">{t('health.log.search')}</span>
          <Search className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-faint" aria-hidden />
          <input type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder={t('health.log.search')} className={`${inputClass} ps-9`} />
        </label>
        <div className="flex gap-1.5" role="radiogroup" aria-label={t('health.log.level')}>
          {levels.map((l) => (
            <button
              key={l}
              role="radio"
              aria-checked={level === l}
              onClick={() => setLevel(l)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${level === l ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'}`}
            >
              {t(`health.log.levels.${l}`)}
            </button>
          ))}
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm text-muted">
          <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} className="accent-[var(--color-accent)]" />
          {t('health.log.live')}
        </label>
      </div>
      <div className={`${card} overflow-hidden`}>
        <div className="max-h-[32rem] overflow-auto font-mono text-xs leading-relaxed" role="log">
          {lines.length === 0 && !logs.isPending && <p className="p-4 font-sans text-sm text-muted">{t('health.log.empty')}</p>}
          {lines.map((l) => (
            <LogRow key={l.seq} line={l} />
          ))}
          {logs.hasNextPage && (
            <div className="p-3 font-sans">
              <Button onClick={() => logs.fetchNextPage()} disabled={logs.isFetchingNextPage}>
                {t('health.log.older')}
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function LogRow({ line }: { line: LogLine }) {
  const { t } = useTranslation()
  const color = line.level === 'error' ? 'text-danger' : line.level === 'warn' ? 'text-text' : 'text-faint'
  return (
    <div className={`flex gap-3 border-b border-line/40 px-3 py-1.5 last:border-0 ${line.level === 'error' ? 'bg-danger/5' : ''}`}>
      <span className="shrink-0 text-faint tabular-nums" title={new Date(line.time).toISOString()}>
        {timeOfDay(line.time)}
      </span>
      <span className={`w-12 shrink-0 font-semibold uppercase ${color}`}>{line.level === 'warn' || line.level === 'error' ? t(`health.level.${line.level}`) : line.level}</span>
      <span className="min-w-0 flex-1 break-words">
        <span className="text-text">{line.message}</span>
        {line.attrs?.map((a, i) => (
          <span key={i} className="whitespace-pre-wrap">
            {' '}
            <span className="text-muted">{a.key}=</span>
            <span className="break-all">{a.value}</span>
          </span>
        ))}
      </span>
    </div>
  )
}
