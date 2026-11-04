import { useQuery, useQueryClient } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { ChevronLeft, ChevronRight, LayoutGrid, Rows3, Search, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type LiveListing, type LiveProgram, type User } from '../api'
import { ChannelLogo } from '../components/ChannelLogo'
import { Button, EmptyState, Spinner } from '../components/ui'
import { formatLocale } from '../i18n'
import { halfHour, matches, placement, progress } from '../live'

const time = (s: number) => new Date(s * 1000).toLocaleTimeString(formatLocale(), { hour: 'numeric', minute: '2-digit' })

// Ticks every 30 seconds, for what's on now.
function useNow() {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => {
    const id = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 30_000)
    return () => clearInterval(id)
  }, [])
  return now
}

// Live TV: the channels that play, by category or in a guide, and the one
// playing above them (?channel=).
export default function LiveTV() {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data, isPending, error } = useQuery({ queryKey: ['live', 'channels'], queryFn: api.liveChannels, refetchInterval: 5 * 60_000 })
  const [params, setParams] = useSearchParams()
  const playing = params.get('channel') ?? ''
  const view = params.get('view') === 'guide' ? 'guide' : 'channels'
  const [q, setQ] = useState('')
  const [category, setCategory] = useState('')
  const set = (k: string, v: string) =>
    setParams(
      (p) => {
        if (v) p.set(k, v)
        else p.delete(k)
        return p
      },
      { replace: k !== 'channel' },
    )

  const channels = useMemo(() => (data?.channels ?? []).filter((c) => matches(c, q, category)), [data, q, category])
  const current = data?.channels.find((c) => c.id === playing)

  if (isPending) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  if (error) return <EmptyState title={t('common:live.cantLoad')} />
  if (!data.channels.length) {
    return (
      <EmptyState title={t('common:live.none')}>
        {me?.isAdmin ? (
          <Link to="/settings/live-tv" className="text-accent underline">
            {t('common:live.addSource')}
          </Link>
        ) : (
          t('common:live.askAdmin')
        )}
      </EmptyState>
    )
  }
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="me-auto text-2xl font-bold tracking-tight">{t('common:live.title')}</h1>
        <div className="flex rounded-lg bg-text/5 p-1" role="tablist">
          {(['channels', 'guide'] as const).map((v) => (
            <button
              key={v}
              role="tab"
              aria-selected={view === v}
              onClick={() => set('view', v === 'guide' ? 'guide' : '')}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm ${view === v ? 'bg-surface text-text shadow' : 'text-muted hover:text-text'}`}
            >
              {v === 'guide' ? <Rows3 className="size-4" /> : <LayoutGrid className="size-4" />}
              {t(`common:live.${v}`)}
            </button>
          ))}
        </div>
      </div>
      {playing && <LivePlayer key={playing} id={playing} listing={current} onClose={() => set('channel', '')} />}
      <div className="flex flex-wrap items-center gap-2">
        <label className="relative">
          <Search className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t('common:live.find')}
            aria-label={t('common:live.find')}
            className="h-9 w-56 rounded-full border border-line bg-surface ps-9 pe-3 text-sm focus:border-accent focus:outline-none"
          />
        </label>
        <div className="scrollbar-none flex min-w-0 flex-1 gap-1.5 overflow-x-auto">
          {['', ...data.categories].map((c) => (
            <button
              key={c || 'all'}
              onClick={() => setCategory(c)}
              className={`shrink-0 rounded-full px-3 py-1.5 text-sm ${category === c ? 'bg-accent text-on-accent' : 'bg-text/5 text-muted hover:text-text'}`}
            >
              {c || t('common:live.all')}
            </button>
          ))}
        </div>
      </div>
      {!channels.length ? (
        <EmptyState title={t('common:live.noMatch')} />
      ) : view === 'guide' ? (
        <Guide channels={channels} playing={playing} onPlay={(id) => set('channel', id)} />
      ) : (
        <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))' }}>
          {channels.map((c) => (
            <ChannelCard key={c.id} channel={c} on={c.id === playing} onPlay={() => set('channel', c.id)} />
          ))}
        </div>
      )}
    </div>
  )
}

function ChannelCard({ channel, on, onPlay }: { channel: LiveListing; on: boolean; onPlay: () => void }) {
  const { t } = useTranslation()
  const now = useNow()
  return (
    <button
      onClick={onPlay}
      className={`flex items-start gap-3 rounded-xl border p-3 text-start transition-colors ${on ? 'border-accent bg-accent/10' : 'border-line/60 bg-surface/50 hover:bg-text/5'}`}
    >
      <ChannelLogo channel={channel} />
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium">
          {channel.number && <span className="me-1.5 text-muted tabular-nums">{channel.number}</span>}
          {channel.name}
        </p>
        {channel.now ? (
          <>
            <p className="truncate text-sm text-muted">{channel.now.title}</p>
            <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-text/10">
              <div className="h-full bg-accent" style={{ width: `${progress(channel.now, now) * 100}%` }} />
            </div>
            {channel.next && <p className="mt-1 truncate text-xs text-faint">{t('common:live.next', { time: time(channel.next.start), title: channel.next.title })}</p>}
          </>
        ) : (
          <p className="truncate text-sm text-faint">{channel.categories?.join(', ') || t('common:live.noGuide')}</p>
        )}
      </div>
    </button>
  )
}

// The player: the channel through the server, live, with what's on.
function LivePlayer({ id, listing, onClose }: { id: string; listing?: LiveListing; onClose: () => void }) {
  const { t } = useTranslation()
  const video = useRef<HTMLVideoElement>(null)
  const [state, setState] = useState<'loading' | 'playing' | 'failed'>('loading')
  const qc = useQueryClient()
  const now = useNow()

  useEffect(() => {
    const el = video.current
    if (!el) return
    let hls: Hls | undefined
    let cancelled = false
    const start = async () => {
      const { url } = await api.playLiveChannel(id)
      if (cancelled) return
      const { default: Hls } = await import('hls.js')
      if (cancelled) return
      if (Hls.isSupported()) {
        hls = new Hls({ lowLatencyMode: false, liveSyncDurationCount: 3, manifestLoadingMaxRetry: 3, levelLoadingMaxRetry: 3, fragLoadingMaxRetry: 4 })
        hls.on(Hls.Events.ERROR, (_, d) => {
          if (!d.fatal) return
          if (d.type === Hls.ErrorTypes.MEDIA_ERROR) hls?.recoverMediaError()
          else setState('failed')
        })
        hls.loadSource(url)
        hls.attachMedia(el)
      } else {
        el.src = url // Safari plays HLS itself
      }
      el.play().catch(() => {})
    }
    start().catch(() => !cancelled && setState('failed'))
    return () => {
      cancelled = true
      hls?.destroy()
      el.removeAttribute('src')
      el.load()
    }
  }, [id])

  // If the list didn't have it (a hidden channel an admin tries), ask for it.
  const { data: detail } = useQuery({ queryKey: ['live', 'channel', id], queryFn: () => api.liveChannel(id), enabled: !listing })
  const channel = listing ?? detail?.channel
  const onNow = listing?.now ?? detail?.programs.find((p) => p.start <= now && p.stop > now)

  return (
    <section className="overflow-hidden rounded-2xl border border-line/60 bg-black md:grid md:grid-cols-[minmax(0,1fr)_320px]">
      <div className="relative aspect-video bg-black">
        <video ref={video} className="size-full" controls playsInline autoPlay onPlaying={() => setState('playing')} onError={() => setState('failed')} />
        {state === 'loading' && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <Spinner />
          </div>
        )}
        {state === 'failed' && (
          <div className="absolute inset-0 grid place-items-center bg-black/80 p-6 text-center text-white">
            <div>
              <p className="font-medium">{t('common:live.wontPlay')}</p>
              <p className="mt-1 text-sm text-white/70">{t('common:live.wontPlayHint')}</p>
              <Button className="mt-4" onClick={() => qc.invalidateQueries({ queryKey: ['live'] })}>
                {t('common:live.tryAgain')}
              </Button>
            </div>
          </div>
        )}
      </div>
      <div className="space-y-3 bg-surface p-4">
        <div className="flex items-start gap-3">
          {channel && <ChannelLogo channel={channel} className="size-10" />}
          <div className="min-w-0 flex-1">
            <p className="text-xs font-semibold tracking-wide text-accent uppercase">{t('common:live.live')}</p>
            <p className="truncate font-semibold">{channel?.name}</p>
          </div>
          <button onClick={onClose} className="rounded-full p-1.5 text-muted hover:bg-text/5 hover:text-text" aria-label={t('close')}>
            <X className="size-5" />
          </button>
        </div>
        {onNow ? (
          <div>
            <p className="font-medium">{onNow.title}</p>
            <p className="text-xs text-muted">
              {time(onNow.start)} – {time(onNow.stop)}
              {onNow.subtitle && ` · ${onNow.subtitle}`}
            </p>
            {onNow.description && <p className="mt-2 line-clamp-6 text-sm text-muted">{onNow.description}</p>}
          </div>
        ) : (
          <p className="text-sm text-muted">{t('common:live.noGuide')}</p>
        )}
        {listing?.next && <p className="text-sm text-faint">{t('common:live.next', { time: time(listing.next.start), title: listing.next.title })}</p>}
      </div>
    </section>
  )
}

const guideHours = 4
const pxPerHour = 240

// The guide: a row per channel, shows along four hours from the half hour.
function Guide({ channels, playing, onPlay }: { channels: LiveListing[]; playing: string; onPlay: (id: string) => void }) {
  const { t } = useTranslation()
  const now = useNow()
  const [offset, setOffset] = useState(0) // in half hours from now's
  const from = halfHour(now) + offset * 1800
  const to = from + guideHours * 3600
  const { data } = useQuery({ queryKey: ['live', 'guide', from], queryFn: () => api.liveGuide(from, guideHours), staleTime: 60_000 })
  const byChannel = useMemo(() => {
    const m = new Map<string, LiveProgram[]>()
    for (const p of data?.programs ?? []) m.set(p.channelId, [...(m.get(p.channelId) ?? []), p])
    return m
  }, [data])
  const width = guideHours * pxPerHour
  const marks = Array.from({ length: guideHours * 2 }, (_, i) => from + i * 1800)
  const nowAt = placement({ start: from, stop: now }, from, to)

  return (
    <div className="overflow-hidden rounded-xl border border-line/60">
      <div className="flex items-center gap-2 border-b border-line/60 bg-surface/60 px-3 py-2">
        <Button variant="ghost" className="h-8 px-2" onClick={() => setOffset((o) => o - 4)} aria-label={t('common:live.earlier')} disabled={offset <= -4}>
          <ChevronLeft className="size-4 rtl:-scale-x-100" />
        </Button>
        <Button variant="ghost" className="h-8 px-2" onClick={() => setOffset(0)} disabled={offset === 0}>
          {t('common:live.now')}
        </Button>
        <Button variant="ghost" className="h-8 px-2" onClick={() => setOffset((o) => o + 4)} aria-label={t('common:live.later')} disabled={offset >= 88}>
          <ChevronRight className="size-4 rtl:-scale-x-100" />
        </Button>
        <span className="text-sm text-muted">{new Date(from * 1000).toLocaleDateString(formatLocale(), { weekday: 'long', day: 'numeric', month: 'short' })}</span>
      </div>
      <div className="overflow-x-auto">
        <div style={{ width: width + 200 }}>
          <div className="flex border-b border-line/60 text-xs text-muted">
            <div className="sticky start-0 z-10 w-[200px] shrink-0 bg-ink" />
            {marks.map((m) => (
              <div key={m} className="shrink-0 border-s border-line/40 px-2 py-1.5" style={{ width: pxPerHour / 2 }}>
                {time(m)}
              </div>
            ))}
          </div>
          {channels.map((c) => (
            <div key={c.id} className={`flex border-b border-line/40 ${c.id === playing ? 'bg-accent/5' : ''}`}>
              <button
                onClick={() => onPlay(c.id)}
                className="sticky start-0 z-10 flex w-[200px] shrink-0 items-center gap-2 border-e border-line/60 bg-ink px-3 py-2 text-start hover:bg-text/5"
              >
                <ChannelLogo channel={c} className="size-8" />
                <span className="truncate text-sm font-medium">{c.name}</span>
              </button>
              <div className="relative h-14 shrink-0" style={{ width }}>
                {(byChannel.get(c.id) ?? []).map((p) => {
                  const at = placement(p, from, to)
                  if (!at) return null
                  const live = p.start <= now && p.stop > now
                  return (
                    <button
                      key={p.start}
                      onClick={() => onPlay(c.id)}
                      title={`${time(p.start)} – ${time(p.stop)} ${p.title}`}
                      className={`absolute inset-y-1 overflow-hidden rounded-md px-2 py-1 text-start text-xs ${live ? 'bg-accent/20 hover:bg-accent/30' : 'bg-text/5 hover:bg-text/10'}`}
                      style={{ insetInlineStart: `calc(${at.left * 100}% + 2px)`, width: `calc(${at.width * 100}% - 4px)` }}
                    >
                      <span className="block truncate font-medium">{p.title}</span>
                      <span className="block truncate text-muted">{time(p.start)}</span>
                    </button>
                  )
                })}
                {!byChannel.get(c.id)?.length && <span className="absolute inset-y-0 start-3 flex items-center text-xs text-faint">{t('common:live.noGuide')}</span>}
                {nowAt && offset <= 0 && <div className="pointer-events-none absolute inset-y-0 w-0.5 bg-accent" style={{ insetInlineStart: `${nowAt.width * 100}%` }} />}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
