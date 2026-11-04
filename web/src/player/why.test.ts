import { describe, expect, it } from 'vitest'
import { Arrivals, ReportGate, speedMbps, worthSaying } from './why'

describe('why playback buffered', () => {
  it('reports one bad stretch once', () => {
    const g = new ReportGate(30_000)
    expect(g.take(1000)).toBe(true)
    expect(g.take(20_000)).toBe(false)
    expect(g.take(31_001)).toBe(true)
  })
  it('says only clear causes without being asked', () => {
    expect(worthSaying({ code: 'slowConnection', at: 0 })).toBe(true)
    expect(worthSaying({ code: 'unknown', at: 0 })).toBe(false)
  })
  it('works out the connection speed', () => {
    expect(speedMbps(10_000_000, 1000)).toBe(80)
    expect(speedMbps(1, 0)).toBe(0)
  })
  it('averages how fast pieces arrived', () => {
    const a = new Arrivals(2)
    expect(a.bps()).toBe(0)
    a.add(1_000_000, 1000)
    a.add(0, 10)
    expect(a.bps()).toBe(8_000_000)
    a.add(3_000_000, 1000)
    a.add(1_000_000, 1000)
    expect(a.bps()).toBe(16_000_000)
  })
})
