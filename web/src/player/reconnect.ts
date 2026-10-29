// Getting playback back after the server goes away for a moment: an update,
// a restart, or the network dropping. The server says when it's restarting
// (an answer to the remote control's wait for commands, and a header on
// every answer while it shuts down). Once it's back, its bootId and version
// say what happened: a new version means an update, a new bootId a restart,
// and the same of both that only the connection dropped.

export const RESTARTING_HEADER = 'Matinee-Restarting'

// How long a "restarting" heard from the server counts, and how long a
// player keeps trying before it gives up.
const HEARD_FOR = 3 * 60_000
export const GIVE_UP_AFTER = 3 * 60_000

let heardAt: number | undefined
const listeners = new Set<() => void>()

// heardRestarting is the server saying it's shutting down.
export function heardRestarting() {
  const first = !restarting()
  heardAt = Date.now()
  if (first) listeners.forEach((f) => f())
}

export function restarting(now = Date.now()) {
  return heardAt !== undefined && now - heardAt < HEARD_FOR
}

// onRestarting calls f when the server first says it's restarting.
export function onRestarting(f: () => void) {
  listeners.add(f)
  return () => {
    listeners.delete(f)
  }
}

export type Health = { version: string; bootId?: string; state?: string }

// health asks whether the server is there; it throws when it isn't.
export async function health(signal?: AbortSignal): Promise<Health> {
  const ctl = new AbortController()
  const timer = window.setTimeout(() => ctl.abort(), 5000)
  const abort = () => ctl.abort()
  signal?.addEventListener('abort', abort)
  try {
    const res = await fetch('/api/v1/health', { cache: 'no-store', signal: ctl.signal })
    if (!res.ok) throw new Error(`health: ${res.status}`)
    const h = (await res.json()) as Health
    if (h.state === 'restarting') heardRestarting()
    return h
  } finally {
    window.clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
  }
}

// What happened while the server was away.
export type Outcome = 'same' | 'restarted' | 'updated'

export function outcome(before: Health | undefined, after: Health): Outcome {
  if (!before) return 'same'
  if (before.version !== after.version) return 'updated'
  if (before.bootId && after.bootId && before.bootId !== after.bootId) return 'restarted'
  return 'same'
}

// Why a player is waiting: the server said it's restarting, or it can't be reached.
export type Waiting = 'restarting' | 'unreachable'

// backoff is how long to wait before the nth try (from 0): 1, 2, 3, then 5 seconds.
export function backoff(n: number) {
  return [1000, 2000, 3000][n] ?? 5000
}

// waitForServer asks /health until the server answers and isn't shutting
// down, calling onWait with why it's still waiting. It throws once it has
// tried for GIVE_UP_AFTER, or when signal is aborted.
export async function waitForServer(onWait: (why: Waiting) => void, signal: AbortSignal): Promise<Health> {
  const start = Date.now()
  for (let n = 0; ; n++) {
    try {
      const h = await health(signal)
      if (h.state !== 'restarting') return h
    } catch {
      // Not there yet.
    }
    if (signal.aborted) throw new Error('stopped')
    if (Date.now() - start > GIVE_UP_AFTER) throw new Error('gave up')
    onWait(restarting() ? 'restarting' : 'unreachable')
    await new Promise((r) => window.setTimeout(r, backoff(n)))
  }
}
