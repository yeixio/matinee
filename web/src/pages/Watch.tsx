import { useQuery, useQueryClient } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { ArrowLeft, Maximize, Minimize, Pause, Play, RotateCcw, RotateCw, Volume2, VolumeX } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, fileUrl, type ItemDetail } from '../api'
import { Spinner } from '../components/ui'
import { clock } from '../format'

const videoCodecs: Record<string, string> = {
  h264: 'avc1.640028',
  hevc: 'hvc1.1.6.L120.90',
  vp9: 'vp09.00.40.08',
  av1: 'av01.0.08M.08',
}
const audioCodecs: Record<string, string> = { aac: 'mp4a.40.2', mp3: 'mp4a.6B', opus: 'opus', vorbis: 'vorbis', flac: 'flac' }

// canDirectPlay asks the browser whether it can decode the file as-is.
// Anything uncertain goes through the server, which only re-encodes video
// when it has to.
function canDirectPlay(item: ItemDetail, el: HTMLVideoElement): boolean {
  if (!item.mimeType || item.mimeType === 'video/x-matroska') return false
  const v = item.videoCodec ? videoCodecs[item.videoCodec] : undefined
  const a = item.audioCodec ? audioCodecs[item.audioCodec] : undefined
  if ((item.videoCodec && !v) || (item.audioCodec && !a)) return false
  const codecs = [v, a].filter(Boolean).join(', ')
  return el.canPlayType(`${item.mimeType}; codecs="${codecs}"`) !== ''
}

const displayTitle = (item: ItemDetail) => {
  const show = item.parents.find((p) => p.kind === 'show')
  const season = item.parents.find((p) => p.kind === 'season')
  if (item.kind === 'episode' && show) {
    return { title: show.title, sub: `S${season?.index ?? 0} · E${item.index ?? '?'} · ${item.title}` }
  }
  return { title: item.title, sub: item.year ? String(item.year) : '' }
}

