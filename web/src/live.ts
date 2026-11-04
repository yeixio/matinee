import type { LiveProgram } from './api'

// Helpers for live TV's guide.

/** How far through a show it is, from 0 to 1. */
export function progress(p: Pick<LiveProgram, 'start' | 'stop'>, now: number): number {
  if (p.stop <= p.start) return 0
  return Math.min(1, Math.max(0, (now - p.start) / (p.stop - p.start)))
}

/** The start of the half hour now is in, in Unix seconds. */
export function halfHour(now: number): number {
  return Math.floor(now / 1800) * 1800
}

/** Where a show sits in a guide window from..to, as fractions of its width. */
export function placement(p: Pick<LiveProgram, 'start' | 'stop'>, from: number, to: number): { left: number; width: number } | null {
  const start = Math.max(p.start, from)
  const stop = Math.min(p.stop, to)
  if (stop <= start) return null
  return { left: (start - from) / (to - from), width: (stop - start) / (to - from) }
}

/** Whether a channel matches a search and a category. */
export function matches(c: { name: string; number?: string; categories?: string[] }, q: string, category: string): boolean {
  if (category && !c.categories?.includes(category)) return false
  const s = q.trim().toLowerCase()
  return !s || c.name.toLowerCase().includes(s) || c.number === s
}
