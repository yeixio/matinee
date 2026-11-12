import { describe, expect, it } from 'vitest'
import { Btn, padMask } from './stream'

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

  it('ignores the guide button', () => {
    expect(padMask(pad([16]))).toBe(0)
  })
})
