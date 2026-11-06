import { describe, expect, it } from 'vitest'

import { chapterMarks, chapterTitleAt } from './chapters'

const chapters = [
  { title: 'Opening', startMs: 0, endMs: 120_000 },
  { title: 'The Heist', startMs: 120_000, endMs: 480_000 },
  { startMs: 480_000, endMs: 599_000 },
  { title: 'Credits', startMs: 599_000, endMs: 600_000 },
]

describe('chapterMarks', () => {
  it('marks where each chapter after the first starts', () => {
    expect(chapterMarks(chapters, 600)).toEqual([0.2, 0.8])
  })
  it('marks nothing with fewer than two chapters or no length', () => {
    expect(chapterMarks(undefined, 600)).toEqual([])
    expect(chapterMarks([chapters[0]], 600)).toEqual([])
    expect(chapterMarks(chapters, 0)).toEqual([])
  })
  it('leaves out marks right on top of each other', () => {
    expect(chapterMarks([chapters[0], { startMs: 300_000, endMs: 300_500 }, { startMs: 300_500, endMs: 600_000 }], 600)).toEqual([0.5])
  })
})

describe('chapterTitleAt', () => {
  it('names the chapter at that moment', () => {
    expect(chapterTitleAt(chapters, 0)).toBe('Opening')
    expect(chapterTitleAt(chapters, 200)).toBe('The Heist')
    expect(chapterTitleAt(chapters, 120)).toBe('The Heist')
  })
  it('says nothing for untitled chapters', () => {
    expect(chapterTitleAt(chapters, 500)).toBeUndefined()
    expect(chapterTitleAt(undefined, 10)).toBeUndefined()
  })
})
