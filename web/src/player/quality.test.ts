import { expect, test } from 'vitest'

import * as Q from './quality'

const step720 = { id: '720p', label: '720p', width: 1280, height: 720, videoBps: 4_000_000, audioBps: 128_000 }

test('Auto when adaptive quality is on, otherwise the original', () => {
  expect(Q.startQuality({ adaptive: true, steps: [step720], cap: null })).toBe('auto')
  expect(Q.startQuality({ adaptive: false, steps: [], cap: null })).toBe('original')
  expect(Q.startQuality(undefined)).toBe('original')
})

test('a file over the cap is too wide or its bitrate is too high', () => {
  expect(Q.overCap({ width: 1920 }, step720)).toBe(true)
  expect(Q.overCap({ width: 1280, mediaInfo: { bitrate: 4_200_000 } }, step720)).toBe(false)
  expect(Q.overCap({ width: 1280, mediaInfo: { bitrate: 9_000_000 } }, step720)).toBe(true)
  expect(Q.overCap({ width: 3840 }, null)).toBe(false)
  expect(Q.overCap({}, step720)).toBe(false)
})

test('three rebuffers in two minutes are too many', () => {
  const r = new Q.Rebuffers()
  expect(r.add(0)).toBe(false)
  expect(r.add(60_000)).toBe(false)
  // The first is too long ago by now.
  expect(r.add(130_000)).toBe(false)
  expect(r.add(140_000)).toBe(true)
  r.reset()
  expect(r.add(150_000)).toBe(false)
})

test('levels are named by the master playlist, or by height', () => {
  expect(Q.levelName({ name: '1080p', height: 800 })).toBe('1080p')
  expect(Q.levelName({ name: '', height: 480 })).toBe('480p')
  expect(Q.levelName(undefined)).toBeUndefined()
})
