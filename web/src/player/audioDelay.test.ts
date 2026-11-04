import { beforeEach, describe, expect, it } from 'vitest'
import { clampDelay, delayLabel, loadDelay, saveDelay } from './audioDelay'

describe('audio delay', () => {
  beforeEach(() => localStorage.clear())
  it('steps and limits', () => {
    expect(clampDelay(37)).toBe(25)
    expect(clampDelay(-5000)).toBe(-2000)
  })
  it('belongs to the browser unless kept for a title', () => {
    saveDelay(1, { ms: -150, forTitle: false })
    expect(loadDelay(2)).toEqual({ ms: -150, forTitle: false })
    saveDelay(2, { ms: 75, forTitle: true })
    expect(loadDelay(2)).toEqual({ ms: 75, forTitle: true })
    expect(loadDelay(3)).toEqual({ ms: -150, forTitle: false })
    saveDelay(2, { ms: 0, forTitle: false })
    expect(loadDelay(2)).toEqual({ ms: 0, forTitle: false })
  })
  it('labels', () => {
    expect(delayLabel(125, 'en')).toBe('+125 ms')
    expect(delayLabel(-50, 'en')).toBe('−50 ms')
  })
})
