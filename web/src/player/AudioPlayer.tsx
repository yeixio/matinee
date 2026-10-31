import { useQueryClient } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, fileUrl, imageUrl, type Book, type Item } from '../api'
import { remote, watchPath, type CommandType, type PlayerHooks } from '../remote'
import * as B from './book'
import { BookBar } from './BookBar'
import { BookPlayer } from './BookPlayer'
import * as Q from './queue'
import { MusicBar } from './MusicBar'
import { NowPlaying } from './NowPlaying'
import { health, onRestarting, outcome, restarting, waitForServer, type Health, type Waiting } from './reconnect'
import { Away, Toast } from './Away'
import { sleepTimer, sleepVolume, useSleep } from './sleep'

export type AudioState = {
  queue: Q.Queue
  current?: Item
  // The audiobook playing, when it's one: the queue is its parts, and
  // next and previous go by chapter.
  book?: Book
  // How fast a book plays; music always plays at 1×.
  speed: number
  setSpeed: (speed: number) => void
  // Plays a book (or a part's book) from where the person left off, or
  // from a time on the book's clock. A podcast episode plays as a book of
  // one part (see book.episodeBook).
  playBook: (id: number, atSec?: number, episode?: Item) => Promise<void>
  // Goes to a time on the book's clock, or jumps by seconds.
  seekBook: (sec: number) => void
  skip: (seconds: number) => void
  playing: boolean
  audio: React.RefObject<HTMLAudioElement | null>
  // A moment's note, such as "Added to the queue", and a way to show one.
  notice?: string
  say: (text: string) => void
  // Why playback is waiting for the server, while it's away, or that it
  // gave up waiting.
  away?: Waiting | 'gaveUp'
  // After giving up: tries the server again, and picks up where it was.
  reconnect: () => void
  // The full player, over the page.
  expanded: boolean
  setExpanded: (open: boolean) => void
  // at < 0 with shuffle starts anywhere.
  play: (items: Item[], at?: number, opts?: { shuffle?: boolean }) => void
  // Plays an artist, album or track (from its album), a book, or opens the video player.
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
export const bookKinds = ['book', 'part']
export const podcastKinds = ['podcast', 'podcastEpisode']

// The playing book's time and length on its own clock, in seconds, and
// the chapter playing.
export function useBookTime() {
  const { book, queue } = useAudio()
  const { time } = useAudioTime()
  if (!book) return { time: 0, dur: 0, chapter: -1 }
  const at = B.bookTime(book, queue.pos, time)
  return { time: at, dur: B.bookLength(book), chapter: B.chapterAt(book.chapters, at) }
}

// What a page reload resumes: the queue, paused where it was. A huge
// shuffle keeps only the part around what's playing.
const MAX_SAVED = 300
const storageKey = (user?: number) => `matinee.music.${user ?? 0}`

function save(user: number | undefined, q: Q.Queue, time: number, book?: Book) {
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
    localStorage.setItem(storageKey(user), JSON.stringify({ ...saved, time, book }))
  } catch {
    // Storage is full or off: nothing to resume, which is fine.
  }
}

function restore(user: number | undefined): { queue: Q.Queue; time: number; book?: Book } | undefined {
  try {
    const raw = localStorage.getItem(storageKey(user))
    if (!raw) return undefined
    const { time, book, ...queue } = JSON.parse(raw) as Q.Queue & { time: number; book?: Book }
    if (!Array.isArray(queue.entries) || !queue.entries[queue.pos]) return undefined
    return { queue, time: time || 0, book: book?.parts?.length ? book : undefined }
  } catch {
    return undefined
  }
}

const VOLUME_KEY = 'matinee.music.volume'

// Why a track stopped: the file or server failed, it sat loading, or the
// person asked to try again.
type Cause = 'error' | 'stall' | 'retry'

