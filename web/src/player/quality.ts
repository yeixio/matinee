import type { QualityStep, Streaming } from '../api'

// Streaming quality (see GET /streaming). With adaptive quality on, people
// pick Auto, one quality step, or Original. Auto plays the file as it is
// when the device can, and moves to an adaptive stream (one that steps the
// picture down and back up with the connection) when it keeps buffering;
// a file the device can't play goes straight to the adaptive stream.
export type Quality = 'auto' | 'original' | string

// The quality a player starts with.
export function startQuality(info: Streaming | undefined): Quality {
  return info?.adaptive ? 'auto' : 'original'
}

// overCap says whether a file is better than the cap for streaming away
// from home, so it has to play through a transcode at the cap instead.
export function overCap(item: { width?: number; mediaInfo?: { bitrate?: number } }, cap: QualityStep | null | undefined): boolean {
  if (!cap) return false
  if (item.width && item.width > cap.width) return true
  const bitrate = item.mediaInfo?.bitrate
  return !!bitrate && bitrate > (cap.videoBps + cap.audioBps) * 1.15
}

// Buffering this many times within the window, or once for this long,
// means the connection can't keep up with the file as it is.
export const REBUFFERS = 3
export const REBUFFER_WINDOW_MS = 2 * 60_000
export const LONG_STALL_MS = 5_000

// Rebuffers counts the times playback stopped to buffer (not counting
// seeks), and says when there have been too many lately.
export class Rebuffers {
  private times: number[] = []
  // add records one, and reports whether that makes too many.
  add(now = Date.now()): boolean {
    this.times = this.times.filter((t) => now - t < REBUFFER_WINDOW_MS)
    this.times.push(now)
    return this.times.length >= REBUFFERS
  }
  reset() {
    this.times = []
  }
}

// levelName is what a step of an adaptive stream is called: its NAME in
// the master playlist, or its height.
export function levelName(level: { name?: string; height?: number } | undefined): string | undefined {
  if (!level) return undefined
  return level.name || (level.height ? `${level.height}p` : undefined)
}
