// Each person's subtitle choices, kept on the server so they follow them to
// every device and app: when subtitles come on, in which languages, and
// how they look. "Off" always means off, whatever a file's own flags say.
import type { ItemDetail, SubtitleTrack } from '../api'

export type SubtitleMode = 'off' | 'forced' | 'always' | 'foreign'
export type SubtitleSize = 'small' | 'medium' | 'large' | 'huge'
export type SubtitleColor = 'white' | 'yellow' | 'cyan' | 'green'
export type SubtitleEdge = 'shadow' | 'outline' | 'none'
export type SubtitlePosition = 'bottom' | 'raised'

export type SubtitlePrefs = {
  /** off: never; forced: only forced lines (signs, foreign dialogue); always; foreign: when the sound isn't in one of my languages. */
  mode: SubtitleMode
  /** Languages in the order they're wanted; empty means the app's language. */
  languages?: string[]
  size?: SubtitleSize
  color?: SubtitleColor
  edge?: SubtitleEdge
  /** How solid the box behind the words is, 0 to 100. */
  background?: number
  position?: SubtitlePosition
}

export const defaultSubtitles: SubtitlePrefs = { mode: 'forced', languages: [], size: 'medium', color: 'white', edge: 'shadow', background: 55, position: 'bottom' }

/** base is a language without its region, with "eng" and "en" alike. */
export function base(lang?: string): string {
  const l = (lang ?? '').toLowerCase().split(/[-_]/)[0]
  if (!l || l === 'und' || l === 'mis' || l === 'zxx') return ''
  try {
    return new Intl.Locale(Intl.getCanonicalLocales(l)[0]).language
  } catch {
    return l
  }
}

/** wantedLanguages are the person's languages, or else the app's. */
export function wantedLanguages(p: SubtitlePrefs, appLanguage: string): string[] {
  const list = (p.languages?.length ? p.languages : [appLanguage]).map(base).filter(Boolean)
  return [...new Set(list)]
}

/** Text over pictures (nothing converted), then plain over SDH. */
const better = (a: SubtitleTrack, b: SubtitleTrack) => Number(!!a.burnIn) - Number(!!b.burnIn) || Number(!!a.sdh) - Number(!!b.sdh)

function forcedTrack(tracks: SubtitleTrack[], audioLanguage?: string): SubtitleTrack | undefined {
  const forced = tracks.filter((t) => t.forced)
  const audio = base(audioLanguage)
  return (
    (audio && forced.find((t) => base(t.language) === audio)) ||
    forced.find((t) => t.default) ||
    forced[0]
  )
}

function fullTrack(tracks: SubtitleTrack[], languages: string[]): SubtitleTrack | undefined {
  for (const lang of languages) {
    const same = tracks.filter((t) => !t.forced && base(t.language) === lang).sort(better)
    if (same[0]) return same[0]
  }
  return undefined
}

/**
 * pickSubtitles chooses what shows when playback starts. Off is off. Forced
 * lines show whenever subtitles aren't off and nothing fuller is wanted.
 */
export function pickSubtitles(
  tracks: SubtitleTrack[],
  p: SubtitlePrefs,
  o: { appLanguage: string; audioLanguage?: string },
): SubtitleTrack | undefined {
  if (p.mode === 'off') return undefined
  const languages = wantedLanguages(p, o.appLanguage)
  const audio = base(o.audioLanguage)
  const full =
    p.mode === 'always' || (p.mode === 'foreign' && !!audio && !languages.includes(audio)) ? fullTrack(tracks, languages) : undefined
  return full ?? forcedTrack(tracks, o.audioLanguage)
}

/**
 * afterPick is what a pick in the player says about what the person wants
 * next time: Off turns subtitles off, a full track turns them on in its
 * language first, and a forced track goes back to forced lines only.
 */
export function afterPick(p: SubtitlePrefs, t: SubtitleTrack | undefined, appLanguage: string): SubtitlePrefs {
  if (!t) return { ...p, mode: 'off' }
  if (t.forced) return { ...p, mode: 'forced' }
  const lang = base(t.language)
  const languages = lang ? [lang, ...wantedLanguages(p, appLanguage).filter((l) => l !== lang)] : p.languages
  return { ...p, mode: p.mode === 'foreign' ? 'foreign' : 'always', languages }
}

/**
 * fromOldChoice reads what this browser remembered before choices moved to
 * the server: a language code, "" for a track without one, or "off".
 */
export function fromOldChoice(old: string | null): SubtitlePrefs {
  if (old === null) return defaultSubtitles
  if (old === 'off') return { ...defaultSubtitles, mode: 'off' }
  return { ...defaultSubtitles, mode: 'always', languages: old ? [base(old)] : [] }
}

const sizes: Record<SubtitleSize, number> = { small: 0.75, medium: 1, large: 1.3, huge: 1.65 }
const colors: Record<SubtitleColor, string> = { white: '#ffffff', yellow: '#ffe14d', cyan: '#5ee7ff', green: '#7dff8a' }
const edges: Record<SubtitleEdge, string> = {
  shadow: '0 1px 3px rgb(0 0 0 / 0.9)',
  outline: '-1px -1px 0 #000, 1px -1px 0 #000, -1px 1px 0 #000, 1px 1px 0 #000, 0 0 3px #000',
  none: 'none',
}

/** subtitleLook is how lines are drawn for these choices. */
export function subtitleLook(p: SubtitlePrefs) {
  const scale = sizes[p.size ?? 'medium'] ?? 1
  const bg = Math.max(0, Math.min(100, p.background ?? 55))
  return {
    fontSize: `calc(clamp(1rem, 2.4vw + 0.35rem, 2.75rem) * ${scale})`,
    color: colors[p.color ?? 'white'] ?? colors.white,
    textShadow: edges[p.edge ?? 'shadow'] ?? edges.shadow,
    backgroundColor: `rgb(0 0 0 / ${bg / 100})`,
    raised: p.position === 'raised',
    scale,
  }
}

// Subtitle timing, for files whose subtitles drift: kept per title on this browser.
export const TIMING_STEP = 0.1
const timingKey = (titleId: number) => `matinee.subtitleTiming.${titleId}`

/** subtitleTiming is how many seconds later than written a title's subtitles show. */
export function subtitleTiming(titleId: number): number {
  try {
    return Number(localStorage.getItem(timingKey(titleId))) || 0
  } catch {
    return 0
  }
}

export function saveSubtitleTiming(titleId: number, seconds: number) {
  const s = Math.round(Math.max(-30, Math.min(30, seconds)) * 10) / 10
  try {
    if (s) localStorage.setItem(timingKey(titleId), String(s))
    else localStorage.removeItem(timingKey(titleId))
  } catch {
    // Lasts while the page is open.
  }
  return s
}

/** audioLanguageOf is the language a file plays in: its main sound, else the film's own. */
export function audioLanguageOf(item: Pick<ItemDetail, 'mediaInfo' | 'details'>): string | undefined {
  const audio = item.mediaInfo?.audio.filter((a) => !a.commentary && !a.descriptive) ?? []
  return (audio.find((a) => a.default) ?? audio[0])?.language || item.details?.originalLanguage
}
