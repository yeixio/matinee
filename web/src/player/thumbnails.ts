// The seek bar's preview pictures (GET /items/{id}/thumbnails): small
// frames of the video, a few seconds apart, packed into JPEG sheets.

export type Thumbnails = {
  /** ready: there are pictures. making: on their way, ask again soon. none or off: there are none. */
  state: 'ready' | 'making' | 'none' | 'off'
  intervalMs?: number
  width?: number
  height?: number
  columns?: number
  rows?: number
  count?: number
  sheets?: number
  tag?: string
}

/** Where the picture for a moment is: which sheet, and its top-left corner on it. */
export type Spot = { sheet: number; x: number; y: number }

/** The picture to show for the moment sec seconds in, or null when there are none. */
export function spotAt(t: Thumbnails | undefined, sec: number): Spot | null {
  if (!t || t.state !== 'ready' || !t.intervalMs || !t.count || !t.columns || !t.rows || !t.width || !t.height) return null
  const i = Math.min(t.count - 1, Math.max(0, Math.floor((sec * 1000) / t.intervalMs)))
  const per = t.columns * t.rows
  const k = i % per
  return { sheet: Math.floor(i / per), x: (k % t.columns) * t.width, y: Math.floor(k / t.columns) * t.height }
}

export const sheetUrl = (id: number, sheet: number, tag?: string) =>
  `/api/v1/items/${id}/thumbnails/${sheet}${tag ? `?v=${encodeURIComponent(tag)}` : ''}`

/** How long to wait before asking again, while they're being made; false to stop asking. */
export function askAgain(t: Thumbnails | undefined, asked: number): number | false {
  return t?.state === 'making' && asked < 15 ? 20_000 : false
}
