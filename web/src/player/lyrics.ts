// A song's words, and which line is being sung, kept apart from drawing so
// they can be tested. The server finds them (GET /items/{id}/lyrics): from
// a .lrc file next to the song, its tags, or, if an admin allows it,
// LRCLIB. The apps have a copy (matinee-apps mobile/src/music/lyrics.ts).

/** One line. `ms` is when it's sung; untimed words have none. */
export type LyricLine = { ms?: number; text: string }

export type Lyrics = {
  synced: boolean
  lines: LyricLine[]
  // file, embedded or online; none when there are no words.
  source?: 'file' | 'embedded' | 'online'
  instrumental?: boolean
}

/**
 * The line being sung `ms` into the song: the last one that has started, or
 * -1 before the first. Lines are in order, so it's a binary search.
 */
export function lineAt(lines: LyricLine[], ms: number): number {
  let lo = 0
  let hi = lines.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if ((lines[mid].ms ?? 0) <= ms) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found
}

/**
 * How far through its time the line is, 0 to 1: until the next line
 * starts, or for the last line, a few seconds. Players use it to fill a
 * pause's dots.
 */
export function lineProgress(lines: LyricLine[], i: number, ms: number): number {
  if (i < 0 || i >= lines.length) return 0
  const start = lines[i].ms ?? 0
  const end = lines[i + 1]?.ms ?? start + 5000
  if (end <= start) return 1
  return Math.min(1, Math.max(0, (ms - start) / (end - start)))
}
