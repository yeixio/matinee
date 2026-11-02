import { describe, expect, it } from 'vitest'
import { lineAt, lineProgress } from './lyrics'

const lines = [
  { ms: 1000, text: 'one' },
  { ms: 2000, text: '' },
  { ms: 2000, text: 'two' },
  { ms: 5000, text: 'three' },
]

describe('lineAt', () => {
  it('finds the line being sung', () => {
    expect(lineAt(lines, 0)).toBe(-1)
    expect(lineAt(lines, 999)).toBe(-1)
    expect(lineAt(lines, 1000)).toBe(0)
    expect(lineAt(lines, 1999)).toBe(0)
    // Two lines at once: the later one.
    expect(lineAt(lines, 2000)).toBe(2)
    expect(lineAt(lines, 60000)).toBe(3)
    expect(lineAt([], 5)).toBe(-1)
  })
})

describe('lineProgress', () => {
  it('goes from 0 to 1 until the next line', () => {
    expect(lineProgress(lines, 2, 2000)).toBe(0)
    expect(lineProgress(lines, 2, 3500)).toBe(0.5)
    expect(lineProgress(lines, 2, 9000)).toBe(1)
    expect(lineProgress(lines, 3, 7500)).toBe(0.5)
    expect(lineProgress(lines, -1, 0)).toBe(0)
  })
})
