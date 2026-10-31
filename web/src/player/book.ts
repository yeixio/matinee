// Audiobook playback, as plain functions the player and its tests share. A
// book plays as one long listen across its parts (files); every time here
// is on the book's own clock, in seconds, which runs across the parts.
import type { Book, BookChapter, Item } from '../api'

// Speeds the speed menu offers.
export const SPEEDS = [0.75, 0.9, 1, 1.1, 1.2, 1.3, 1.5, 1.75, 2, 2.5, 3]
// How far the back and ahead buttons jump, in seconds.
export const SKIP_BACK = 15
export const SKIP_AHEAD = 30
// Back this far into a chapter, the previous-chapter button goes to the
// chapter before rather than the start of this one.
const RESTART_WITHIN = 3

// The book's length in seconds.
export function bookLength(b: Book): number {
  const last = b.parts[b.parts.length - 1]
  if (!last) return 0
  return (last.startMs + (last.durationMs ?? 0)) / 1000
}

// Which part holds a time on the book's clock, and how far into it.
export function locate(b: Book, sec: number): { part: number; offset: number } {
  const ms = Math.max(0, sec * 1000)
  let i = 0
  while (i + 1 < b.parts.length && b.parts[i + 1].startMs <= ms) i++
  const start = b.parts[i]?.startMs ?? 0
  return { part: i, offset: (ms - start) / 1000 }
}

// The time on the book's clock for so far into a part.
export function bookTime(b: Book, part: number, offset: number): number {
  return (b.parts[part]?.startMs ?? 0) / 1000 + offset
}

// The chapter playing at a time, by index; -1 when the book has none.
export function chapterAt(chapters: BookChapter[], sec: number): number {
  const ms = sec * 1000
  let at = -1
  for (let i = 0; i < chapters.length; i++) {
    if (chapters[i].startMs <= ms + 1) at = i
    else break
  }
  return at < 0 && chapters.length ? 0 : at
}

// Where the previous-chapter button goes: the start of this chapter, or of
// the one before when it's just begun.
export function previousChapterStart(chapters: BookChapter[], sec: number): number {
  const i = chapterAt(chapters, sec)
  if (i < 0) return 0
  const start = chapters[i].startMs / 1000
  if (sec - start > RESTART_WITHIN || i === 0) return start
  return chapters[i - 1].startMs / 1000
}

// Where the next-chapter button goes, or undefined in the last chapter.
export function nextChapterStart(chapters: BookChapter[], sec: number): number | undefined {
  const next = chapters[chapterAt(chapters, sec) + 1]
  return next ? next.startMs / 1000 : undefined
}

// Seconds of listening left at a speed: an hour at 2× is half an hour.
export function timeLeft(sec: number, until: number, speed: number): number {
  return Math.max(0, until - sec) / (speed || 1)
}

// The speed someone last picked, per profile; 1 when none.
const speedKey = (user?: number) => `matinee.books.speed.${user ?? 0}`

export function savedSpeed(user?: number): number {
  try {
    const v = Number(localStorage.getItem(speedKey(user)))
    return SPEEDS.includes(v) ? v : 1
  } catch {
    return 1
  }
}

export function saveSpeed(user: number | undefined, speed: number) {
  try {
    localStorage.setItem(speedKey(user), String(speed))
  } catch {
    // no storage: the speed lasts until the page closes
  }
}

// "1.25×" in the page's language.
export function speedLabel(speed: number, locale?: string): string {
  return `${speed.toLocaleString(locale, { maximumFractionDigits: 2 })}×`
}

// A podcast episode plays like a book of one part with no chapters, so it
// gets the book player: speed, skipping back and ahead, the sleep timer and
// its place kept on the server. The bar names the show where a book names
// its author.
export function episodeBook(ep: Item): Book {
  return {
    ...ep,
    author: ep.podcast ?? ep.author,
    parts: [{ ...ep, startMs: 0, mimeType: '' }],
    chapters: [],
  }
}

export const isEpisode = (b?: Pick<Item, 'kind'>) => b?.kind === 'podcastEpisode'
