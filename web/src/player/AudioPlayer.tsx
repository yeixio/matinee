import type Hls from 'hls.js'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, fileUrl, imageUrl, type Item } from '../api'
import { remote, watchPath, type CommandType, type PlayerHooks } from '../remote'
import * as Q from './queue'
import { MusicBar } from './MusicBar'
import { NowPlaying } from './NowPlaying'

export type AudioState = {
  queue: Q.Queue
  current?: Item
  playing: boolean
  audio: React.RefObject<HTMLAudioElement | null>
  // A moment's note, such as "Added to the queue".
  notice?: string
  // The full player, over the page.
  expanded: boolean
  setExpanded: (open: boolean) => void
  // at < 0 with shuffle starts anywhere.
  play: (items: Item[], at?: number, opts?: { shuffle?: boolean }) => void
  // Plays an artist, album or track (from its album), or opens the video player.
  playItem: (item: Item, positionMs?: number) => Promise<void>
  playNext: (items: Item[]) => void
  addToQueue: (items: Item[]) => void
  jump: (pos: number) => void
  remove: (key: string) => void
  move: (from: number, to: number) => void
  clearUpNext: () => void
  toggle: () => void
  pause: () => void
  next: () => void
  prev: () => void
  seek: (sec: number) => void
  setShuffle: (on: boolean) => void
  cycleRepeat: () => void
  stop: () => void
}

const AudioContext = createContext<AudioState | null>(null)

export function useAudio() {
  const ctx = useContext(AudioContext)
  if (!ctx) throw new Error('useAudio outside AudioProvider')
  return ctx
}

// The playing track's time and length, in seconds, as they change.
export function useAudioTime() {
  const { audio } = useAudio()
  const [time, setTime] = useState(0)
  const [dur, setDur] = useState(0)
  useEffect(() => {
    const el = audio.current
    if (!el) return
    const tick = () => {
      setTime(el.currentTime)
      setDur(Number.isFinite(el.duration) ? el.duration : 0)
    }
    tick()
    const events = ['timeupdate', 'durationchange', 'seeked', 'emptied'] as const
    events.forEach((e) => el.addEventListener(e, tick))
    return () => events.forEach((e) => el.removeEventListener(e, tick))
  }, [audio])
  return { time, dur }
}

export const musicKinds = ['artist', 'album', 'track']

// What a page reload resumes: the queue, paused where it was. A huge
// shuffle keeps only the part around what's playing.
const MAX_SAVED = 300
const storageKey = (user?: number) => `matinee.music.${user ?? 0}`

function save(user: number | undefined, q: Q.Queue, time: number) {
  try {
    if (!q.entries.length) {
      localStorage.removeItem(storageKey(user))
      return
    }
    let saved = q
    if (q.entries.length > MAX_SAVED) {
      const from = Math.max(0, q.pos - 50)
      const entries = q.entries.slice(from, from + MAX_SAVED)
      saved = { ...q, entries, original: q.shuffle ? entries : entries, pos: q.pos - from }
    }
    localStorage.setItem(storageKey(user), JSON.stringify({ ...saved, time }))
  } catch {
    // Storage is full or off: nothing to resume, which is fine.
  }
}

function restore(user: number | undefined): { queue: Q.Queue; time: number } | undefined {
  try {
    const raw = localStorage.getItem(storageKey(user))
    if (!raw) return undefined
    const { time, ...queue } = JSON.parse(raw) as Q.Queue & { time: number }
    if (!Array.isArray(queue.entries) || !queue.entries[queue.pos]) return undefined
    return { queue, time: time || 0 }
  } catch {
    return undefined
  }
}

const VOLUME_KEY = 'matinee.music.volume'

