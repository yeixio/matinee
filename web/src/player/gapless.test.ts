import { describe, expect, it } from 'vitest'
import type { Item } from '../api'
import * as G from './gapless'
import * as Q from './queue'

const song = (id: number, index: number, extra: Partial<Item> = {}): Item =>
  ({ id, kind: 'track', title: `Song ${id}`, parentId: 1, index, gapless: true, durationMs: 60_000, hasPoster: false, hasBackdrop: false, ...extra }) as Item

describe('runFrom', () => {
  const album = [song(1, 1), song(2, 2), song(3, 3)]
  it('joins the album from the song playing', () => {
    const q = Q.start(Q.emptyQueue, album, 1, false)
    expect(G.runFrom(q).map((e) => e.item.id)).toEqual([2, 3])
  })
  it('stops at a song from elsewhere, or out of order', () => {
    const q = Q.start(Q.emptyQueue, [song(1, 1), song(2, 2), song(9, 1, { parentId: 7 }), song(3, 3)], 0, false)
    expect(G.runFrom(q).map((e) => e.item.id)).toEqual([1, 2])
    const back = Q.start(Q.emptyQueue, [song(2, 2), song(1, 1)], 0, false)
    expect(G.runFrom(back)).toEqual([])
  })
  it('goes on to the next disc', () => {
    const q = Q.start(Q.emptyQueue, [song(1, 9, { disc: 1 }), song(2, 1, { disc: 2 })], 0, false)
    expect(G.runFrom(q)).toHaveLength(2)
  })
  it('leaves albums with gaps, shuffle, repeat one and a last song alone', () => {
    expect(G.runFrom(Q.start(Q.emptyQueue, album.map((s) => ({ ...s, gapless: false })), 0, false))).toEqual([])
    expect(G.runFrom(Q.start(Q.emptyQueue, album, 0, true))).toEqual([])
    expect(G.runFrom({ ...Q.start(Q.emptyQueue, album, 0, false), repeat: 'one' })).toEqual([])
    expect(G.runFrom(Q.start(Q.emptyQueue, album, 2, false))).toEqual([])
  })
})

describe('a run', () => {
  const q = Q.start(Q.emptyQueue, [song(1, 1), song(2, 2), song(3, 3)], 0, false)
  const entries = G.runFrom(q)
  // Started 20 seconds into the first song.
  const run = G.toRun(entries, { url: '', startSec: 20, tracks: [{ id: 1, startSec: -20 }, { id: 2, startSec: 40 }, { id: 3, startSec: 100 }] })
  it('knows each song’s place and length', () => {
    expect(run.keys).toEqual(entries.map((e) => e.key))
    expect(run.lengths).toEqual([60, 60, 60])
  })
  it('tells which song is playing', () => {
    expect(G.at(run, 0)).toBe(0)
    expect(G.at(run, 39.9)).toBe(0)
    expect(G.at(run, 40)).toBe(1)
    expect(G.at(run, 250)).toBe(2)
  })
})

describe('setGapless', () => {
  it('marks the album’s songs and keeps the entries', () => {
    const q = Q.start(Q.emptyQueue, [song(1, 1, { gapless: false }), song(9, 1, { parentId: 7, gapless: false })], 0, false)
    const on = G.setGapless(q, 1, true)
    expect(on.entries.map((e) => e.item.gapless)).toEqual([true, false])
    expect(on.entries.map((e) => e.key)).toEqual(q.entries.map((e) => e.key))
    expect(G.setGapless(q, 42, true)).toBe(q)
  })
})
