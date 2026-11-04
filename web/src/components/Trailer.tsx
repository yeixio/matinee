import { useQuery } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { Clapperboard, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, trailerFileUrl, type Item, type Trailer } from '../api'
import { clock } from '../format'
import { canDirectPlay, type Playable } from '../player/direct'
import { Button, Spinner } from './ui'

// TrailerButton shows on movies, shows and seasons that have a trailer, from
// a file next to the media or (unless an admin turned it off) from YouTube.
export function TrailerButton({ item }: { item: Item }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const has = item.kind === 'movie' || item.kind === 'show' || item.kind === 'season'
  const { data: trailers } = useQuery({
    queryKey: ['trailers', item.id],
    queryFn: () => api.trailers(item.id),
    enabled: has,
    staleTime: 10 * 60_000,
  })
  if (!trailers?.length) return null
  return (
    <>
      <Button icon={<Clapperboard className="size-4" />} onClick={() => setOpen(true)}>
        {t('trailer.trailer')}
      </Button>
      {open && <TrailerDialog item={item} trailers={trailers} onClose={() => setOpen(false)} />}
    </>
  )
}

function TrailerDialog({ item, trailers, onClose }: { item: Item; trailers: Trailer[]; onClose: () => void }) {
  const { t: tr } = useTranslation()
  const [current, setCurrent] = useState(trailers[0])
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [onClose])

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={tr('trailer.named', { title: item.title })}
      className="animate-fade fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 bg-ink/95 p-4 backdrop-blur md:p-10"
      onClick={onClose}
    >
      <div className="flex w-full max-w-6xl items-center justify-between gap-4" onClick={(e) => e.stopPropagation()}>
        <div className="min-w-0">
          <div className="truncate font-display text-lg font-semibold">{item.title}</div>
          <div className="truncate text-sm text-muted">{current.name}</div>
        </div>
        <button onClick={onClose} aria-label={tr('close')} className="grid size-10 shrink-0 place-items-center rounded-full bg-text/10 hover:bg-text/20">
          <X className="size-5" />
        </button>
      </div>
      <div
        className="aspect-video w-full max-w-6xl overflow-hidden rounded-xl bg-black shadow-2xl ring-1 ring-text/10"
        style={{ maxHeight: 'calc(100vh - 12rem)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {current.source === 'local' ? (
          <LocalClip
            key={current.id}
            clip={current}
            fileUrl={trailerFileUrl(item.id, current.id)}
            startHls={() => api.startTrailerHls(item.id, current.id)}
            wontPlay={tr('trailer.wontPlay')}
            onEnded={onClose}
          />
        ) : (
          <iframe
            key={current.id}
            className="size-full"
            // The privacy-enhanced player sets no cookies until the trailer plays.
            src={`https://www.youtube-nocookie.com/embed/${encodeURIComponent(current.key ?? '')}?autoplay=1&rel=0&modestbranding=1`}
            title={current.name}
            allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
            allowFullScreen
          />
        )}
      </div>
      {trailers.length > 1 && (
        <div className="flex max-w-6xl flex-wrap justify-center gap-2" onClick={(e) => e.stopPropagation()}>
          {trailers.map((t) => (
            <button
              key={t.id}
              onClick={() => setCurrent(t)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                t.id === current.id ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint hover:text-text'
              }`}
            >
              {t.name}
              {t.durationMs ? <span className="ms-1.5 text-faint tabular-nums">{clock(t.durationMs / 1000)}</span> : null}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// LocalClip plays a trailer or another extra's file straight from the server
// when the browser can, and converted otherwise, the way the main player does.
export function LocalClip({
  clip,
  fileUrl,
  startHls,
  wontPlay,
  onEnded,
}: {
  clip: Playable
  fileUrl: string
  startHls: () => Promise<{ url: string }>
  wontPlay: string
  onEnded: () => void
}) {
  const video = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  // The newest of these, without starting playback again when they change.
  const latest = useRef({ startHls, wontPlay })
  latest.current = { startHls, wontPlay }

  useEffect(() => {
    const el = video.current
    if (!el) return
    let hls: Hls | undefined
    let cancelled = false
    const fail = () => !cancelled && setError(latest.current.wontPlay)
    const start = async () => {
      if (canDirectPlay(clip, el)) {
        el.src = fileUrl
      } else {
        const { url } = await latest.current.startHls()
        const { default: Hls } = await import('hls.js')
        if (cancelled) return
        if (Hls.isSupported()) {
          hls = new Hls()
          hls.loadSource(url)
          hls.attachMedia(el)
          hls.on(Hls.Events.ERROR, (_, d) => d.fatal && fail())
        } else {
          el.src = url // Safari plays HLS natively
        }
      }
      el.play().catch(() => {})
    }
    start().catch(fail)
    return () => {
      cancelled = true
      hls?.destroy()
      el.removeAttribute('src')
      el.load()
    }
  }, [clip, fileUrl])

  return (
    <div className="relative size-full">
      <video
        ref={video}
        className="size-full"
        controls
        playsInline
        onPlaying={() => setLoading(false)}
        onEnded={onEnded}
        onError={() => setError(wontPlay)}
      />
      {loading && !error && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <Spinner />
        </div>
      )}
      {error && <div className="absolute inset-0 grid place-items-center text-sm text-muted">{error}</div>}
    </div>
  )
}
