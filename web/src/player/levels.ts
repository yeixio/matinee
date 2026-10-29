// The music visualizer's numbers, kept apart from drawing so they can be
// tested. The server listens to each song once and sends how loud each part
// of its sound is, moment by moment (GET /items/{id}/levels), so every
// player draws the same thing: the phone and TV apps can't hear what they
// play. The apps have a copy (matinee-apps mobile/src/music/levels.ts).

/** A song's levels: `fps` moments a second, each `bands` values from the bass up, 0 to 1. */
export type Levels = { fps: number; bands: number; data: Uint8Array }

/** How the server sends them: the bytes in base64. */
export type LevelsJSON = { fps: number; bands: number; levels: string }

export function decodeLevels(j: LevelsJSON): Levels {
  return { fps: j.fps, bands: j.bands, data: Uint8Array.from(atob(j.levels), (c) => c.charCodeAt(0)) }
}

/**
 * The levels at `sec` into the song, into `out` (one per band, 0 to 1), in
 * between the moments either side so the drawing moves smoothly.
 */
export function levelsAt(l: Levels, sec: number, out: Float32Array) {
  const frames = Math.floor(l.data.length / l.bands)
  const f = Math.max(0, sec * l.fps)
  const i = Math.floor(f)
  const mix = f - i
  for (let b = 0; b < out.length; b++) {
    const band = Math.min(l.bands - 1, Math.floor((b * l.bands) / out.length))
    const a = i < frames ? l.data[i * l.bands + band] : 0
    const z = i + 1 < frames ? l.data[(i + 1) * l.bands + band] : a
    out[b] = (a + (z - a) * mix) / 255
  }
  return out
}

/**
 * Bars jump up with a beat and fall back slowly, as a meter's needle does.
 * `dt` is the seconds since the last frame.
 */
export function settle(shown: Float32Array, target: Float32Array, dt: number) {
  const fall = Math.min(1, dt * 3.5)
  for (let b = 0; b < shown.length; b++) {
    const t = target[b]
    shown[b] = t >= shown[b] ? shown[b] + (t - shown[b]) * Math.min(1, dt * 30) : shown[b] - (shown[b] - t) * fall
  }
  return shown
}

/**
 * While a song's levels are on their way (or a downloaded song plays with
 * no server to ask), a slow, quiet wave, so the visualizer never sits dead.
 */
export function idle(sec: number, out: Float32Array) {
  for (let b = 0; b < out.length; b++) {
    out[b] = 0.12 + 0.08 * Math.sin(sec * 1.6 + b * 0.55) + 0.05 * Math.sin(sec * 0.7 - b * 0.3)
  }
  return out
}

/** The bass: the first sixth of the bands, which the beat lives in. */
export function bass(levels: Float32Array) {
  const n = Math.max(1, Math.floor(levels.length / 6))
  let sum = 0
  for (let b = 0; b < n; b++) sum += levels[b]
  return sum / n
}

/** The visualizer's looks, in the order the remote or a tap steps through them. */
export const styles = ['bars', 'halo', 'pulse'] as const
export type Style = (typeof styles)[number]

export const nextStyle = (s: Style, step = 1): Style => styles[(styles.indexOf(s) + step + styles.length) % styles.length]
export const parseStyle = (v: string | null | undefined): Style => (styles.includes(v as Style) ? (v as Style) : 'bars')
