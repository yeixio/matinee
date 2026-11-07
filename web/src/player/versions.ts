// A movie or episode can have several files: a 4K copy and a 1080p one, or
// the theatrical and the extended cut. The title page lets people pick one,
// and otherwise the one that suits this screen plays.
import type { HDR, Version } from '../api'
import { hdrLabel, sizeLabel } from '../media'
import { duration } from '../format'

/** resolutionLabel names a picture height the way people know it. */
export function resolutionLabel(height?: number): string {
  if (!height) return ''
  if (height >= 2000) return '4K'
  if (height >= 1400) return '1440p'
  if (height >= 1000) return '1080p'
  if (height >= 700) return '720p'
  if (height >= 560) return '576p'
  return `${height}p`
}

const hdrs: HDR[] = ['dolby-vision', 'hdr10+', 'hdr10', 'hlg']

/** versionLabel tells versions apart: "4K · Dolby Vision · Director's Cut". */
export function versionLabel(v: Version): string {
  const hdr = hdrs.find((h) => v.mediaTags?.includes(h))
  return [resolutionLabel(v.height), hdr && hdrLabel[hdr], v.edition].filter(Boolean).join(' · ')
}

/**
 * versionDetail is the smaller print: how long a cut runs (an edition is
 * often told apart by its length alone), then size and codecs.
 */
export function versionDetail(v: Version): string {
  return [v.edition && v.durationMs ? duration(v.durationMs) : '', v.size ? sizeLabel(v.size) : '', v.videoCodec?.toUpperCase(), v.container?.toUpperCase()]
    .filter(Boolean)
    .join(' · ')
}

export type PickOptions = {
  /** The tallest picture worth sending: the screen's, in device pixels. */
  maxHeight: number
  /** Whether a version plays here as it is, with nothing converted. */
  plays?: (v: Version) => boolean
}

/**
 * pickVersion chooses what plays when nobody picked: the biggest picture
 * that fits the screen, then one that plays as it is, then the title's own.
 * When none fits, the smallest.
 */
export function pickVersion(versions: Version[], o: PickOptions): Version | undefined {
  if (versions.length === 0) return undefined
  const fits = (v: Version) => (v.height ?? 0) <= o.maxHeight * 1.1
  const rank = (v: Version, i: number) => [
    fits(v) ? 1 : 0,
    fits(v) ? (v.height ?? 0) : -(v.height ?? 0),
    o.plays?.(v) ? 1 : 0,
    -i,
  ]
  let best = 0
  let bestRank = rank(versions[0], 0)
  versions.forEach((v, i) => {
    const r = rank(v, i)
    for (let k = 0; k < r.length; k++) {
      if (r[k] === bestRank[k]) continue
      if (r[k] > bestRank[k]) {
        best = i
        bestRank = r
      }
      break
    }
  })
  return versions[best]
}

const storageKey = (titleId: number) => `matinee.version.${titleId}`

/** chosenVersion is the version picked for a title on this browser, if any. */
export function chosenVersion(titleId: number, versions: Version[]): Version | undefined {
  try {
    const id = Number(localStorage.getItem(storageKey(titleId)))
    return versions.find((v) => v.id === id)
  } catch {
    return undefined
  }
}

/** chooseVersion remembers the version picked for a title on this browser. */
export function chooseVersion(titleId: number, versionId: number) {
  try {
    localStorage.setItem(storageKey(titleId), String(versionId))
  } catch {
    // Private windows can refuse; the pick then lasts for this page only.
  }
}

/** screenHeight is the screen's short side in device pixels. */
export function screenHeight(): number {
  const s = typeof window === 'undefined' ? undefined : window.screen
  if (!s?.height) return 1080
  return Math.round(Math.min(s.width || s.height, s.height) * (window.devicePixelRatio || 1))
}
