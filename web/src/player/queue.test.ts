import { describe, expect, it } from 'vitest'
import type { Item } from '../api'
import * as Q from './queue'

const song = (id: number): Item => ({ id, libraryId: 1, kind: 'track', title: `Song ${id}`, addedAt: 0, playable: true, hasPoster: false, hasBackdrop: false })
const songs = (...ids: number[]) => ids.map(song)
const ids = (q: Q.Queue) => q.entries.map((e) => e.item.id)
// Always picks the last place, so shuffles are predictable.
const last = () => 0.999

describe('queue', () => {
  it('plays from where you click', () => {
    const q = Q.start(Q.emptyQueue, songs(1, 2, 3), 1)
    expect(Q.current(q)?.id).toBe(2)
    expect(Q.upNext(q).map((e) => e.item.id)).toEqual([3])
  })

  it('moves on, and stops at the end unless repeating', () => {
    let q = Q.start(Q.emptyQueue, songs(1, 2), 1)
    expect(Q.next(q, true)).toBeUndefined()
    q = Q.cycleRepeat(q) // all
    expect(Q.current(Q.next(q, true)!)?.id).toBe(1)
    q = Q.cycleRepeat(q) // one
    expect(Q.next(q, true)).toBe(q)
    // Skipping by hand still moves on.
    expect(Q.current(Q.next(q, false)!)?.id).toBe(1)
    expect(Q.cycleRepeat(q).repeat).toBe('off')
  })

  it('goes back, and around when repeating', () => {
    let q = Q.start(Q.emptyQueue, songs(1, 2, 3))
    expect(Q.previous(q)).toBe(q)
    q = Q.cycleRepeat(q)
    expect(Q.current(Q.previous(q))?.id).toBe(3)
  })

  it('shuffles what is left and goes back to the album order', () => {
    let q = Q.start(Q.emptyQueue, songs(1, 2, 3, 4), 1)
    q = Q.setShuffle(q, true, last)
    expect(Q.current(q)?.id).toBe(2)
    expect(ids(q).slice(0, 2)).toEqual([1, 2])
    expect(new Set(ids(q))).toEqual(new Set([1, 2, 3, 4]))
    q = Q.next(q, false)!
    const playing = Q.current(q)!.id
    q = Q.setShuffle(q, false)
    expect(ids(q)).toEqual([1, 2, 3, 4])
    expect(Q.current(q)?.id).toBe(playing)
  })

  it('starts a shuffle anywhere', () => {
    const q = Q.start(Q.emptyQueue, songs(1, 2, 3), -1, true, last)
    expect(Q.current(q)?.id).toBe(3)
    expect(q.pos).toBe(0)
    expect(q.shuffle).toBe(true)
  })

  it('plays next and adds to the end', () => {
    let q = Q.start(Q.emptyQueue, songs(1, 2, 3))
    q = Q.addToQueue(q, songs(9))
    q = Q.playNext(q, songs(7, 8))
    expect(ids(q)).toEqual([1, 7, 8, 2, 3, 9])
    // Unshuffling keeps them where they were put.
    q = Q.setShuffle(Q.setShuffle(q, true), false)
    expect(ids(q)).toEqual([1, 7, 8, 2, 3, 9])
  })

  it('queues into an empty queue by playing', () => {
    expect(Q.current(Q.addToQueue(Q.emptyQueue, songs(5)))?.id).toBe(5)
  })

  it('removes, moves and clears upcoming songs, never the one playing', () => {
    let q = Q.start(Q.emptyQueue, songs(1, 2, 3, 4), 1)
    expect(Q.remove(q, q.entries[1].key)).toBe(q)
    q = Q.remove(q, q.entries[0].key)
    expect(ids(q)).toEqual([2, 3, 4])
    expect(Q.current(q)?.id).toBe(2)
    q = Q.move(q, 2, 1)
    expect(ids(q)).toEqual([2, 4, 3])
    expect(Q.move(q, 0, 2)).toBe(q)
    q = Q.clearUpNext(q)
    expect(ids(q)).toEqual([2])
  })

  it('keeps the same song queued twice apart', () => {
    const q = Q.start(Q.emptyQueue, songs(1, 1))
    expect(q.entries[0].key).not.toBe(q.entries[1].key)
  })

  it('names the artist and album', () => {
    expect(Q.byline({ ...song(1), artist: 'A', album: 'B' })).toBe('A · B')
  })
})
