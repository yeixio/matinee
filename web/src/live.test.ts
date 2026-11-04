import { describe, expect, it } from 'vitest'
import { halfHour, matches, placement, progress } from './live'

describe('live TV guide', () => {
  it('measures how far through a show it is', () => {
    expect(progress({ start: 100, stop: 200 }, 150)).toBe(0.5)
    expect(progress({ start: 100, stop: 200 }, 50)).toBe(0)
    expect(progress({ start: 100, stop: 200 }, 250)).toBe(1)
  })
  it('starts the guide on the half hour', () => {
    expect(halfHour(1800 * 10 + 1799)).toBe(1800 * 10)
  })
  it('places shows in the window, clipped', () => {
    expect(placement({ start: 0, stop: 100 }, 50, 150)).toEqual({ left: 0, width: 0.5 })
    expect(placement({ start: 200, stop: 300 }, 50, 150)).toBeNull()
  })
  it('finds channels by name, number and category', () => {
    const c = { name: 'News One', number: '7', categories: ['News'] }
    expect(matches(c, 'news', '')).toBe(true)
    expect(matches(c, '7', 'News')).toBe(true)
    expect(matches(c, '', 'Movies')).toBe(false)
  })
})
