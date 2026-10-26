import { describe, expect, it } from 'vitest'
import type { ItemDetail, MediaInfo } from './api'
import { audioLanguages, bitrateLabel, channelLabel, codecName, languageName, qualityBadges, sizeLabel } from './media'

const base: ItemDetail = { id: 1, libraryId: 1, kind: 'movie', title: 'Film', addedAt: 0, playable: true, hasPoster: false, hasBackdrop: false, parents: [] }

const remux: MediaInfo = {
  container: 'matroska',
  size: 61e9,
  durationMs: 7_380_000,
  analyzedAt: 0,
  video: [{ index: 0, codec: 'hevc', width: 3840, height: 2160, resolution: '4K', hdr: 'dolby-vision', hdrBase: 'hdr10', dvProfile: 8 }],
  audio: [
    { index: 0, codec: 'truehd', channels: 8, channelLayout: '7.1', atmos: true, language: 'eng', default: true },
    { index: 1, codec: 'ac3', channels: 6, channelLayout: '5.1(side)', language: 'fre' },
    { index: 2, codec: 'ac3', channels: 2, language: 'eng', commentary: true },
  ],
  subtitles: [],
}

describe('qualityBadges', () => {
  it('sums up an analyzed file', () => {
    expect(qualityBadges({ ...base, mediaInfo: remux }).map((b) => b.label)).toEqual(['4K', 'Dolby Vision', 'Dolby Atmos', '7.1'])
  })
  it("uses a show's tags", () => {
    expect(qualityBadges({ ...base, kind: 'show', mediaTags: ['1080p', '4k', 'hdr', 'hdr10', 'hevc', '5.1'] }).map((b) => b.label)).toEqual([
      '4K',
      'HDR10',
      '5.1',
    ])
  })
  it('falls back to the scanned size', () => {
    expect(qualityBadges({ ...base, width: 1920, height: 1036 }).map((b) => b.label)).toEqual(['1080p'])
  })
  it('labels music', () => {
    const flac: MediaInfo = { ...remux, video: [], audio: [{ index: 0, codec: 'flac', channels: 2, bitDepth: 24, sampleRate: 96000 }] }
    expect(qualityBadges({ ...base, kind: 'track', mediaInfo: flac }).map((b) => b.label)).toEqual(['Hi-Res'])
  })
})

describe('labels', () => {
  it('names things', () => {
    expect(audioLanguages(remux)).toEqual(['English', 'French'])
    expect(languageName('und')).toBe('')
    expect(channelLabel(6, '5.1(side)')).toBe('5.1')
    expect(channelLabel(2)).toBe('Stereo')
    expect(codecName('dts', 'DTS-HD MA + DTS:X')).toBe('DTS-HD MA')
    expect(bitrateLabel(58_000_000)).toBe('58 Mb/s')
    expect(bitrateLabel(640_000)).toBe('640 kb/s')
    expect(sizeLabel(61e9)).toBe('61.0 GB')
  })
})
