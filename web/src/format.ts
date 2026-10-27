import i18n, { formatLocale } from './i18n'

export function duration(ms?: number): string {
  if (!ms) return ''
  const mins = Math.max(1, Math.round(ms / 60000))
  if (mins < 60) return i18n.t('common:duration.minutes', { minutes: mins })
  return i18n.t('common:duration.hoursMinutes', { hours: Math.floor(mins / 60), minutes: mins % 60 })
}

export function clock(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  const mm = h ? String(m).padStart(2, '0') : String(m)
  return `${h ? h + ':' : ''}${mm}:${String(s).padStart(2, '0')}`
}

export function resolution(width?: number, height?: number): string {
  if (!width || !height) return ''
  if (width >= 3800) return '4K'
  if (width >= 1900) return '1080p'
  if (width >= 1260) return '720p'
  return 'SD'
}

// A stable hue per title, so placeholder posters differ but never flicker.
export function hue(text: string): number {
  let h = 0
  for (let i = 0; i < text.length; i++) h = (h * 31 + text.charCodeAt(i)) >>> 0
  return h % 360
}

export function progressFraction(positionMs?: number, durationMs?: number): number {
  if (!positionMs || !durationMs) return 0
  return Math.min(1, positionMs / durationMs)
}

// "S1 · E2" for an episode.
export function episodeCode(season?: number, episode?: number): string {
  return i18n.t('common:episodeCode', { season: season ?? 0, episode: episode ?? '?' })
}

// A season's name: the online match's, or "Season 2" and "Specials" in the
// page's language for the names a scan gives.
export function seasonTitle(item: { kind: string; title: string; index?: number }): string {
  if (item.kind !== 'season') return item.title
  if (item.index === 0 && (item.title === 'Specials' || item.title === 'Season 0')) return i18n.t('common:specials')
  const n = /^Season (\d+)$/.exec(item.title)
  return n ? i18n.t('common:seasonN', { season: Number(n[1]) }) : item.title
}

// A rating out of 10 with one decimal, such as 7.9 or 7,9.
export function ratingLabel(rating: number): string {
  return new Intl.NumberFormat(formatLocale(), { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(rating)
}
