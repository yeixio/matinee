import { afterEach, describe, expect, it, vi } from 'vitest'
import { waitForNewRun } from './RestartServer'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('waitForNewRun', () => {
  it('waits through the old run and the gap until a new run answers', async () => {
    vi.useFakeTimers()
    const answer = (h: object) => () => Promise.resolve(new Response(JSON.stringify(h)))
    const answers = [
      answer({ version: '0.3.0', bootId: 'a', state: 'restarting' }),
      () => Promise.reject(new TypeError('Failed to fetch')),
      answer({ version: '0.3.0', bootId: 'b', state: 'ok' }),
    ]
    const fetch = vi.fn(() => answers.shift()!())
    vi.stubGlobal('fetch', fetch)
    const slow = vi.fn()
    let done = false
    const back = waitForNewRun('a', new AbortController().signal, slow).then(() => (done = true))
    await vi.advanceTimersByTimeAsync(10_000)
    await back
    expect(done).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(3)
    expect(slow).not.toHaveBeenCalled()
  })

  it('says when the server is slow to come back, and keeps waiting', async () => {
    vi.useFakeTimers()
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(() => (++calls < 40 ? Promise.reject(new TypeError('Failed to fetch')) : Promise.resolve(new Response(JSON.stringify({ version: '0.3.0', bootId: 'b' }))))),
    )
    const slow = vi.fn()
    const back = waitForNewRun('a', new AbortController().signal, slow)
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    await back
    expect(slow).toHaveBeenCalled()
  })
})
