import { describe, expect, it } from 'vitest'
import { barLetters, jumpIndex } from './AlphaBar'

const letters = [
  { letter: '#', index: 0, count: 2 },
  { letter: 'A', index: 2, count: 5 },
  { letter: 'M', index: 7, count: 3 },
  { letter: 'Я', index: 10, count: 1 },
]

describe('A to Z bar', () => {
  it('shows # and A to Z, then other letters the list has', () => {
    const bar = barLetters(letters, false)
    expect(bar.slice(0, 3)).toEqual(['#', 'A', 'B'])
    expect(bar.at(-1)).toBe('Я')
    expect(barLetters(letters, true)[0]).toBe('Я')
  })
  it('jumps to a letter, or the next one down that has titles', () => {
    const bar = barLetters(letters, false)
    expect(jumpIndex(letters, bar, 'A')).toBe(2)
    expect(jumpIndex(letters, bar, 'B')).toBe(7)
    expect(jumpIndex(letters, bar, 'Z')).toBe(10)
    const desc = [...letters].reverse().map((l, i) => ({ ...l, index: i }))
    expect(jumpIndex(desc, barLetters(desc, true), 'B')).toBe(desc.find((l) => l.letter === 'A')!.index)
  })
})
