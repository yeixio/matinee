import { afterEach, describe, expect, it, vi } from 'vitest'
import { backoff, heardRestarting, onRestarting, outcome, restarting, waitForServer } from './reconnect'

describe('outcome', () => {
  const before = { version: '0.1.3', bootId: 'a' }
  it('tells an update from a restart from a dropped connection', () => {
    expect(outcome(before, { version: '0.1.4', bootId: 'b' })).toBe('updated')
    expect(outcome(before, { version: '0.1.3', bootId: 'b' })).toBe('restarted')
    expect(outcome(before, { version: '0.1.3', bootId: 'a' })).toBe('same')
    // An older server says no bootId, and nothing is known before the first answer.
    expect(outcome({ version: '0.1.3' }, { version: '0.1.3', bootId: 'b' })).toBe('same')
    expect(outcome(undefined, { version: '0.1.4' })).toBe('same')
  })
})

describe('waiting', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('backs off to every 5 seconds', () => {
    expect([0, 1, 2, 3, 9].map(backoff)).toEqual([1000, 2000, 3000, 5000, 5000])
  })

  it('remembers hearing the server is restarting, for a while', () => {
    const heard = vi.fn()
    const stop = onRestarting(heard)
    heardRestarting()
    heardRestarting()
    stop()
    expect(heard).toHaveBeenCalledTimes(1)
    expect(restarting()).toBe(true)
    expect(restarting(Date.now() + 10 * 60_000)).toBe(false)
  })

  it('waits until the server is back and done restarting', async () => {
    vi.useFakeTimers()
    const answers = [
      () => Promise.reject(new TypeError('Failed to fetch')),
      () => Promise.resolve(new Response(JSON.stringify({ version: '0.1.3', state: 'restarting' }))),
      () => Promise.resolve(new Response(JSON.stringify({ version: '0.1.4', bootId: 'b', state: 'ok' }))),
    ]
    vi.stubGlobal('fetch', vi.fn(() => answers.shift()!()))
    const why: string[] = []
    const back = waitForServer((w) => why.push(w), new AbortController().signal)
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(back).resolves.toMatchObject({ version: '0.1.4', bootId: 'b' })
    expect(why).toEqual(['restarting', 'restarting'])
  })
})
