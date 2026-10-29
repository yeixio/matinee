// The music queue: what plays, in what order, with shuffle and repeat.
// Plain data and functions, so the player and its tests share them.
import type { Item } from '../api'

export type Repeat = 'off' | 'all' | 'one'

// One place in the queue. The same track can be queued twice, so each
// entry has its own key.
export type Entry = { key: string; item: Item }

export type Queue = {
  // In playing order.
  entries: Entry[]
  // The order before shuffling, to go back to when shuffle is turned off.
  original: Entry[]
  pos: number
  shuffle: boolean
  repeat: Repeat
}

export const emptyQueue: Queue = { entries: [], original: [], pos: 0, shuffle: false, repeat: 'off' }

let counter = 0
const entry = (item: Item): Entry => ({ key: `${item.id}-${++counter}`, item })

export function current(q: Queue): Item | undefined {
  return q.entries[q.pos]?.item
}

// Mixes up everything but the first entry, which keeps playing.
function shuffled(entries: Entry[], random: () => number): Entry[] {
  const out = entries.slice(1)
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return entries.length ? [entries[0], ...out] : []
}

// start plays items from start; with shuffle on, start plays first and the
// rest are mixed. shuffle undefined keeps the queue's setting.
export function start(q: Queue, items: Item[], at = 0, shuffle = q.shuffle, random = Math.random): Queue {
  const original = items.map(entry)
  if (!shuffle) return { ...q, entries: original, original, pos: Math.min(at, Math.max(original.length - 1, 0)), shuffle }
  // Shuffling a whole album from its Shuffle button starts anywhere.
  const first = at < 0 ? Math.floor(random() * original.length) : at
  const rest = original.filter((_, i) => i !== first)
  return { ...q, entries: shuffled([original[first], ...rest], random), original, pos: 0, shuffle }
}

export function setShuffle(q: Queue, on: boolean, random = Math.random): Queue {
  if (on === q.shuffle) return q
  const cur = q.entries[q.pos]
  if (!cur) return { ...q, shuffle: on }
  if (on) {
    // What's already played stays behind; what's left is mixed.
    const left = q.entries.slice(q.pos)
    return { ...q, entries: [...q.entries.slice(0, q.pos), ...shuffled(left, random)], shuffle: true }
  }
  const pos = q.original.findIndex((e) => e.key === cur.key)
  return { ...q, entries: q.original, pos: Math.max(pos, 0), shuffle: false }
}

export function cycleRepeat(q: Queue): Queue {
  return { ...q, repeat: q.repeat === 'off' ? 'all' : q.repeat === 'all' ? 'one' : 'off' }
}

// Play next: right after what's playing, in both orders.
export function playNext(q: Queue, items: Item[]): Queue {
  if (!q.entries.length) return start(q, items)
  const add = items.map(entry)
  const cur = q.entries[q.pos]
  const at = q.original.findIndex((e) => e.key === cur.key) + 1
  return {
    ...q,
    entries: [...q.entries.slice(0, q.pos + 1), ...add, ...q.entries.slice(q.pos + 1)],
    original: [...q.original.slice(0, at), ...add, ...q.original.slice(at)],
  }
}

export function addToQueue(q: Queue, items: Item[]): Queue {
  if (!q.entries.length) return start(q, items)
  const add = items.map(entry)
  return { ...q, entries: [...q.entries, ...add], original: [...q.original, ...add] }
}

export function remove(q: Queue, key: string): Queue {
  const i = q.entries.findIndex((e) => e.key === key)
  if (i < 0 || i === q.pos) return q
  return {
    ...q,
    entries: q.entries.filter((e) => e.key !== key),
    original: q.original.filter((e) => e.key !== key),
    pos: i < q.pos ? q.pos - 1 : q.pos,
  }
}

// Clears what's coming up, leaving what's playing.
export function clearUpNext(q: Queue): Queue {
  const keep = new Set(q.entries.slice(0, q.pos + 1).map((e) => e.key))
  return { ...q, entries: q.entries.slice(0, q.pos + 1), original: q.original.filter((e) => keep.has(e.key)) }
}

// Moves an upcoming entry to another place in the playing order.
export function move(q: Queue, from: number, to: number): Queue {
  if (from === q.pos || to <= q.pos || from === to || from < 0 || from >= q.entries.length) return q
  const entries = [...q.entries]
  const [e] = entries.splice(from, 1)
  entries.splice(Math.min(to, entries.length), 0, e)
  return { ...q, entries, original: q.shuffle ? q.original : entries }
}

export function jump(q: Queue, pos: number): Queue {
  return pos >= 0 && pos < q.entries.length ? { ...q, pos } : q
}

// next moves on: auto is the end of a track, where repeat one plays it
// again. Returns undefined at the end of the queue.
export function next(q: Queue, auto: boolean): Queue | undefined {
  if (auto && q.repeat === 'one') return q
  if (q.pos + 1 < q.entries.length) return { ...q, pos: q.pos + 1 }
  if (q.repeat !== 'off' && q.entries.length) return { ...q, pos: 0 }
  return undefined
}

// previous goes back a track, or around to the end when repeating.
export function previous(q: Queue): Queue {
  if (q.pos > 0) return { ...q, pos: q.pos - 1 }
  if (q.repeat !== 'off' && q.entries.length) return { ...q, pos: q.entries.length - 1 }
  return q
}

export function upNext(q: Queue): Entry[] {
  return q.entries.slice(q.pos + 1)
}

// Who and what: "Artist · Album" for a track.
export function byline(item: Item): string {
  return [item.artist, item.album].filter(Boolean).join(' · ')
}
