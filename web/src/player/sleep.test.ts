import { describe, expect, it } from 'vitest'
import { FADE_SECONDS, secondsLeft, sleepTimer, sleepVolume } from './sleep'

describe('sleep timer', () => {
  it('counts down from when it was set', () => {
    sleepTimer.after(15, 1_000_000)
    expect(secondsLeft(sleepTimer.get(), 1_000_000)).toBe(900)
    expect(secondsLeft(sleepTimer.get(), 1_000_000 + 899_500)).toBe(1)
    expect(secondsLeft(sleepTimer.get(), 1_000_000 + 999_000)).toBe(0)
    expect(secondsLeft('end')).toBeUndefined()
    expect(secondsLeft(undefined)).toBeUndefined()
  })
  it('fades the sound out over the last seconds', () => {
    expect(sleepVolume(undefined)).toBe(1)
    expect(sleepVolume(600)).toBe(1)
    expect(sleepVolume(FADE_SECONDS / 2)).toBe(0.5)
    expect(sleepVolume(0)).toBe(0)
  })
  it('goes with the player, unless auto-play moves on', () => {
    sleepTimer.set('end')
    sleepTimer.carryOver()
    sleepTimer.leave()
    expect(sleepTimer.get()).toBe('end')
    sleepTimer.leave()
    expect(sleepTimer.get()).toBeUndefined()
  })
})