// Music keeps playing while you browse: one <audio> element lives above the
// routes, with the queue, the bar along the bottom and the full player.
export function AudioProvider({ user, children }: { user?: number; children: ReactNode }) {
  const navigate = useNavigate()
  // The video player fills the window; the music bar waits underneath.
  const watching = useLocation().pathname.startsWith('/watch')
  const audio = useRef<HTMLAudioElement>(null)
  const hls = useRef<Hls | null>(null)
  const [saved] = useState(() => restore(user))
  const [queue, setQueue] = useState<Q.Queue>(saved?.queue ?? Q.emptyQueue)
  const [playing, setPlaying] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const { t } = useTranslation()
  const [notice, setNotice] = useState<string>()
  const noticeTimer = useRef<number>(undefined)
  const note = useCallback((text: string) => {
    setNotice(text)
    window.clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(undefined), 2500)
  }, [])
  const current = Q.current(queue)
  const entry = queue.entries[queue.pos]
  // Where the next track to load starts, and whether it plays: a restored
  // queue loads paused at its old place.
  const resumeAt = useRef<{ time: number; play: boolean } | undefined>(saved && { time: saved.time, play: false })
  // Tracks already counted as played, by queue entry.
  const reported = useRef(new Set<string>())
  const queueRef = useRef(queue)
  queueRef.current = queue

  const load = useCallback(async (item: Item, viaHls = false) => {
    const el = audio.current
    if (!el) return
    hls.current?.destroy()
    hls.current = null
    const at = resumeAt.current?.time
    const autoplay = resumeAt.current?.play ?? true
    resumeAt.current = undefined
    if (!viaHls) {
      el.src = fileUrl(item.id)
    } else {
      const { url } = await api.startHls(item.id, at ?? 0)
      const { default: Hls } = await import('hls.js')
      if (el.canPlayType('application/vnd.apple.mpegurl')) {
        el.src = url
      } else if (Hls.isSupported()) {
        hls.current = new Hls()
        hls.current.loadSource(url)
        hls.current.attachMedia(el)
      }
    }
    if (at && !viaHls) el.currentTime = at
    if (autoplay) el.play().catch(() => setPlaying(false))
  }, [])

  // A new entry (not just a new track: the same song queued twice plays again).
  useEffect(() => {
    if (current) load(current)
    else {
      audio.current?.pause()
      audio.current?.removeAttribute('src')
    }
  }, [entry?.key]) // eslint-disable-line react-hooks/exhaustive-deps

  // Formats the browser can't decode (ALAC, WMA…) fall back to a transcode.
  const onError = () => {
    if (current && !hls.current) load(current, true)
  }

  // A track counts as played at its end, or when skipped past halfway, so
  // albums show up under Recently played.
  const markPlayed = useCallback(() => {
    const el = audio.current
    const e = queueRef.current.entries[queueRef.current.pos]
    if (!el || !e || reported.current.has(e.key)) return
    if (el.ended || (el.duration > 0 && el.currentTime > el.duration / 2)) {
      reported.current.add(e.key)
      api.setProgress(e.item.id, 0, true).catch(() => {})
    }
  }, [])

  const advance = useCallback(
    (auto: boolean) => {
      markPlayed()
      const n = Q.next(queueRef.current, auto)
      const el = audio.current
      if (!n) {
        setPlaying(false)
        if (el) el.currentTime = 0
        return
      }
      if (n.pos === queueRef.current.pos && el) {
        // Repeat one, or a one-track queue repeating: play it again.
        reported.current.delete(n.entries[n.pos].key)
        el.currentTime = 0
        el.play().catch(() => {})
      }
      setQueue(n)
    },
    [markPlayed],
  )

  // Remembered between reloads.
  useEffect(() => {
    const el = audio.current
    if (!el) return
    try {
      const v = Number(localStorage.getItem(VOLUME_KEY))
      if (v > 0 && v <= 1) el.volume = v
    } catch {
      // no storage
    }
    const keep = () => {
      try {
        localStorage.setItem(VOLUME_KEY, String(el.volume))
      } catch {
        // no storage
      }
    }
    el.addEventListener('volumechange', keep)
    return () => el.removeEventListener('volumechange', keep)
  }, [])

  useEffect(() => {
    save(user, queue, audio.current?.currentTime ?? 0)
  }, [user, queue])
  useEffect(() => {
    if (!playing) return
    const t = window.setInterval(() => save(user, queueRef.current, audio.current?.currentTime ?? 0), 5000)
    return () => window.clearInterval(t)
  }, [user, playing])

  const playItem = useCallback(
    async (item: Item, positionMs?: number) => {
      if (!musicKinds.includes(item.kind)) {
        navigate(watchPath(item.id, positionMs))
        return
      }
      const tracks = await api.tracks(item.id)
      const at = item.kind === 'track' ? Math.max(0, tracks.findIndex((t) => t.id === item.id)) : 0
      resumeAt.current = positionMs ? { time: positionMs / 1000, play: true } : undefined
      setQueue((q) => Q.start(q, tracks, at, false))
    },
    [navigate],
  )

  const value = useMemo<AudioState>(() => {
    const el = () => audio.current
    return {
      queue,
      current,
      playing,
      audio,
      notice,
      expanded,
      setExpanded,
      play: (items, at = 0, opts) => {
        if (!items.length) return
        resumeAt.current = undefined
        markPlayed()
        setQueue((q) => Q.start(q, items, at, opts?.shuffle ?? (at < 0 ? true : q.shuffle)))
      },
      playItem,
      playNext: (items) => {
        if (current) note(t('common:music.playingNext'))
        setQueue((q) => Q.playNext(q, items))
      },
      addToQueue: (items) => {
        if (current) note(t('common:music.queued'))
        setQueue((q) => Q.addToQueue(q, items))
      },
      jump: (pos) => {
        markPlayed()
        setQueue((q) => Q.jump(q, pos))
      },
      remove: (key) => setQueue((q) => Q.remove(q, key)),
      move: (from, to) => setQueue((q) => Q.move(q, from, to)),
      clearUpNext: () => setQueue((q) => Q.clearUpNext(q)),
      toggle: () => {
        const a = el()
        if (!a || !current) return
        if (a.paused) a.play().catch(() => {})
        else a.pause()
      },
      pause: () => el()?.pause(),
      next: () => advance(false),
      prev: () => {
        const a = el()
        if (a && a.currentTime > 3) a.currentTime = 0
        else setQueue((q) => Q.previous(q))
      },
      seek: (sec) => {
        const a = el()
        if (a) a.currentTime = sec
      },
      setShuffle: (on) => setQueue((q) => Q.setShuffle(q, on)),
      cycleRepeat: () => setQueue((q) => Q.cycleRepeat(q)),
      stop: () => {
        el()?.pause()
        hls.current?.destroy()
        hls.current = null
        setExpanded(false)
        setQueue(Q.emptyQueue)
      },
    }
  }, [queue, current, playing, notice, expanded, advance, markPlayed, playItem, note, t])

  useMediaSession(value)
  useRemote(value)

  return (
    <AudioContext.Provider value={value}>
      {children}
      <audio
        ref={audio}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => advance(true)}
        onError={onError}
      />
      {current && !watching && <MusicBar />}
      {current && expanded && !watching && <NowPlaying />}
    </AudioContext.Provider>
  )
}

