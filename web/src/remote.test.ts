import { afterEach, describe, expect, it, vi } from 'vitest'

describe('remote control turn', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  // WebKit freezes the page that replaces this one (a reload) while this
  // one still holds the lock, so leaving hands it back at once, even with
  // a request to the server still waiting.
  it('lets go of the lock when the page goes away', async () => {
    let held: Promise<unknown> | undefined
    vi.stubGlobal('navigator', {
      ...navigator,
      locks: {
        request: (_name: string, _opts: object, cb: (lock: object) => Promise<unknown>) => {
          held = cb({})
          return held
        },
      },
    })
    // The server's long poll never answers.
    vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => new Promise((_, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    })))
    const { remote } = await import('./remote')
    remote.start(() => {})
    expect(held).toBeDefined()
    let released = false
    held!.then(() => (released = true))
    await Promise.resolve()
    expect(released).toBe(false)
    window.dispatchEvent(new Event('pagehide'))
    await new Promise((r) => setTimeout(r, 0))
    expect(released).toBe(true)
  })
})
