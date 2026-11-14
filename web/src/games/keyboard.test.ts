import { describe, expect, it } from 'vitest'
import { buttonMessage, keyMessage, linuxKeys, moveMessage, pictureSpot, pointerMessage, wheelMessage } from './keyboard'

describe('keyboard and mouse for PC games', () => {
  it('sends Linux key codes', () => {
    expect(linuxKeys.KeyA).toBe(30)
    expect(linuxKeys.Enter).toBe(28)
    expect([...keyMessage(linuxKeys.ArrowUp, true)]).toEqual([4, 103, 0, 1])
  })

  it('finds the spot on a letterboxed picture', () => {
    // A 16:9 picture in a square box has bars above and below.
    const box = { width: 1600, height: 1600 }
    const pic = { width: 1920, height: 1080 }
    expect(pictureSpot(0, 800, box, pic)).toEqual([0, 32768])
    expect(pictureSpot(1600, 800, box, pic)).toEqual([65535, 32768])
    expect(pictureSpot(800, 10, box, pic)).toBeUndefined()
    expect([...pointerMessage([65535, 256])]).toEqual([5, 255, 255, 0, 1])
  })

  it('sends moves, buttons and the wheel', () => {
    expect([...moveMessage(-5, 3)]).toEqual([6, 0xfb, 0xff, 3, 0])
    expect([...buttonMessage(2, true)!]).toEqual([7, 2, 1])
    expect(buttonMessage(7, true)).toBeUndefined()
    expect([...wheelMessage(-1, 0)]).toEqual([8, 0xff, 0])
  })
})
