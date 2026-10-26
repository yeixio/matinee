import { describe, expect, it } from 'vitest'
import { STILL_WATCHING_AFTER, stillWatching, upNextAt } from './autoplay'

describe('upNextAt', () => {
  it('shows the card where the credits start', () => {
    expect(upNextAt(1800, 1_620_500)).toBe(1620.5)
  })
  it('falls back to 30 seconds before the end', () => {
    expect(upNextAt(1800)).toBe(1770)
    expect(upNextAt(1800, 1_900_000)).toBe(1770) // a chapter past the end doesn't count
  })
  it('waits for the end of very short videos', () => {
    expect(upNextAt(90)).toBe(90)
  })
  it('needs a duration', () => {
    expect(upNextAt(0)).toBeUndefined()
  })
})

describe('stillWatching', () => {
  it('asks after three episodes in a row nobody touched', () => {
    stillWatching.input()
    for (let i = 0; i < STILL_WATCHING_AFTER; i++) {
      expect(stillWatching.shouldAsk()).toBe(false)
      stillWatching.autoPlayed()
    }
    expect(stillWatching.shouldAsk()).toBe(true)
    stillWatching.input()
    expect(stillWatching.shouldAsk()).toBe(false)
  })
})
