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

// Plain http on the home network has no crypto.randomUUID (#439).
describe('client id over plain http', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  it('makes a UUID without crypto.randomUUID', async () => {
    const real = globalThis.crypto
    vi.stubGlobal('crypto', {
      getRandomValues: (a: Uint8Array) => real.getRandomValues(a),
    })
    const { newId, clientId } = await import('./remote')
    const id = newId()
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(newId()).not.toBe(id)
    expect(clientId()).toMatch(/^[0-9a-f-]{36}$/)
  })
})
