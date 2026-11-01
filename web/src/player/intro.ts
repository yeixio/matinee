// Skip intro rules, shared in spirit with the phone and TV apps (see
// docs/intros.md): while an episode's intro plays, a Skip intro button
// jumps to its end. People who turned on Skip intros in Settings skip it
// without pressing anything, once per episode, so seeking back to watch
// it works.

export type Intro = { startMs: number; endMs: number }

// The button goes away this close to the intro's end, when skipping
// would barely skip anything.
export const HIDE_BEFORE_END_SECONDS = 2

// inIntro says whether the Skip intro button shows at this point in the
// episode, in seconds.
export function inIntro(seconds: number, intro?: Intro): boolean {
  if (!intro || intro.endMs <= intro.startMs) return false
  return seconds >= intro.startMs / 1000 - 0.25 && seconds < intro.endMs / 1000 - HIDE_BEFORE_END_SECONDS
}

// introEnd is where skipping lands, in seconds.
export function introEnd(intro: Intro): number {
  return intro.endMs / 1000
}
