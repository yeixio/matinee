import { describe, expect, it } from 'vitest'

import { browserPlays, playsQuery, repackages } from './plays'

describe('browserPlays', () => {
  it('asks MSE for HEVC and AV1', () => {
    const p = browserPlays({ mse: (t) => t.includes('hvc1'), hdrScreen: true })
    expect(p).toEqual({ video: ['h264', 'hevc'], hdr: ['hdr10', 'hlg'] })
  })
  it('asks the native HLS player when there is no MSE', () => {
    const p = browserPlays({ native: (t) => (t.includes('av01') ? '' : 'probably') })
    expect(p).toEqual({ video: ['h264', 'hevc'], hdr: [] })
  })
  it('asks for Dolby Vision on an HDR screen', () => {
    const p = browserPlays({ mse: (t) => t.includes('hvc1') || t.includes('dvh1'), hdrScreen: true })
    expect(p.hdr).toEqual(['hdr10', 'hlg', 'dolby-vision'])
    expect(browserPlays({ mse: (t) => t.includes('hvc1') || t.includes('dvh1') }).hdr).toEqual([])
  })
  it('takes only H.264 when nothing can be asked', () => {
    expect(browserPlays({})).toEqual({ video: ['h264'], hdr: [] })
  })
})

describe('repackages', () => {
  const hdr = { video: ['h264', 'hevc'], hdr: ['hdr10' as const, 'hlg' as const] }
  const sdr = { video: ['h264', 'hevc'], hdr: [] }
  it('copies H.264 for anyone', () => expect(repackages({ videoCodec: 'h264' }, { video: ['h264'], hdr: [] })).toBe(true))
  it('needs the codec', () => expect(repackages({ videoCodec: 'av1', mediaInfo: { video: [{}] } }, hdr)).toBe(false))
  it('copies SDR HEVC', () => expect(repackages({ videoCodec: 'hevc', mediaInfo: { video: [{}] } }, sdr)).toBe(true))
  it('needs the HDR', () => {
    expect(repackages({ videoCodec: 'hevc', mediaInfo: { video: [{ hdr: 'hdr10' }] } }, sdr)).toBe(false)
    expect(repackages({ videoCodec: 'hevc', mediaInfo: { video: [{ hdr: 'hdr10+' }] } }, hdr)).toBe(true)
    expect(repackages({ videoCodec: 'hevc', mediaInfo: { video: [{ hdr: 'dolby-vision', hdrBase: 'hdr10' }] } }, hdr)).toBe(true)
    expect(repackages({ videoCodec: 'hevc', mediaInfo: { video: [{ hdr: 'dolby-vision' }] } }, hdr)).toBe(false)
  })
  it('plays Dolby Vision profile 7 as its HDR10 base', () => {
    const p7 = { videoCodec: 'hevc', mediaInfo: { video: [{ hdr: 'dolby-vision' as const, hdrBase: 'hdr10' as const, dvProfile: 7 }] } }
    expect(repackages(p7, { video: ['hevc'], hdr: ['dolby-vision'] })).toBe(false)
    expect(repackages(p7, hdr)).toBe(true)
  })
  it('takes a file not looked at yet only on an HDR screen', () => {
    expect(repackages({ videoCodec: 'hevc' }, sdr)).toBe(false)
    expect(repackages({ videoCodec: 'hevc' }, hdr)).toBe(true)
  })
})

it('playsQuery', () => {
  expect(playsQuery({ video: ['h264', 'hevc'], hdr: ['hdr10+'] })).toBe('&video=h264,hevc&hdr=hdr10%2B')
  expect(playsQuery({ video: ['h264'], hdr: [] })).toBe('&video=h264')
})