// Music and audiobooks keep playing while you browse: one <audio> element
// lives above the routes, with the queue, the bar along the bottom and the
// full player. A book's parts are the queue, played in order.
export function AudioProvider({ user, children }: { user?: number; children: ReactNode }) {
  const navigate = useNavigate()
  // The video player fills the window; the music bar waits underneath.
  const watching = useLocation().pathname.startsWith('/watch')
  const audio = useRef<HTMLAudioElement>(null)
  const hls = useRef<Hls | null>(null)
  const [saved] = useState(() => restore(user))
  const [queue, setQueue] = useState<Q.Queue>(saved?.queue ?? Q.emptyQueue)
  const [book, setBook] = useState<Book | undefined>(saved?.book)
  const bookRef = useRef(book)
  bookRef.current = book
  const [speed, setSpeedState] = useState(() => B.savedSpeed(user))
  const speedRef = useRef(speed)
  speedRef.current = speed
  const qc = useQueryClient()
  const [playing, setPlaying] = useState(false)
  const playingRef = useRef(playing)
  playingRef.current = playing
  // The volume before the sleep timer began fading it.
  const fadeFrom = useRef<number>(undefined)
  const [expanded, setExpanded] = useState(false)
  const { t } = useTranslation()
  const [notice, setNotice] = useState<string>()
  const noticeTimer = useRef<number>(undefined)
  const note = useCallback((text: string, ms = 2500) => {
    setNotice(text)
    window.clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(undefined), ms)
  }, [])
  const [away, setAway] = useState<Waiting | 'gaveUp'>()
  const current = Q.current(queue)
  const entry = queue.entries[queue.pos]
  // Where the next track to load starts, and whether it plays: a restored
  // queue loads paused at its old place.
  const resumeAt = useRef<{ time: number; play: boolean } | undefined>(saved && { time: saved.time, play: false })
  // Tracks already counted as played, by queue entry.
  const reported = useRef(new Set<string>())
  const queueRef = useRef(queue)
  queueRef.current = queue
  // How the track playing is loaded (a transcode for formats the browser
  // can't play), and the last place it got to, to pick up from if the
  // server goes away.
  const viaHlsNow = useRef(false)
  const lastTime = useRef(0)
  const recoverRef = useRef<(cause: Cause) => void>(undefined)

  const load = useCallback(async (item: Item, viaHls = false) => {
    const el = audio.current
    if (!el) return
    hls.current?.destroy()
    hls.current = null
    const at = resumeAt.current?.time
    const autoplay = resumeAt.current?.play ?? true
    resumeAt.current = undefined
    viaHlsNow.current = viaHls
    lastTime.current = at ?? 0
    if (!viaHls) {
      el.src = fileUrl(item.id)
    } else {
      let url: string
      try {
        url = (await api.startHls(item.id, at ?? 0)).url
      } catch {
        // The server may be away: find out, and come back here once it's back.
        resumeAt.current = { time: at ?? 0, play: autoplay }
        recoverRef.current?.('error')
        return
      }
      const { default: Hls } = await import('hls.js')
      if (el.canPlayType('application/vnd.apple.mpegurl')) {
        el.src = url
      } else if (Hls.isSupported()) {
        const h = new Hls()
        hls.current = h
        h.on(Hls.Events.ERROR, (_, d) => {
          if (d.fatal && hls.current === h) recoverRef.current?.('error')
        })
        h.loadSource(url)
        h.attachMedia(el)
      }
    }
    // A new source starts at the default rate, so books keep their speed.
    const rate = bookRef.current ? speedRef.current : 1
    el.defaultPlaybackRate = rate
    el.playbackRate = rate
    if (at && !viaHls) el.currentTime = at
    if (autoplay) el.play().catch(() => setPlaying(false))
  }, [])

  // A new entry (not just a new track: the same song queued twice plays again).
  useEffect(() => {
    recovering.current?.abort()
    recovering.current = undefined
    setAway(undefined)
    if (current) load(current)
    else {
      audio.current?.pause()
      audio.current?.removeAttribute('src')
    }
  }, [entry?.key]) // eslint-disable-line react-hooks/exhaustive-deps

  // What the server was when the player started, to tell later what
  // happened if it goes away.
  const server = useRef<Health>(undefined)
  useEffect(() => {
    health()
      .then((h) => (server.current ??= h))
      .catch(() => {})
  }, [])
  const recovering = useRef<AbortController>(undefined)
  useEffect(() => () => recovering.current?.abort(), [])

  // recover works out why the track stopped and gets it going again where
  // it was. When the server is fine, it's the file: formats the browser
  // can't decode (ALAC, WMA…) fall back to a transcode. When the server is
  // away or came back different, the player says so, waits for it, and
  // picks up where it was.
  const recover = useCallback(
    async (cause: Cause) => {
      const el = audio.current
      const item = Q.current(queueRef.current)
      if (!el || !item || recovering.current) return
      const ctl = new AbortController()
      recovering.current = ctl
      // A track that failed to load keeps the place it was to start from.
      const wanted = resumeAt.current
      const at = wanted?.time ?? (el.currentTime || lastTime.current)
      const play = cause === 'retry' || (wanted?.play ?? (!el.paused || playingRef.current))
      const resume = (viaHls = viaHlsNow.current) => {
        resumeAt.current = { time: at, play }
        load(item, viaHls)
      }
      try {
        let now: Health | undefined
        try {
          now = await health(ctl.signal)
        } catch {
          // Away: wait for it below.
        }
        if (ctl.signal.aborted) return
        if (now && now.state !== 'restarting' && outcome(server.current, now) === 'same') {
          server.current = now
          if (cause === 'error' && !viaHlsNow.current) resume(true)
          else if (cause !== 'error') resume()
          return
        }
        hls.current?.destroy()
        hls.current = null
        el.pause()
        if (!now || now.state === 'restarting') {
          setAway(restarting() ? 'restarting' : 'unreachable')
          now = await waitForServer(setAway, ctl.signal)
        }
        const what = outcome(server.current, now)
        server.current = now
        setAway(undefined)
        if (what === 'updated') note(t('common:reconnect.updated', { version: now.version }), 6000)
        else if (what === 'restarted') note(t('common:reconnect.restarted'), 6000)
        // Still the same track: the person may have moved on meanwhile.
        if (Q.current(queueRef.current)?.id === item.id) resume()
      } catch {
        if (!ctl.signal.aborted) {
          resumeAt.current = { time: at, play: true }
          setAway('gaveUp')
        }
      } finally {
        if (recovering.current === ctl) recovering.current = undefined
      }
    },
    [load, note, t],
  )
  recoverRef.current = recover
  const onError = () => recover('error')

  // A track that sits loading for a while may have lost the server without
  // an error (a file playing directly just stops arriving).
  const stallTimer = useRef<number>(undefined)
  const onWaiting = () => {
    window.clearTimeout(stallTimer.current)
    stallTimer.current = window.setTimeout(() => {
      if (!audio.current?.paused) recover('stall')
    }, 10_000)
  }
  const onPlaying = () => window.clearTimeout(stallTimer.current)
  useEffect(() => () => window.clearTimeout(stallTimer.current), [])

  // Heads-up as soon as the server says it's restarting; what's buffered
  // often carries the song through.
  useEffect(
    () =>
      onRestarting(() => {
        if (Q.current(queueRef.current)) note(t('common:reconnect.heads'), 8000)
      }),
    [note, t],
  )

  // A track counts as played at its end, or when skipped past halfway, so
  // albums show up under Recently played.
  const markPlayed = useCallback(() => {
    const el = audio.current
    const e = queueRef.current.entries[queueRef.current.pos]
    // A book's parts aren't counted one by one: the book keeps its place (see below).
    if (!el || !e || reported.current.has(e.key) || bookRef.current) return
    if (el.ended || (el.duration > 0 && el.currentTime > el.duration / 2)) {
      reported.current.add(e.key)
      api.setProgress(e.item.id, 0, true).catch(() => {})
    }
  }, [])

  const advance = useCallback(
    (auto: boolean) => {
      markPlayed()
      const el = audio.current
      const n = Q.next(queueRef.current, auto)
      // The sleep timer set for the end of this song, or this chapter:
      // what's next waits, paused, for the morning.
      const sleepNow = auto && sleepTimer.get() === 'end'
      if (sleepNow) {
        sleepTimer.set(undefined)
        note(t('sleep.paused'))
      }
      if (n && sleepNow && n.pos !== queueRef.current.pos) {
        resumeAt.current = { time: 0, play: false }
        setPlaying(false)
        setQueue(n)
        return
      }
      if (!n || sleepNow) {
        setPlaying(false)
        const b = bookRef.current
        if (b && auto) {
          // The end of the book: it's finished, and starts over next time.
          api
            .setProgress(b.id, 0, true)
            .then(() => {
              qc.invalidateQueries({ queryKey: ['item', b.id] })
              if (B.isEpisode(b)) qc.invalidateQueries({ queryKey: ['children', b.parentId] })
            })
            .catch(() => {})
          resumeAt.current = { time: 0, play: false }
          setQueue((q) => Q.jump(q, 0))
          return
        }
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
    [markPlayed, qc, note, t],
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
      // The sleep timer's fade isn't someone turning it down.
      if (fadeFrom.current !== undefined) return
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
    save(user, queue, audio.current?.currentTime ?? 0, book)
  }, [user, queue, book])
  useEffect(() => {
    if (!playing) return
    const t = window.setInterval(() => save(user, queueRef.current, audio.current?.currentTime ?? 0, bookRef.current), 5000)
    return () => window.clearInterval(t)
  }, [user, playing])

  // Where someone is in a book follows them to their other devices: saved
  // while it plays, when it pauses, and when the page goes away.
  const bookNow = useCallback(() => {
    const b = bookRef.current
    const el = audio.current
    return b && el ? B.bookTime(b, queueRef.current.pos, el.currentTime) : 0
  }, [])
  const reportBook = useCallback(() => {
    const b = bookRef.current
    const ms = Math.round(bookNow() * 1000)
    // The first seconds say nothing, and would undo finishing the book.
    if (!b || !audio.current?.src || ms < 5000) return
    api.setProgress(b.id, ms).catch(() => {})
  }, [bookNow])
  const wasPlaying = useRef(false)
  useEffect(() => {
    if (!book) return
    if (playing) {
      wasPlaying.current = true
      const t = window.setInterval(reportBook, 15_000)
      return () => window.clearInterval(t)
    }
    if (wasPlaying.current) {
      wasPlaying.current = false
      reportBook()
      qc.invalidateQueries({ queryKey: ['item', book.id] })
      if (B.isEpisode(book)) qc.invalidateQueries({ queryKey: ['children', book.parentId] })
    }
  }, [book, playing, reportBook, qc])
  useEffect(() => {
    const away = () => document.visibilityState === 'hidden' && !audio.current?.paused && reportBook()
    window.addEventListener('pagehide', reportBook)
    document.addEventListener('visibilitychange', away)
    return () => {
      window.removeEventListener('pagehide', reportBook)
      document.removeEventListener('visibilitychange', away)
    }
  }, [reportBook])

  const seekBook = useCallback(
    (sec: number) => {
      const b = bookRef.current
      const el = audio.current
      if (!b || !el) return
      const { part, offset } = B.locate(b, Math.max(0, Math.min(sec, B.bookLength(b) - 0.5)))
      if (part === queueRef.current.pos && el.src) {
        el.currentTime = offset
        return
      }
      resumeAt.current = { time: offset, play: !el.paused }
      setQueue((q) => Q.jump(q, part))
    },
    [],
  )

  const playBook = useCallback(
    async (id: number, atSec?: number, episode?: Item) => {
      const b = episode ? B.episodeBook(episode) : await api.book(id)
      if (!b.parts.length) return
      const el = audio.current
      const from = atSec ?? (b.played ? 0 : (b.positionMs ?? 0) / 1000)
      if (bookRef.current?.id === b.id && el?.src) {
        setBook(b)
        if (atSec !== undefined) seekBook(atSec)
        el.play().catch(() => {})
        return
      }
      reportBook()
      markPlayed()
      const { part, offset } = B.locate(b, from)
      bookRef.current = b
      setBook(b)
      resumeAt.current = { time: offset, play: true }
      setQueue((q) => ({ ...Q.start(q, b.parts, part, false), repeat: 'off' }))
    },
    [markPlayed, reportBook, seekBook],
  )

  // Music takes over from a book; the book keeps its place.
  const leaveBook = useCallback(() => {
    if (!bookRef.current) return
    reportBook()
    bookRef.current = undefined
    setBook(undefined)
  }, [reportBook])

  const playItem = useCallback(
    async (item: Item, positionMs?: number) => {
      if (bookKinds.includes(item.kind)) {
        await playBook(item.id, positionMs !== undefined ? positionMs / 1000 : undefined)
        return
      }
      if (item.kind === 'podcastEpisode') {
        // Fresh, for the person's place in it.
        const ep = await api.item(item.id)
        await playBook(ep.id, positionMs !== undefined ? positionMs / 1000 : undefined, ep)
        return
      }
      if (!musicKinds.includes(item.kind)) {
        navigate(watchPath(item.id, positionMs))
        return
      }
      const tracks = await api.tracks(item.id)
      const at = item.kind === 'track' ? Math.max(0, tracks.findIndex((t) => t.id === item.id)) : 0
      leaveBook()
      resumeAt.current = positionMs ? { time: positionMs / 1000, play: true } : undefined
      setQueue((q) => Q.start(q, tracks, at, false))
    },
    [navigate, playBook, leaveBook],
  )

  // The sleep timer: the sound fades over its last half minute, then it
  // pauses. Set for the end, a book stops at the end of the chapter.
  const { sleep, left } = useSleep()
  useEffect(() => {
    const el = audio.current
    if (!el) return
    const v = sleepVolume(left)
    if (v < 1 && playing) {
      fadeFrom.current ??= el.volume
      el.volume = fadeFrom.current * v
    } else if (fadeFrom.current !== undefined) {
      el.volume = fadeFrom.current
      fadeFrom.current = undefined
    }
    if (left === 0) {
      sleepTimer.set(undefined)
      el.pause()
      if (fadeFrom.current !== undefined) el.volume = fadeFrom.current
      fadeFrom.current = undefined
      note(t('sleep.paused'))
    }
  }, [left, playing, note, t])
  const chapterEnd = useRef<number>(undefined)
  useEffect(() => {
    const b = book
    chapterEnd.current = undefined
    if (sleep !== 'end' || !b) return
    const i = B.chapterAt(b.chapters, bookNow())
    chapterEnd.current = i >= 0 ? b.chapters[i].endMs / 1000 : undefined
  }, [sleep, book, bookNow])
  const onTime = () => {
    const end = chapterEnd.current
    if (end === undefined || !bookRef.current || bookNow() < end - 0.3) return
    // Chapters that end with the file stop there, in advance above.
    if (end * 1000 >= (bookRef.current.parts[queueRef.current.pos]?.startMs ?? 0) + (bookRef.current.parts[queueRef.current.pos]?.durationMs ?? 0) - 500) return
    chapterEnd.current = undefined
    sleepTimer.set(undefined)
    audio.current?.pause()
    note(t('sleep.paused'))
  }

  const value = useMemo<AudioState>(() => {
    const el = () => audio.current
    const play = (items: Item[], at = 0, opts?: { shuffle?: boolean }) => {
      if (!items.length) return
      leaveBook()
      resumeAt.current = undefined
      markPlayed()
      setQueue((q) => Q.start(q, items, at, opts?.shuffle ?? (at < 0 ? true : q.shuffle)))
    }
    const chapters = book?.chapters ?? []
    return {
      queue,
      current,
      book,
      speed,
      setSpeed: (v) => {
        setSpeedState(v)
        B.saveSpeed(user, v)
        const a = el()
        if (a && bookRef.current) {
          a.defaultPlaybackRate = v
          a.playbackRate = v
        }
      },
      playBook,
      seekBook,
      skip: (sec) => {
        const a = el()
        if (!a) return
        if (bookRef.current) seekBook(bookNow() + sec)
        else a.currentTime = Math.max(0, a.currentTime + sec)
      },
      playing,
      audio,
      notice,
      say: note,
      away,
      reconnect: () => {
        setAway(undefined)
        recover('retry')
      },
      expanded,
      setExpanded,
      play,
      playItem,
      // A book isn't a queue to add songs to: songs start afresh.
      playNext: (items) => {
        if (book) return play(items)
        if (current) note(t('common:music.playingNext'))
        setQueue((q) => Q.playNext(q, items))
      },
      addToQueue: (items) => {
        if (book) return play(items)
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
      next: () => {
        if (!book) return advance(false)
        const to = B.nextChapterStart(chapters, bookNow())
        if (to !== undefined) seekBook(to)
      },
      prev: () => {
        const a = el()
        if (book) seekBook(B.previousChapterStart(chapters, bookNow()))
        else if (a && a.currentTime > 3) a.currentTime = 0
        else setQueue((q) => Q.previous(q))
      },
      seek: (sec) => {
        const a = el()
        if (a) a.currentTime = sec
      },
      setShuffle: (on) => setQueue((q) => Q.setShuffle(q, on)),
      cycleRepeat: () => setQueue((q) => Q.cycleRepeat(q)),
      stop: () => {
        recovering.current?.abort()
        recovering.current = undefined
        setAway(undefined)
        reportBook()
        el()?.pause()
        hls.current?.destroy()
        hls.current = null
        setExpanded(false)
        bookRef.current = undefined
        setBook(undefined)
        sleepTimer.set(undefined)
        setQueue(Q.emptyQueue)
      },
    }
  }, [queue, current, book, speed, user, playing, notice, away, recover, expanded, advance, markPlayed, playItem, playBook, seekBook, bookNow, reportBook, leaveBook, note, t])

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
        onDurationChange={(e) => {
          // Feeds often leave an episode's length out, or get it wrong.
          const b = bookRef.current
          const d = e.currentTarget.duration
          if (B.isEpisode(b) && b && Number.isFinite(d) && d > 0 && Math.abs((b.durationMs ?? 0) - d * 1000) > 2000) {
            const durationMs = Math.round(d * 1000)
            const fixed = { ...b, durationMs, parts: [{ ...b.parts[0], durationMs }] }
            bookRef.current = fixed
            setBook(fixed)
          }
        }}
        onTimeUpdate={(e) => {
          if (e.currentTarget.currentTime > 0) lastTime.current = e.currentTarget.currentTime
          onTime()
        }}
        onWaiting={onWaiting}
        onPlaying={onPlaying}
        onError={onError}
      />
      {current && away && !watching && <Away away={away} onRetry={value.reconnect} onStop={value.stop} />}
      {(!current || expanded) && notice && !away && !watching && <Toast text={notice} />}
      {current && !watching && (book ? <BookBar /> : <MusicBar />)}
      {current && expanded && !watching && (book ? <BookPlayer /> : <NowPlaying />)}
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
      ['seekbackward', (d) => ref.current.skip(-(d.seekOffset || B.SKIP_BACK))],
      ['seekforward', (d) => ref.current.skip(d.seekOffset || B.SKIP_AHEAD)],
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
    const b = s.book
    ms.metadata = b
      ? new MediaMetadata({ title: b.title, artist: b.author ?? '', album: b.podcast ?? b.series ?? '', artwork: [{ src: art }] })
      : new MediaMetadata({ title: item.title, artist: item.artist ?? '', album: item.album ?? '', artwork: [{ src: art }] })
  }, [ms, item, s.book])

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
        if (ref.current.current) c.push('play', 'pause', 'stop', 'seek', 'next', 'previous', 'volume', 'mute', 'sleep')
        return c
      },
      state: () => {
        const { current, audio, book } = ref.current
        const el = audio.current
        if (!current || !el) return undefined
        // A book reports itself, on its own clock.
        if (book) {
          return {
            itemId: book.id,
            positionMs: Math.round(B.bookTime(book, ref.current.queue.pos, el.currentTime) * 1000),
            durationMs: Math.round(B.bookLength(book) * 1000),
            paused: el.paused,
            volume: el.volume,
            muted: el.muted,
          }
        }
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
            if (st.book) st.seekBook((c.positionMs ?? 0) / 1000)
            else st.seek((c.positionMs ?? 0) / 1000)
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
          case 'sleep':
            if (c.endOfItem) sleepTimer.set('end')
            else if (c.minutes) sleepTimer.after(c.minutes)
            else sleepTimer.set(undefined)
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
