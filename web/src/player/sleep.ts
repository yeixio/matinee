// Sleep timer rules, shared in spirit with the phone and TV apps (see
// docs/sleep-timer.md): pause after a while, or stop when this episode or
// movie ends. The sound fades out over the last 30 seconds. The timer keeps
// going into the next episode, and goes away when someone leaves the player.

import { useEffect, useState, useSyncExternalStore } from 'react'

// What the menu offers, in minutes, besides the end of what's playing.
export const SLEEP_MINUTES = [15, 30, 45, 60, 90]
// The sound fades out over this long before the timer pauses.
export const FADE_SECONDS = 30

// A time to pause at (milliseconds since 1970) and the minutes picked, or
// the end of what's playing.
export type Sleep = { at: number; minutes: number } | 'end' | undefined

let current: Sleep
// Set when auto-play moves on, so the next episode keeps the timer.
let carry = false
const listeners = new Set<() => void>()

export const sleepTimer = {
  get: () => current,
  set(s: Sleep) {
    current = s
    listeners.forEach((l) => l())
  },
  // Pause this many minutes from now.
  after(minutes: number, now = Date.now()) {
    sleepTimer.set({ at: now + minutes * 60_000, minutes })
  },
  subscribe(l: () => void) {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
  // Auto-play is starting the next episode.
  carryOver() {
    carry = true
  },
  // The player is closing: the timer goes too, unless the next episode is
  // taking over.
  leave() {
    if (!carry) sleepTimer.set(undefined)
    carry = false
  },
}

// Seconds left on a timed sleep, never below 0; undefined otherwise.
export function secondsLeft(s: Sleep, now = Date.now()): number | undefined {
  if (!s || s === 'end') return undefined
  return Math.max(0, Math.ceil((s.at - now) / 1000))
}

// How loud to play, 0 to 1, with so many seconds left.
export function sleepVolume(left: number | undefined): number {
  if (left === undefined) return 1
  return Math.max(0, Math.min(1, left / FADE_SECONDS))
}

// The sleep timer and, for a timed one, the seconds left, ticking.
export function useSleep(): { sleep: Sleep; left?: number } {
  const sleep = useSyncExternalStore(sleepTimer.subscribe, sleepTimer.get)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!sleep || sleep === 'end') return
    setNow(Date.now())
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [sleep])
  return { sleep, left: secondsLeft(sleep, now) }
}