// The system's own controls: media keys, the lock screen and notification
// on phones, and headphone buttons.
function useMediaSession(s: AudioState) {
  const ref = useRef(s)
  ref.current = s
  const ms = typeof navigator !== 'undefined' && 'mediaSession' in navigator ? navigator.mediaSession : undefined
  const item = s.current

  useEffect(() => {
    if (!ms) return
    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => ref.current.audio.current?.play().catch(() => {})],
      ['pause', () => ref.current.pause()],
      ['previoustrack', () => ref.current.prev()],
      ['nexttrack', () => ref.current.next()],
      ['seekto', (d) => d.seekTime !== undefined && ref.current.seek(d.seekTime)],
    ]
    for (const [action, h] of handlers) {
      try {
        ms.setActionHandler(action, h)
      } catch {
        // not supported here
      }
    }
    return () => {
      for (const [action] of handlers) {
        try {
          ms.setActionHandler(action, null)
        } catch {
          // not supported here
        }
      }
    }
  }, [ms])

  useEffect(() => {
    if (!ms) return
    if (!item) {
      ms.metadata = null
      return
    }
    const art = new URL(imageUrl(item.id, 'poster'), window.location.href).href
    ms.metadata = new MediaMetadata({ title: item.title, artist: item.artist ?? '', album: item.album ?? '', artwork: [{ src: art }] })
  }, [ms, item])

  useEffect(() => {
    if (!ms) return
    ms.playbackState = !item ? 'none' : s.playing ? 'playing' : 'paused'
    const el = s.audio.current
    if (!el || !item) return
    const update = () => {
      if (el.duration > 0 && Number.isFinite(el.duration)) {
        try {
          ms.setPositionState({ duration: el.duration, position: Math.min(el.currentTime, el.duration), playbackRate: el.playbackRate })
        } catch {
          // a stale position; the next update fixes it
        }
      }
    }
    update()
    el.addEventListener('durationchange', update)
    el.addEventListener('seeked', update)
    return () => {
      el.removeEventListener('durationchange', update)
      el.removeEventListener('seeked', update)
    }
  }, [ms, item, s.playing, s.audio])
}

// Remote control (see remote.ts): Home Assistant and people's other devices
// see what's playing here and control it, and can start music on this
// browser. The video player takes over while it's open.
function useRemote(s: AudioState) {
  const ref = useRef(s)
  ref.current = s
  useEffect(() => {
    const hooks: PlayerHooks = {
      commands: () => {
        const c: CommandType[] = ['playItem']
        if (ref.current.current) c.push('play', 'pause', 'stop', 'seek', 'next', 'previous', 'volume', 'mute')
        return c
      },
      state: () => {
        const { current, audio } = ref.current
        const el = audio.current
        if (!current || !el) return undefined
        return {
          itemId: current.id,
          positionMs: Math.round(el.currentTime * 1000),
          durationMs: current.durationMs ?? (el.duration > 0 ? Math.round(el.duration * 1000) : undefined),
          paused: el.paused,
          volume: el.volume,
          muted: el.muted,
        }
      },
      handle: (c) => {
        const st = ref.current
        const el = st.audio.current
        switch (c.type) {
          case 'playItem':
            if (c.itemId) api.item(c.itemId).then((it) => st.playItem(it, c.positionMs)).catch(() => {})
            break
          case 'play':
            el?.play().catch(() => {})
            break
          case 'pause':
            st.pause()
            break
          case 'stop':
            st.stop()
            break
          case 'seek':
            st.seek((c.positionMs ?? 0) / 1000)
            break
          case 'next':
            st.next()
            break
          case 'previous':
            st.prev()
            break
          case 'volume':
            if (el) el.volume = Math.max(0, Math.min(1, c.volume ?? 1))
            break
          case 'mute':
            if (el) el.muted = !!c.muted
            break
        }
      },
    }
    return remote.attach(hooks, true)
  }, [])

  useEffect(() => remote.report(), [s.current, s.playing])
  useEffect(() => {
    if (!s.playing) return
    const t = window.setInterval(remote.report, 10_000)
    return () => window.clearInterval(t)
  }, [s.playing])
}
