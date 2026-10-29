import { describe, expect, it } from 'vitest'
import type { Book } from '../api'
import { bookLength, bookTime, chapterAt, locate, nextChapterStart, previousChapterStart, timeLeft } from './book'

const part = (id: number, startMs: number, durationMs: number) =>
  ({ id, startMs, durationMs, libraryId: 1, kind: 'part', title: `Part ${id}`, addedAt: 0, playable: true, hasPoster: false, hasBackdrop: false, mimeType: 'audio/mpeg' })

// Two files: the first has two chapters of its own, the second is one.
const book = {
  id: 1, libraryId: 1, kind: 'book', title: 'Book', addedAt: 0, playable: false, hasPoster: false, hasBackdrop: false,
  parts: [part(10, 0, 600_000), part(11, 600_000, 300_000)],
  chapters: [
    { title: 'One', startMs: 0, endMs: 200_000, partId: 10 },
    { title: 'Two', startMs: 200_000, endMs: 600_000, partId: 10 },
    { title: 'Three', startMs: 600_000, endMs: 900_000, partId: 11 },
  ],
} as Book

describe('book clock', () => {
  it('runs across parts', () => {
    expect(bookLength(book)).toBe(900)
    expect(locate(book, 0)).toEqual({ part: 0, offset: 0 })
    expect(locate(book, 650)).toEqual({ part: 1, offset: 50 })
    expect(locate(book, 600)).toEqual({ part: 1, offset: 0 })
    expect(bookTime(book, 1, 50)).toBe(650)
  })

  it('finds the chapter playing', () => {
    expect(chapterAt(book.chapters, 0)).toBe(0)
    expect(chapterAt(book.chapters, 250)).toBe(1)
    expect(chapterAt(book.chapters, 899)).toBe(2)
    expect(chapterAt([], 10)).toBe(-1)
  })

  it('goes back to this chapter, or the one before when it just began', () => {
    expect(previousChapterStart(book.chapters, 300)).toBe(200)
    expect(previousChapterStart(book.chapters, 201)).toBe(0)
    expect(previousChapterStart(book.chapters, 1)).toBe(0)
    expect(nextChapterStart(book.chapters, 300)).toBe(600)
    expect(nextChapterStart(book.chapters, 700)).toBeUndefined()
  })

  it('counts time left at the speed', () => {
    expect(timeLeft(300, 900, 2)).toBe(300)
    expect(timeLeft(950, 900, 1)).toBe(0)
  })
})
