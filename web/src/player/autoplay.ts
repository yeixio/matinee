// Auto-play rules, shared in spirit with the phone and TV apps (see
// docs/autoplay.md): when an episode's credits start, or near its end, an
// "Up next" card counts down and plays the next episode.

// How long the card counts down before the next episode starts.
export const COUNTDOWN_SECONDS = 10
// Without a credits chapter, the card shows this long before the end.
export const BEFORE_END_SECONDS = 30
// After this many episodes in a row with nobody touching anything, ask
// "Still watching?" instead of playing on.
export const STILL_WATCHING_AFTER = 3

// upNextAt says when, in seconds into the episode, the card shows: where
// the credits start, else 30 seconds before the end. Very short videos
// only get it at the end.
export function upNextAt(durationSeconds: number, creditsMs?: number): number | undefined {
  if (!durationSeconds) return undefined
  const credits = creditsMs ? creditsMs / 1000 : 0
  if (credits > 0 && credits < durationSeconds) return credits
  if (durationSeconds < 4 * BEFORE_END_SECONDS) return durationSeconds
  return durationSeconds - BEFORE_END_SECONDS
}

// Episodes played on by themselves since someone last pressed or tapped
// something. It lives as long as the page, across episodes.
let unattended = 0

export const stillWatching = {
  // Someone pressed a key, clicked or tapped.
  input() {
    unattended = 0
  },
  // The next episode started without anyone choosing it.
  autoPlayed() {
    unattended++
  },
  // Whether to ask before playing on.
  shouldAsk: () => unattended >= STILL_WATCHING_AFTER,
}
