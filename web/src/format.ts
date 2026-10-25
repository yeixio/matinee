export function duration(ms?: number): string {
  if (!ms) return ''
  const mins = Math.max(1, Math.round(ms / 60000))
  if (mins < 60) return `${mins}m`
  return `${Math.floor(mins / 60)}h ${mins % 60}m`
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
  return `S${season ?? 0} · E${episode ?? '?'}`
}
