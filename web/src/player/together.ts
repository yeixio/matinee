// Watch together (see docs/watch-together.md): this browser's place in a
// shared session. Everyone's player follows the session's state, which the
// server keeps; when someone here plays, pauses, seeks or moves to another
// title, the player tells the server and everyone else follows.
//
// The session outlives one player: moving on to the next episode keeps it,
// and leaving the player (the /watch pages) leaves it.

import { api, ApiError, type TogetherAction, type TogetherState } from '../api'
import { clientId } from '../remote'

// How far a player may drift from the session before it seeks back in line,
// in seconds. Smaller than this is buffering noise people don't notice.
export const DRIFT_SECONDS = 2
// How long the server holds a request for changes.
const WAIT_SECONDS = 25

type Listener = (s: TogetherState | undefined) => void

let state: TogetherState | undefined
// The server's clock minus this one's, from the last answer.
let skew = 0
let loopCtl: AbortController | undefined
const listeners = new Set<Listener>()
// Open players; when the last one closes for good, this browser leaves.
let attached = 0
let leaveTimer: number | undefined

function set(s: TogetherState | undefined) {
  if (s) skew = s.serverTime - Date.now()
  if (s?.ended) s = undefined
  state = s
  listeners.forEach((l) => l(state))
}

const me = () => ({ clientId: clientId(), platform: 'web', device: '' })

// expectedSeconds is where the session's playback is now.
export function expectedSeconds(s: TogetherState, now = Date.now() + skew): number {
  const ms = s.paused ? s.positionMs : s.positionMs + Math.max(0, now - s.updatedAt)
  return ms / 1000
}

// needsSeek says whether a player at `at` seconds is too far from the session.
export function needsSeek(s: TogetherState, at: number, now?: number): boolean {
  return Math.abs(expectedSeconds(s, now) - at) > DRIFT_SECONDS
}

// formatCode shows a code the way people read it out: ABC-DEF.
export function formatCode(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)}-${code.slice(3)}` : code
}

export function togetherLink(code: string): string {
  return `${location.origin}/together/${code}`
}

async function follow(ctl: AbortController) {
  let failures = 0
  while (!ctl.signal.aborted && state) {
    const code = state.code
    try {
      const res = await fetch(
        `/api/v1/together/${code}/state?client=${encodeURIComponent(clientId())}&since=${state.version}&wait=${WAIT_SECONDS}`,
        { credentials: 'same-origin', signal: ctl.signal, cache: 'no-store' },
      )
      if (ctl.signal.aborted) return
      if (res.status === 404 || res.status === 403) {
        // The server restarted and lost the session, or dropped us after a
        // long gap: start it again under its code, or join whoever did.
        await rejoin(code)
        continue
      }
      if (!res.ok) throw new Error(res.statusText)
      failures = 0
      const s = (await res.json()) as TogetherState
      if (state?.code === code) set(s)
    } catch {
      if (ctl.signal.aborted) return
      failures++
      await new Promise((r) => window.setTimeout(r, Math.min(15_000, 1_000 * failures)))
    }
  }
}

async function rejoin(code: string) {
  const last = state
  if (!last) return
  try {
    set(await api.joinTogether(code, me()))
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 404) throw e
    set(
      await api.startTogether({
        ...me(),
        itemId: last.itemId,
        positionMs: Math.round(expectedSeconds(last) * 1000),
        paused: last.paused,
        resume: code,
      }),
    )
  }
}

function run() {
  loopCtl?.abort()
  const ctl = new AbortController()
  loopCtl = ctl
  follow(ctl)
}

export const together = {
  get state() {
    return state
  },
  subscribe(l: Listener) {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
  // Start a session around what this player has open.
  async start(itemId: number, positionMs: number, paused: boolean) {
    set(await api.startTogether({ ...me(), itemId, positionMs, paused }))
    run()
    return state!
  },
  async join(code: string) {
    set(await api.joinTogether(code, me()))
    run()
    return state!
  },
  // Something this person did, for everyone. The state changes here right
  // away so the player doesn't snap back while the server answers.
  async act(action: Exclude<TogetherAction, 'start' | 'join' | 'leave'>, positionMs: number, itemId?: number) {
    const s = state
    if (!s) return
    const now = Date.now() + skew
    set({
      ...s,
      paused: action === 'pause' ? true : action === 'play' ? false : s.paused,
      positionMs,
      itemId: itemId ?? s.itemId,
      updatedAt: now,
      serverTime: now,
    })
    try {
      const next = await api.actTogether(s.code, { ...me(), action, positionMs: Math.round(positionMs), itemId })
      if (state?.code === next.code && next.version >= (state?.version ?? 0)) set(next)
    } catch {
      // The next answer from the server puts things right.
    }
  },
  // The watch page opened; the returned function is it closing. Closing
  // and opening again straight away (moving to another title) stays in.
  attach() {
    attached++
    window.clearTimeout(leaveTimer)
    return () => {
      attached--
      window.clearTimeout(leaveTimer)
      leaveTimer = window.setTimeout(() => attached === 0 && together.leave(), 1000)
    }
  },
  leave() {
    const s = state
    loopCtl?.abort()
    set(undefined)
    if (s) {
      fetch(`/api/v1/together/${s.code}/members/${encodeURIComponent(clientId())}`, {
        method: 'DELETE',
        credentials: 'same-origin',
        keepalive: true,
      }).catch(() => {})
    }
  },
  async end() {
    const s = state
    loopCtl?.abort()
    set(undefined)
    if (s) await api.endTogether(s.code, me()).catch(() => {})
  },
}
