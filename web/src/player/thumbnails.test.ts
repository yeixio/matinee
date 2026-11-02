import { describe, expect, it } from 'vitest'

import { askAgain, sheetUrl, spotAt, type Thumbnails } from './thumbnails'

const t: Thumbnails = { state: 'ready', intervalMs: 10_000, width: 240, height: 134, columns: 10, rows: 10, count: 712, sheets: 8, tag: 'x1' }

describe('spotAt', () => {
  it('finds the picture on its sheet', () => {
    expect(spotAt(t, 0)).toEqual({ sheet: 0, x: 0, y: 0 })
    expect(spotAt(t, 9.9)).toEqual({ sheet: 0, x: 0, y: 0 })
    expect(spotAt(t, 10)).toEqual({ sheet: 0, x: 240, y: 0 })
    expect(spotAt(t, 125)).toEqual({ sheet: 0, x: 480, y: 134 })
    expect(spotAt(t, 1000)).toEqual({ sheet: 1, x: 0, y: 0 })
  })
  it('stays within the video', () => {
    expect(spotAt(t, -5)).toEqual({ sheet: 0, x: 0, y: 0 })
    expect(spotAt(t, 99_999)).toEqual({ sheet: 7, x: 240, y: 134 })
  })
  it('has nothing without pictures', () => {
    expect(spotAt(undefined, 10)).toBeNull()
    expect(spotAt({ state: 'making' }, 10)).toBeNull()
  })
})

describe('sheets', () => {
  it('carry the tag', () => expect(sheetUrl(5, 2, 'x1')).toBe('/api/v1/items/5/thumbnails/2?v=x1'))
  it('are asked for again while being made, for a while', () => {
    expect(askAgain({ state: 'making' }, 1)).toBe(20_000)
    expect(askAgain({ state: 'making' }, 15)).toBe(false)
    expect(askAgain(t, 1)).toBe(false)
  })
})
