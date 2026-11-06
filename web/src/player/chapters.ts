// A video's own chapters (the item's chapters), as marks on the seek bar
// and a name under the seek preview.

import type { Chapter } from '../api'

/** Where on the bar each chapter after the first starts, from 0 to 1. */
export function chapterMarks(chapters: Chapter[] | undefined, durationSec: number): number[] {
  if (!chapters || chapters.length < 2 || !(durationSec > 0)) return []
  const total = durationSec * 1000
  const out: number[] = []
  for (const c of chapters) {
    const f = c.startMs / total
    // A mark at either end, or on top of the one before, says nothing.
    if (f <= 0.005 || f >= 0.995 || (out.length && f - out[out.length - 1] < 0.003)) continue
    out.push(f)
  }
  return out
}

/** The title of the chapter playing sec seconds in, or undefined. */
export function chapterTitleAt(chapters: Chapter[] | undefined, sec: number): string | undefined {
  if (!chapters || chapters.length < 2) return undefined
  const ms = sec * 1000
  let at: Chapter | undefined
  for (const c of chapters) {
    if (c.startMs > ms) break
    at = c
  }
  return at?.title?.trim() || undefined
}