export default function Watch() {
  const id = Number(useParams().id)
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data: item } = useQuery({ queryKey: ['item', id], queryFn: () => api.item(id) })

  const video = useRef<HTMLVideoElement>(null)
  const hls = useRef<Hls | null>(null)
  const wrap = useRef<HTMLDivElement>(null)
  // HLS sessions start at an offset, so the video's clock is relative to it.
  const offset = useRef(0)
  const [mode, setMode] = useState<'direct' | 'hls'>()
  const [time, setTime] = useState(0)
  const [paused, setPaused] = useState(true)
  const [waiting, setWaiting] = useState(true)
  const [muted, setMuted] = useState(false)
  const [full, setFull] = useState(false)
  const [chrome, setChrome] = useState(true)
  const [error, setError] = useState('')
  const hideTimer = useRef<number>(undefined)

  const duration = item?.durationMs ? item.durationMs / 1000 : 0
  const position = () => offset.current + (video.current?.currentTime ?? 0)

  const startHls = useCallback(
    async (at: number) => {
      const el = video.current
      if (!el || !item) return
      setWaiting(true)
      hls.current?.destroy()
      hls.current = null
      try {
        const { url } = await api.startHls(item.id, Math.floor(at))
        offset.current = Math.floor(at)
        // hls.js is only downloaded when a file needs converting.
        const { default: Hls } = await import('hls.js')
        if (Hls.isSupported()) {
          const h = new Hls({ maxBufferLength: 60 })
          h.loadSource(url)
          h.attachMedia(el)
          h.on(Hls.Events.ERROR, (_, d) => d.fatal && setError("Playback failed. The file may be damaged or in a format the server can't convert."))
          hls.current = h
        } else {
          el.src = url // Safari plays HLS natively
        }
        setMode('hls')
        el.play().catch(() => setPaused(true))
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Playback failed')
      }
    },
    [item],
  )

  // Pick direct play or a transcode, and resume where the viewer left off.
  useEffect(() => {
    const el = video.current
    if (!item || !el || mode) return
    const resume = !item.played && item.positionMs ? item.positionMs / 1000 : 0
    if (canDirectPlay(item, el)) {
      setMode('direct')
      offset.current = 0
      el.src = fileUrl(item.id)
      el.currentTime = resume
      el.play().catch(() => setPaused(true))
    } else {
      startHls(resume)
    }
  }, [item, mode, startHls])

  useEffect(() => () => hls.current?.destroy(), [])

  // Save progress every 10 seconds and when leaving.
  useEffect(() => {
    if (!item) return
    const save = () => {
      const pos = position()
      if (pos > 1) api.setProgress(item.id, Math.round(pos * 1000)).catch(() => {})
    }
    const timer = window.setInterval(() => !video.current?.paused && save(), 10_000)
    window.addEventListener('pagehide', save)
    return () => {
      save()
      window.clearInterval(timer)
      window.removeEventListener('pagehide', save)
      qc.invalidateQueries({ queryKey: ['home'] })
      qc.invalidateQueries({ queryKey: ['item', item.id] })
      if (item.parentId) qc.invalidateQueries({ queryKey: ['children', item.parentId] })
    }
  }, [item, qc])

  const seek = (to: number) => {
    const el = video.current
    if (!el) return
    to = Math.max(0, Math.min(duration || to, to))
    if (mode === 'direct') {
      el.currentTime = to
      return
    }
    // Inside what the transcode has produced: seek in place. Further out: restart there.
    const rel = to - offset.current
    const end = el.seekable.length ? el.seekable.end(el.seekable.length - 1) : 0
    if (rel >= 0 && rel <= end) el.currentTime = rel
    else startHls(to)
  }

  const toggle = () => {
    const el = video.current
    if (!el) return
    if (el.paused) el.play()
    else el.pause()
  }

  const toggleFull = () => {
    if (document.fullscreenElement) document.exitFullscreen()
    else wrap.current?.requestFullscreen()
  }

  const poke = useCallback(() => {
    setChrome(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => !video.current?.paused && setChrome(false), 2500)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return
      poke()
      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          toggle()
          break
        case 'ArrowRight':
          seek(position() + 10)
          break
        case 'ArrowLeft':
          seek(position() - 10)
          break
        case 'f':
          toggleFull()
          break
        case 'm':
          if (video.current) video.current.muted = !video.current.muted
          break
        case 'Escape':
          if (!document.fullscreenElement) navigate(-1)
      }
    }
    const onFull = () => setFull(!!document.fullscreenElement)
    window.addEventListener('keydown', onKey)
    document.addEventListener('fullscreenchange', onFull)
    return () => {
      window.removeEventListener('keydown', onKey)
      document.removeEventListener('fullscreenchange', onFull)
    }
  })

  const t = item ? displayTitle(item) : { title: '', sub: '' }
  const frac = duration ? Math.min(1, time / duration) : 0

  return (
    <div
      ref={wrap}
      onMouseMove={poke}
      onTouchStart={poke}
      className={`fixed inset-0 z-50 bg-black select-none ${chrome ? '' : 'cursor-none'}`}
    >
      <video
        ref={video}
        className="size-full"
        playsInline
        onClick={toggle}
        onDoubleClick={toggleFull}
        onTimeUpdate={() => setTime(position())}
        onPlay={() => {
          setPaused(false)
          poke()
        }}
        onPause={() => {
          setPaused(true)
          setChrome(true)
        }}
        onWaiting={() => setWaiting(true)}
        onPlaying={() => setWaiting(false)}
        onCanPlay={() => setWaiting(false)}
        onVolumeChange={() => setMuted(!!video.current?.muted)}
        onEnded={() => item && api.setProgress(item.id, 0, true).then(() => navigate(-1))}
        onError={() => mode === 'direct' && startHls(position())}
      />

      {(waiting || !item) && !error && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <Spinner className="size-12" />
        </div>
      )}
      {error && (
        <div className="absolute inset-0 grid place-items-center p-6 text-center text-muted">
          <div>
            <p>{error}</p>
            <button onClick={() => navigate(-1)} className="mt-4 text-accent">
              Go back
            </button>
          </div>
        </div>
      )}

      <div className={`transition-opacity duration-300 ${chrome ? 'opacity-100' : 'pointer-events-none opacity-0'}`}>
        <div className="absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent p-4 pt-[max(1rem,env(safe-area-inset-top))] md:p-6">
          <button onClick={() => navigate(-1)} className="rounded-full p-2 hover:bg-white/10" aria-label="Back">
            <ArrowLeft className="size-6" />
          </button>
          <div className="min-w-0">
            <div className="truncate text-lg font-semibold">{t.title}</div>
            {t.sub && <div className="truncate text-sm text-white/70">{t.sub}</div>}
          </div>
        </div>

        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-4 pt-16 pb-[max(1rem,env(safe-area-inset-bottom))] md:px-8 md:pb-6">
          <div className="group relative mb-3 flex h-4 items-center">
            <div className="h-1 w-full rounded-full bg-white/25 transition-all group-hover:h-1.5">
              <div className="h-full rounded-full bg-accent" style={{ width: `${frac * 100}%` }} />
            </div>
            <input
              type="range"
              min={0}
              max={duration || 1}
              step={1}
              value={time}
              onChange={(e) => seek(Number(e.target.value))}
              aria-label="Seek"
              className="absolute inset-0 w-full cursor-pointer opacity-0"
            />
          </div>
          <div className="flex items-center gap-2 md:gap-4">
            <button onClick={toggle} className="rounded-full p-2 hover:bg-white/10" aria-label={paused ? 'Play' : 'Pause'}>
              {paused ? <Play className="size-7 fill-white" /> : <Pause className="size-7 fill-white" />}
            </button>
            <button onClick={() => seek(position() - 10)} className="rounded-full p-2 hover:bg-white/10" aria-label="Back 10 seconds">
              <RotateCcw className="size-6" />
            </button>
            <button onClick={() => seek(position() + 10)} className="rounded-full p-2 hover:bg-white/10" aria-label="Forward 10 seconds">
              <RotateCw className="size-6" />
            </button>
            <button
              onClick={() => video.current && (video.current.muted = !video.current.muted)}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label={muted ? 'Unmute' : 'Mute'}
            >
              {muted ? <VolumeX className="size-6" /> : <Volume2 className="size-6" />}
            </button>
            <div className="text-sm text-white/80 tabular-nums">
              {clock(time)} <span className="text-white/40">/ {clock(duration)}</span>
            </div>
            <div className="ml-auto flex items-center gap-2">
              {mode === 'hls' && <span className="hidden rounded bg-white/10 px-2 py-0.5 text-xs text-white/60 sm:inline">Converting</span>}
              <button onClick={toggleFull} className="rounded-full p-2 hover:bg-white/10" aria-label="Fullscreen">
                {full ? <Minimize className="size-6" /> : <Maximize className="size-6" />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
