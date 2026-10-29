import { expect, test } from 'vitest'

import * as L from './levels'

// Two bands, three moments a second: the bass rises, the treble falls.
const levels: L.Levels = { fps: 2, bands: 2, data: Uint8Array.from([0, 255, 255, 0, 255, 0]) }

test('the server sends levels as base64', () => {
  const l = L.decodeLevels({ fps: 20, bands: 2, levels: btoa(String.fromCharCode(0, 128, 255)) })
  expect([...l.data]).toEqual([0, 128, 255])
  expect(l.fps).toBe(20)
})

test('levels between two moments are in between', () => {
  const out = L.levelsAt(levels, 0.25, new Float32Array(2))
  expect(Math.abs(out[0] - 0.5) < 0.01).toBe(true)
  expect(Math.abs(out[1] - 0.5) < 0.01).toBe(true)
})

test('past the end, the last moment holds; before the start, the first', () => {
  expect([...L.levelsAt(levels, 99, new Float32Array(2))]).toEqual([0, 0])
  expect([...L.levelsAt(levels, -1, new Float32Array(2))]).toEqual([0, 1])
})

test('more bars than bands share them', () => {
  const out = L.levelsAt(levels, 0, new Float32Array(4))
  expect([...out]).toEqual([0, 0, 1, 1])
})

test('bars jump up and fall back slowly', () => {
  const shown = new Float32Array([0, 1])
  L.settle(shown, new Float32Array([1, 0]), 1 / 30)
  expect(shown[0] > 0.9).toBe(true)
  expect(shown[1] > 0.8 && shown[1] < 1).toBe(true)
})

test('the idle wave stays low and moves', () => {
  const a = L.idle(0, new Float32Array(24))
  const b = L.idle(1, new Float32Array(24))
  expect([...a].every((v) => v > 0 && v < 0.3)).toBe(true)
  expect([...a]).not.toEqual([...b])
})

test('the looks step round, and anything unknown is bars', () => {
  expect(L.nextStyle('pulse')).toBe('bars')
  expect(L.nextStyle('bars', -1)).toBe('pulse')
  expect(L.parseStyle('halo')).toBe('halo')
  expect(L.parseStyle('nope')).toBe('bars')
  expect(L.parseStyle(null)).toBe('bars')
})
