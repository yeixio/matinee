import type { HDR } from '../api'

// What this browser takes as it is inside the server's HLS, so a picture
// it decodes (HEVC or AV1, 4K, HDR) is repackaged rather than converted.

/** What the server's ?video= and ?hdr= say. */
export type Plays = { video: string[]; hdr: HDR[] }

// Main 10 at 4K, the most a file usually asks of a decoder.
const probes: Record<string, string> = { hevc: 'hvc1.2.4.L153.B0', av1: 'av01.0.12M.10' }

type Env = {
  /** MediaSource.isTypeSupported, where hls.js plays the stream. */
  mse?: (type: string) => boolean
  /** The video element's canPlayType, where the browser plays HLS itself. */
  native?: (type: string) => string
  /** Whether the screen shows HDR: (dynamic-range: high). */
  hdrScreen?: boolean
}

/** Asks the browser what it decodes in fMP4 and whether its screen shows HDR. */
export function browserPlays(env: Env): Plays {
  const video = ['h264']
  for (const [codec, probe] of Object.entries(probes)) {
    const type = `video/mp4; codecs="${probe}"`
    if (env.mse?.(type) || (env.native && env.native(type) !== '')) video.push(codec)
  }
  // Browsers on an HDR screen show HDR10 and HLG; Dolby Vision is left to the apps.
  return { video, hdr: env.hdrScreen ? ['hdr10', 'hlg'] : [] }
}

/** This browser's Plays. */
export function currentPlays(el: HTMLVideoElement): Plays {
  const MS = (globalThis as { MediaSource?: { isTypeSupported(t: string): boolean } }).MediaSource
  return browserPlays({
    mse: MS ? (t) => MS.isTypeSupported(t) : undefined,
    native: el.canPlayType('application/vnd.apple.mpegurl') ? (t) => el.canPlayType(t) : undefined,
    hdrScreen: typeof matchMedia === 'function' && matchMedia('(dynamic-range: high)').matches,
  })
}

type Picture = { videoCodec?: string; mediaInfo?: { video?: { hdr?: HDR; hdrBase?: HDR }[] } }

/**
 * Whether the server copies this file's picture for a player with plays,
 * as it decides it (internal/stream/plays.go): it decodes the codec and its
 * screen shows the HDR. Then Auto asks for the original, repackaged, and
 * only moves to an adaptive stream when the connection can't keep up.
 */
export function repackages(item: Picture, plays: Plays): boolean {
  const codec = item.videoCodec
  if (!codec || !plays.video.includes(codec)) return false
  if (codec === 'h264') return true
  const v = item.mediaInfo?.video?.[0]
  if (!v) return plays.hdr.includes('hdr10') && plays.hdr.includes('hlg')
  return shows(plays.hdr, v.hdr, v.hdrBase)
}

function shows(screen: HDR[], hdr?: HDR, base?: HDR): boolean {
  if (!hdr) return true
  if (hdr === 'hdr10+') return screen.includes('hdr10+') || screen.includes('hdr10')
  if (hdr === 'dolby-vision') return screen.includes('dolby-vision') || (!!base && shows(screen, base))
  return screen.includes(hdr)
}

/** The query the server's HLS start takes for plays. */
export function playsQuery(plays: Plays): string {
  return `&video=${plays.video.join(',')}${plays.hdr.length ? `&hdr=${encodeURIComponent(plays.hdr.join(','))}` : ''}`
}
