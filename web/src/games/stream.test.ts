import { describe, expect, it } from 'vitest'
import { Btn, padMask, stickValues } from './stream'

const pad = (pressed: number[], axes = [0, 0]) => ({
  buttons: Array.from({ length: 17 }, (_, i) => ({ pressed: pressed.includes(i), touched: false, value: pressed.includes(i) ? 1 : 0 })),
  axes,
})

describe('padMask', () => {
  it('maps face buttons by position', () => {
    // Bottom face button is libretro's B, right is A.
    expect(padMask(pad([0]))).toBe(1 << Btn.B)
    expect(padMask(pad([1]))).toBe(1 << Btn.A)
    expect(padMask(pad([9, 8]))).toBe((1 << Btn.Start) | (1 << Btn.Select))
  })

  it('reads the d-pad and the left stick', () => {
    expect(padMask(pad([12, 15]))).toBe((1 << Btn.Up) | (1 << Btn.Right))
    expect(padMask(pad([], [-0.9, 0.8]))).toBe((1 << Btn.Left) | (1 << Btn.Down))
    expect(padMask(pad([], [0.3, -0.2]))).toBe(0)
  })

  it('leaves the stick alone for a console with sticks', () => {
    expect(padMask(pad([12], [-0.9, 0.8]), false)).toBe(1 << Btn.Up)
  })

  it('ignores the guide button', () => {
    expect(padMask(pad([16]))).toBe(0)
  })
})

describe('stickValues', () => {
  it('sends sticks as signed bytes with a dead zone', () => {
    expect(stickValues({ axes: [1, -1, 0.05, -0.5] })).toEqual([127, -127, 0, -63])
    expect(stickValues({ axes: [] })).toEqual([0, 0, 0, 0])
  })
})
