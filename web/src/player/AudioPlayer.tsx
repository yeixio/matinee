import type Hls from 'hls.js'
import { Pause, Play, SkipBack, SkipForward, X } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { api, fileUrl, type Item } from '../api'
import { Artwork } from '../components/Artwork'
import { clock } from '../format'

type AudioState = {
  queue: Item[]
  index: number
  current?: Item
  playing: boolean
  play: (queue: Item[], index?: number) => void
  toggle: () => void
  next: () => void
  prev: () => void
  stop: () => void
}

const AudioContext = createContext<AudioState | null>(null)

export function useAudio() {
  const ctx = useContext(AudioContext)
  if (!ctx) throw new Error('useAudio outside AudioProvider')
  return ctx
}

// Music keeps playing while you browse: one <audio> element lives above the routes.
export function AudioProvider({ children }: { children: ReactNode }) {
  const audio = useRef<HTMLAudioElement>(null)
  const hls = useRef<Hls | null>(null)
  const [queue, setQueue] = useState<Item[]>([])
  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(false)
  const current = queue[index]

  const load = useCallback(async (item: Item, viaHls = false) => {
    const el = audio.current
    if (!el) return
    hls.current?.destroy()
    hls.current = null
    if (!viaHls) {
      el.src = fileUrl(item.id)
    } else {
      const { url } = await api.startHls(item.id)
      const { default: Hls } = await import('hls.js')
      if (el.canPlayType('application/vnd.apple.mpegurl')) {
        el.src = url
      } else if (Hls.isSupported()) {
        hls.current = new Hls()
        hls.current.loadSource(url)
        hls.current.attachMedia(el)
      }
    }
    el.play().catch(() => setPlaying(false))
  }, [])

  useEffect(() => {
    if (current) load(current)
  }, [current, load])

  // Formats the browser can't decode (ALAC, WMA…) fall back to a transcode.
  const onError = () => {
    if (current && !hls.current) load(current, true)
  }

  const value = useMemo<AudioState>(
    () => ({
      queue,
      index,
      current,
      playing,
      play: (q, i = 0) => {
        setQueue(q)
        setIndex(i)
      },
      toggle: () => {
        const el = audio.current
        if (!el) return
        if (el.paused) el.play()
        else el.pause()
      },
      next: () => setIndex((i) => Math.min(i + 1, queue.length - 1)),
      prev: () => {
        const el = audio.current
        if (el && el.currentTime > 3) el.currentTime = 0
        else setIndex((i) => Math.max(i - 1, 0))
      },
      stop: () => {
        audio.current?.pause()
        hls.current?.destroy()
        hls.current = null
        setQueue([])
      },
    }),
    [queue, index, current, playing],
  )

  return (
    <AudioContext.Provider value={value}>
      {children}
      <audio
        ref={audio}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => (index < queue.length - 1 ? setIndex(index + 1) : setPlaying(false))}
        onError={onError}
      />
      {current && <NowPlaying audio={audio} state={value} />}
    </AudioContext.Provider>
  )
}

function NowPlaying({ audio, state }: { audio: React.RefObject<HTMLAudioElement | null>; state: AudioState }) {
  const [time, setTime] = useState(0)
  const [dur, setDur] = useState(0)
  useEffect(() => {
    const el = audio.current
    if (!el) return
    const tick = () => {
      setTime(el.currentTime)
      setDur(el.duration || 0)
    }
    el.addEventListener('timeupdate', tick)
    el.addEventListener('durationchange', tick)
    return () => {
      el.removeEventListener('timeupdate', tick)
      el.removeEventListener('durationchange', tick)
    }
  }, [audio])
  const item = state.current!
  const seek = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (audio.current) audio.current.currentTime = Number(e.target.value)
  }
  return (
    <div
      className="fixed inset-x-0 bottom-[calc(57px+env(safe-area-inset-bottom))] z-30 border-t border-line/60 bg-surface/95 backdrop-blur md:bottom-0 md:left-60"
    >
      <input
        type="range"
        min={0}
        max={dur || 0}
        step={0.1}
        value={time}
        onChange={seek}
        aria-label="Seek"
        className="absolute inset-x-0 -top-1.5 h-3 w-full cursor-pointer appearance-none bg-transparent accent-accent"
        style={{ background: `linear-gradient(to right, var(--color-accent) ${dur ? (time / dur) * 100 : 0}%, transparent 0) center / 100% 3px no-repeat` }}
      />
      <div className="flex h-16 items-center gap-3 px-4 md:h-20 md:px-6">
        <Link to={item.parentId ? `/item/${item.parentId}` : '#'} className="shrink-0">
          <Artwork item={item} type="poster" label={false} className="size-11 rounded-md md:size-14" />
        </Link>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{item.title}</div>
          <div className="text-xs text-muted tabular-nums">
            {clock(time)} / {clock(dur)}
          </div>
        </div>
        <div className="flex items-center gap-1">
          <button onClick={state.prev} className="rounded-full p-2 text-muted hover:text-text" aria-label="Previous">
            <SkipBack className="size-5 fill-current" />
          </button>
          <button
            onClick={state.toggle}
            className="grid size-11 place-items-center rounded-full bg-text text-ink hover:scale-105"
            aria-label={state.playing ? 'Pause' : 'Play'}
          >
            {state.playing ? <Pause className="size-5 fill-current" /> : <Play className="size-5 translate-x-0.5 fill-current" />}
          </button>
          <button
            onClick={state.next}
            disabled={state.index >= state.queue.length - 1}
            className="rounded-full p-2 text-muted hover:text-text disabled:opacity-30"
            aria-label="Next"
          >
            <SkipForward className="size-5 fill-current" />
          </button>
          <button onClick={state.stop} className="ml-1 hidden rounded-full p-2 text-faint hover:text-text md:block" aria-label="Close player">
            <X className="size-5" />
          </button>
        </div>
      </div>
    </div>
  )
}
