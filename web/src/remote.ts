// Remote control (see docs/remote-control.md): the web player tells the
// server what it plays, so Home Assistant and people's other devices can see
// it, and takes commands such as pause or "play this film" from them.
//
// One browser is one client, whatever tabs it has open: the tab holding the
// lock talks to the server, and another takes over when it closes.

import { ApiError } from './api'
import { heardRestarting, RESTARTING_HEADER } from './player/reconnect'

export type CommandType =
  | 'play'
  | 'pause'
  | 'stop'
  | 'seek'
  | 'next'
  | 'previous'
  | 'playItem'
  | 'volume'
  | 'mute'
  | 'sleep'
  | 'subtitles'

export type Command = {
  id: string
  type: CommandType
  positionMs?: number
  itemId?: number
  item?: { id: number; title: string }
  volume?: number
  muted?: boolean
  minutes?: number
  endOfItem?: boolean
  track?: string
  from?: string
}

export type Playing = {
  itemId: number
  positionMs: number
  durationMs?: number
  paused: boolean
  buffering?: boolean
  volume?: number
  muted?: boolean
  subtitles?: string
  transcoding?: boolean
  sleepAt?: number
  sleepAtEnd?: boolean
}

// What an open player offers: its state, the commands it takes, and what
// to do with one.
export type PlayerHooks = {
  commands: () => CommandType[]
  state: () => Playing | undefined
  handle: (c: Command) => void
}

const KEY = 'matinee.clientId'
// How long to wait for commands per request; the server holds it open.
const WAIT_SECONDS = 25

export function clientId(): string {
  try {
    let id = localStorage.getItem(KEY)
    if (!id) {
      id = crypto.randomUUID()
      localStorage.setItem(KEY, id)
    }
    return id
  } catch {
    // No storage (a private window): this tab is its own client.
    return (fallbackId ??= crypto.randomUUID())
  }
}
let fallbackId: string | undefined

// Open players, newest last: the newest gets commands. The music player is
// always there underneath, and the video player goes on top while it's open.
const players: PlayerHooks[] = []
const top = () => players[players.length - 1]
let navigate: ((to: string) => void) | undefined
let running = false
// This tab holds the lock, so it's the one talking to the server.
let active = false
let current: AbortController | undefined
let reportTimer: number | undefined

const path = () => `/api/v1/clients/${encodeURIComponent(clientId())}`

async function send(method: string, url: string, body?: unknown, signal?: AbortSignal) {
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    signal,
    keepalive: method === 'DELETE',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.headers.get(RESTARTING_HEADER)) heardRestarting()
  if (!res.ok) throw new ApiError(res.status, res.statusText)
  return res.status === 204 ? undefined : res.json()
}

function hello() {
  return {
    name: '', // the server names it after the browser
    platform: 'web',
    app: 'Matinee web',
    commands: top()?.commands() ?? ['playItem'],
    playing: top()?.state() ?? null,
  }
}

async function report() {
  window.clearTimeout(reportTimer)
  reportTimer = undefined
  if (!active) return
  try {
    await send('PUT', path(), hello())
  } catch {
    // The next report or poll tries again.
  }
}

// Commands that need no player: start something.
function handle(c: Command) {
  const player = top()
  if (player) {
    player.handle(c)
    return
  }
  if (c.type === 'playItem' && c.itemId && navigate) {
    navigate(watchPath(c.itemId, c.positionMs))
  }
}

export function watchPath(itemId: number, positionMs?: number) {
  return `/watch/${itemId}${positionMs !== undefined ? `?t=${Math.floor(positionMs / 1000)}` : ''}`
}

async function loop(signal: AbortSignal) {
  await report()
  let failures = 0
  while (!signal.aborted) {
    try {
      const res = await send('GET', `${path()}/commands?wait=${WAIT_SECONDS}`, undefined, signal)
      failures = 0
      // The server is shutting down; players say so (player/reconnect.ts).
      if (res?.restarting) heardRestarting()
      for (const c of (res?.commands ?? []) as Command[]) handle(c)
    } catch (e) {
      if (signal.aborted) return
      // The server forgot this client (it restarted): say hello again.
      if (e instanceof ApiError && e.status === 404) {
        await report()
        continue
      }
      failures++
      await new Promise((r) => window.setTimeout(r, Math.min(30_000, 2_000 * failures)))
      if (failures > 1) await report()
    }
  }
}

function leave() {
  if (active) fetch(path(), { method: 'DELETE', credentials: 'same-origin', keepalive: true }).catch(() => {})
}

// hold talks to the server until this tab stops or another takes over.
async function hold() {
  const ctl = new AbortController()
  current = ctl
  active = true
  await loop(ctl.signal)
  if (current === ctl) active = false
}

// acquire waits for this tab's turn. steal takes it now, for a tab that
// opens a player while another tab has the turn.
function acquire(steal = false) {
  if (!navigator.locks) {
    if (!active) hold()
    return
  }
  navigator.locks.request('matinee-remote', steal ? { steal: true } : {}, hold).catch(() => {
    // Another tab took over: stop, and wait for another turn.
    current?.abort()
    active = false
    if (running) acquire()
  })
}

export const remote = {
  // Start talking to the server; to is how to open a page.
  start(to: (path: string) => void) {
    navigate = to
    if (running) return
    running = true
    window.addEventListener('pagehide', leave)
    acquire()
  },
  // A player opened; it gets commands until the returned function is
  // called. A background player (the music player, always there) doesn't
  // take the turn from another tab.
  attach(hooks: PlayerHooks, background = false) {
    players.push(hooks)
    if (running && !active && !background) acquire(true)
    else remote.report()
    return () => {
      const i = players.indexOf(hooks)
      if (i >= 0) players.splice(i, 1)
      remote.report()
    }
  },
  // Something changed: tell the server soon. Changes close together go as one.
  report() {
    if (reportTimer === undefined) reportTimer = window.setTimeout(report, 300)
  },
}
