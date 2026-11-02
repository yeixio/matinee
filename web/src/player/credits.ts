// Skip credits rules, shared in spirit with the phone and TV apps (see
// docs/credits.md): where an episode's or movie's closing credits start,
// and the Skip credits button for when no Up next card shows there.

import type { ItemDetail } from '../api'

export type Credits = { startMs: number; endMs?: number }

// The button goes away this close to the credits' end.
export const HIDE_BEFORE_END_SECONDS = 2
// Credits that stop closer than this to the end of the video run to it.
const END_SLACK_SECONDS = 5

// creditsOf says where an item's closing credits are: what the server
// found, or, from servers before that, where a chapter says they start.
export function creditsOf(item?: ItemDetail): Credits | undefined {
  if (item?.endCredits && item.endCredits.startMs > 0) return item.endCredits
  const ms = item?.mediaInfo?.creditsMs
  return ms ? { startMs: ms } : undefined
}

// runsToEnd says whether nothing plays after the credits.
export function runsToEnd(credits: Credits, durationSeconds: number): boolean {
  return !credits.endMs || credits.endMs / 1000 >= durationSeconds - END_SLACK_SECONDS
}

// creditsEnd is where skipping the credits lands, in seconds: what plays
// after them, or the end of the video.
export function creditsEnd(credits: Credits, durationSeconds: number): number {
  return runsToEnd(credits, durationSeconds) ? durationSeconds : credits.endMs! / 1000
}

// inCredits says whether the credits are playing at this point, in seconds,
// early enough that skipping them skips something.
export function inCredits(seconds: number, durationSeconds: number, credits?: Credits): boolean {
  if (!credits || !durationSeconds || credits.startMs <= 0 || credits.startMs / 1000 >= durationSeconds) return false
  return seconds >= credits.startMs / 1000 - 0.25 && seconds < creditsEnd(credits, durationSeconds) - HIDE_BEFORE_END_SECONDS
}
