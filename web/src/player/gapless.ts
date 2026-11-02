// Albums meant to play without gaps (a DJ mix, a live album) play as one
// stream the server joins song to song (POST /gapless): the songs coming up
// in the queue from the album are a run, and the player shows the song
// whose start the stream has passed. Plain data and functions, so the
// player and its tests share them.
import type { GaplessStream, Item } from '../api'
import type { Entry, Queue } from './queue'

// The most songs one stream joins.
export const MAX_RUN = 100

// A stream playing: its queue entries, and where each starts on its clock.
export type Run = { keys: string[]; starts: number[]; lengths: number[] }

const place = (it: Item) => [it.disc ?? 0, it.index ?? 0]

// follows says whether b comes straight after a on the same album.
function follows(a: Item, b: Item): boolean {
  if (b.kind !== 'track' || !b.gapless || b.parentId === undefined || b.parentId !== a.parentId) return false
  const [ad, ai] = place(a)
  const [bd, bi] = place(b)
  return bd > ad || (bd === ad && bi > ai)
}

// runFrom is what plays as one stream from the queue's place: the song
// playing and those after it from the same album without gaps, in the
// album's order. Fewer than two need no stream; shuffle and repeat one
// play songs one at a time.
export function runFrom(q: Queue): Entry[] {
  const first = q.entries[q.pos]
  if (!first || q.shuffle || q.repeat === 'one' || first.item.kind !== 'track' || !first.item.gapless) return []
  const out = [first]
  for (let i = q.pos + 1; i < q.entries.length && out.length < MAX_RUN; i++) {
    if (!follows(out[out.length - 1].item, q.entries[i].item)) break
    out.push(q.entries[i])
  }
  return out.length > 1 ? out : []
}

// toRun lines up the server's answer with the queue entries it plays.
export function toRun(entries: Entry[], stream: GaplessStream): Run {
  const starts = entries.map((_, i) => stream.tracks[i]?.startSec ?? 0)
  // Each song's whole length; the first may start partway in.
  const lengths = entries.map((e, i) => (i + 1 < entries.length ? starts[i + 1] - starts[i] : (e.item.durationMs ?? 0) / 1000))
  return { keys: entries.map((e) => e.key), starts, lengths }
}

// at is which of the run's songs plays at a time on the stream's clock.
export function at(run: Run, time: number): number {
  let i = 0
  // A moment early: timeupdate comes a few times a second.
  while (i + 1 < run.starts.length && time >= run.starts[i + 1] - 0.05) i++
  return i
}

// setGapless marks an album's songs in the queue as playing without gaps,
// or not, keeping each entry (and so what's playing).
export function setGapless(q: Queue, albumId: number, on: boolean): Queue {
  const mark = (e: Entry): Entry => (e.item.parentId === albumId && e.item.kind === 'track' ? { ...e, item: { ...e.item, gapless: on } } : e)
  if (!q.entries.some((e) => e.item.parentId === albumId)) return q
  return { ...q, entries: q.entries.map(mark), original: q.original.map(mark) }
}
