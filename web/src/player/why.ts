import type { TFunction } from 'i18next'
import type { PlaybackCause } from '../api'

// Why playback buffered: when the picture stops to buffer for a moment, the
// player tells the server what it measured, and the server answers with the
// cause in plain words (see the server's internal/diagnose).

/** How long the player has to be stuck before it asks why. */
export const STALL_MS = 2500
/** Asks at most this often, so one bad stretch is one report. */
export const REPORT_GAP_MS = 30_000

/** Lets a report through at most once every gap. */
export class ReportGate {
  private last = -Infinity
  constructor(private gap = REPORT_GAP_MS) {}
  take(now = Date.now()) {
    if (now - this.last < this.gap) return false
    this.last = now
    return true
  }
}

/** How many milliseconds of video the element has ahead of where it plays. */
export function bufferedAheadMs(el: HTMLMediaElement) {
  const at = el.currentTime
  for (let i = 0; i < el.buffered.length; i++) {
    if (el.buffered.start(i) <= at + 0.25 && el.buffered.end(i) >= at) return Math.max(0, Math.round((el.buffered.end(i) - at) * 1000))
  }
  return 0
}

/** The cause in plain words, as one sentence. */
export function causeText(t: TFunction, c: PlaybackCause) {
  return t(`player.why.${c.code}`, c.values ?? {})
}

/** Whether a cause is clear enough to say without being asked. */
export const worthSaying = (c: PlaybackCause) => c.code !== 'unknown'

/** A few seconds of downloading noise from the server, timed (see GET /speedtest). */
export async function testConnection(signal?: AbortSignal, budgetMs = 6000): Promise<{ mbps: number; pingMs: number }> {
  const t0 = performance.now()
  await fetch('/api/v1/speedtest?bytes=0', { credentials: 'same-origin', cache: 'no-store', signal })
  const pingMs = Math.round(performance.now() - t0)
  const res = await fetch('/api/v1/speedtest?bytes=104857600', { credentials: 'same-origin', cache: 'no-store', signal })
  if (!res.ok || !res.body) throw new Error(res.statusText)
  const reader = res.body.getReader()
  const start = performance.now()
  let bytes = 0
  while (performance.now() - start < budgetMs) {
    const { done, value } = await reader.read()
    if (done) break
    bytes += value.byteLength
  }
  reader.cancel().catch(() => {})
  return { mbps: speedMbps(bytes, performance.now() - start), pingMs }
}

/** Megabits a second for bytes in ms, to one decimal. */
export function speedMbps(bytes: number, ms: number) {
  if (ms <= 0) return 0
  return Math.round(((bytes * 8) / (ms / 1000) / 1e6) * 10) / 10
}

/** How fast the last few pieces of a stream arrived, in bits a second. */
export class Arrivals {
  private samples: { bytes: number; ms: number }[] = []
  constructor(private keep = 6) {}
  add(bytes: number, ms: number) {
    if (bytes <= 0 || ms <= 0) return
    this.samples.push({ bytes, ms })
    if (this.samples.length > this.keep) this.samples.shift()
  }
  /** 0 until a piece has arrived. */
  bps() {
    const bytes = this.samples.reduce((n, s) => n + s.bytes, 0)
    const ms = this.samples.reduce((n, s) => n + s.ms, 0)
    return ms > 0 ? Math.round((bytes * 8 * 1000) / ms) : 0
  }
}
