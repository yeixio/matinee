import { useQuery, useQueryClient } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { ArrowLeft, Maximize, Minimize, Pause, Play, RotateCcw, RotateCw, Volume2, VolumeX } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, fileUrl, type ItemDetail, type SubtitleTrack } from '../api'
import { Spinner } from '../components/ui'
import { clock } from '../format'
import { canDirectPlay } from '../player/direct'
import { defaultTrack, savePreference, SubtitleMenu, SubtitleOverlay } from '../player/Subtitles'
import { parseVTT, type Cue } from '../player/vtt'

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
  const [sub, setSub] = useState<SubtitleTrack>()
  const [cues, setCues] = useState<Cue[]>([])
  // The picture subtitle track drawn into the current transcode, if any.
  const burned = useRef<string>(undefined)
  const lastSub = useRef<SubtitleTrack>(undefined)
  const menuOpen = useRef(false)
  // Counts transcode starts so a slow, replaced one can't take over the player.
  const hlsSeq = useRef(0)
  // While the seek bar is held, where it points. The seek happens on release.
  const [scrub, setScrub] = useState<number>()
  const scrubAt = useRef<number>(undefined)
  const dragging = useRef(false)
  const scrubTimer = useRef<number>(undefined)

  const duration = item?.durationMs ? item.durationMs / 1000 : 0
  const position = useCallback(() => offset.current + (video.current?.currentTime ?? 0), [])

  const startHls = useCallback(
    async (at: number, burn = burned.current) => {
      const el = video.current
      if (!el || !item) return
      const seq = ++hlsSeq.current
      setWaiting(true)
      setError('')
      hls.current?.destroy()
      hls.current = null
      burned.current = burn
      try {
        const { url } = await api.startHls(item.id, Math.floor(at), burn)
        if (seq !== hlsSeq.current) return
        offset.current = Math.floor(at)
        // hls.js is only downloaded when a file needs converting.
        const { default: Hls } = await import('hls.js')
        if (seq !== hlsSeq.current) return
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
        // A newer seek replaced this transcode, which isn't a failure.
        if (seq !== hlsSeq.current) return
        setError(e instanceof Error ? e.message : 'Playback failed')
      }
    },
    [item],
  )

  const playDirect = useCallback(
    (at: number) => {
      const el = video.current
      if (!el || !item) return
      hlsSeq.current++
      hls.current?.destroy()
      hls.current = null
      burned.current = undefined
      setMode('direct')
      offset.current = 0
      el.src = fileUrl(item.id)
      el.currentTime = at
      el.play().catch(() => setPaused(true))
    },
    [item],
  )

  // Pick direct play or a transcode, and resume where the viewer left off.
  useEffect(() => {
    const el = video.current
    if (!item || !el || mode) return
    const resume = !item.played && item.positionMs ? item.positionMs / 1000 : 0
    const pick = defaultTrack(item.subtitles ?? [])
    setSub(pick)
    if (pick?.burnIn) startHls(resume, pick.id)
    else if (canDirectPlay(item, el)) playDirect(resume)
    else startHls(resume)
  }, [item, mode, startHls, playDirect])

  // Text subtitles are drawn by the player; fetch the cues when the choice changes.
  useEffect(() => {
    setCues([])
    if (!item || !sub || sub.burnIn) return
    let current = true
    api
      .subtitles(item.id, sub.id)
      .then((text) => current && setCues(parseVTT(text)))
      .catch(() => {})
    return () => {
      current = false
    }
  }, [item, sub])

  // Picture subtitles are part of the video, so switching to or from one
  // restarts playback at the same spot.
  const selectSub = (t: SubtitleTrack | undefined) => {
    const el = video.current
    if (!item || !el) return
    savePreference(t)
    setSub(t)
    if (t) lastSub.current = t
    const burn = t?.burnIn ? t.id : undefined
    if (burn === burned.current) return
    if (!burn && canDirectPlay(item, el)) playDirect(position())
    else startHls(position(), burn)
  }

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

  // Direct play seeks live while dragging. A transcode restarts on each far
  // seek, so it waits for the bar to be let go (or a pause in key presses).
  const scrubTo = (to: number) => {
    window.clearTimeout(scrubTimer.current)
    if (mode === 'direct') {
      seek(to)
      return
    }
    scrubAt.current = to
    setScrub(to)
    if (!dragging.current) scrubTimer.current = window.setTimeout(commitScrub, 400)
  }
  const commitScrub = () => {
    window.clearTimeout(scrubTimer.current)
    dragging.current = false
    const to = scrubAt.current
    if (to === undefined) return
    scrubAt.current = undefined
    setScrub(undefined)
    setTime(to)
    seek(to)
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
    hideTimer.current = window.setTimeout(() => !video.current?.paused && !menuOpen.current && setChrome(false), 2500)
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
        case 'c':
          if (item?.subtitles?.length) selectSub(sub ? undefined : (lastSub.current ?? item.subtitles.find((t) => !t.burnIn) ?? item.subtitles[0]))
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
  const shown = scrub ?? time
  const frac = duration ? Math.min(1, shown / duration) : 0

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

      {sub && !sub.burnIn && !error && <SubtitleOverlay cues={cues} position={position} lifted={chrome} />}

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
              value={shown}
              onPointerDown={() => (dragging.current = true)}
              onPointerUp={commitScrub}
              onPointerCancel={() => commitScrub()}
              onChange={(e) => scrubTo(Number(e.target.value))}
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
              {clock(shown)} <span className="text-white/40">/ {clock(duration)}</span>
            </div>
            <div className="ml-auto flex items-center gap-2">
              {mode === 'hls' && <span className="hidden rounded bg-white/10 px-2 py-0.5 text-xs text-white/60 sm:inline">Converting</span>}
              {!!item?.subtitles?.length && (
                <SubtitleMenu
                  tracks={item.subtitles}
                  selected={sub?.id}
                  onSelect={selectSub}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
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
