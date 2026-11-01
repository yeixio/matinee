import { describe, expect, it } from 'vitest'
import { inIntro, introEnd } from './intro'

describe('inIntro', () => {
  const intro = { startMs: 60_000, endMs: 90_000 }
  it('shows the button while the intro plays', () => {
    expect(inIntro(59, intro)).toBe(false)
    expect(inIntro(59.9, intro)).toBe(true)
    expect(inIntro(75, intro)).toBe(true)
  })
  it('hides it just before the intro ends, and after', () => {
    expect(inIntro(87.9, intro)).toBe(true)
    expect(inIntro(88, intro)).toBe(false)
    expect(inIntro(120, intro)).toBe(false)
  })
  it('needs an intro', () => {
    expect(inIntro(10, undefined)).toBe(false)
    expect(inIntro(10, { startMs: 5000, endMs: 5000 })).toBe(false)
  })
  it('lands at the end', () => {
    expect(introEnd(intro)).toBe(90)
  })
})
