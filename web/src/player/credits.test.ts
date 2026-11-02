import { describe, expect, it } from 'vitest'
import type { ItemDetail as Item } from '../api'
import { creditsEnd, creditsOf, inCredits } from './credits'

describe('creditsOf', () => {
  it('prefers what the server found', () => {
    const item = { endCredits: { startMs: 1000, source: 'audio' }, mediaInfo: { creditsMs: 2000 } } as unknown as Item
    expect(creditsOf(item)).toEqual({ startMs: 1000, source: 'audio' })
  })
  it('falls back to a credits chapter', () => {
    expect(creditsOf({ mediaInfo: { creditsMs: 2000 } } as unknown as Item)).toEqual({ startMs: 2000 })
    expect(creditsOf({} as Item)).toBeUndefined()
  })
})

describe('inCredits', () => {
  const credits = { startMs: 1_500_000 }
  it('shows the button while the credits play', () => {
    expect(inCredits(1499, 1800, credits)).toBe(false)
    expect(inCredits(1500, 1800, credits)).toBe(true)
    expect(inCredits(1797, 1800, credits)).toBe(true)
  })
  it('takes it away just before there is nothing left to skip', () => {
    expect(inCredits(1798.5, 1800, credits)).toBe(false)
    expect(inCredits(1600, 1800, { startMs: 1_500_000, endMs: 1_620_000 })).toBe(true)
    expect(inCredits(1619, 1800, { startMs: 1_500_000, endMs: 1_620_000 })).toBe(false)
  })
  it('needs credits inside the video', () => {
    expect(inCredits(1500, 1800)).toBe(false)
    expect(inCredits(1900, 1800, { startMs: 1_900_000 })).toBe(false)
  })
})

describe('creditsEnd', () => {
  it('lands after the credits, or at the end', () => {
    expect(creditsEnd({ startMs: 1_500_000, endMs: 1_620_000 }, 1800)).toBe(1620)
    expect(creditsEnd({ startMs: 1_500_000 }, 1800)).toBe(1800)
    expect(creditsEnd({ startMs: 1_500_000, endMs: 1_798_000 }, 1800)).toBe(1800)
  })
})
