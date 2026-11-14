// The player's keyboard and mouse for a PC game on the game host: keys go
// as Linux key codes and the pointer as a spot on the picture, or as moves
// once the mouse is captured (see internal/gamehost/kbm.go on the server).

/** Linux key codes (linux/input-event-codes.h) by KeyboardEvent.code. */
export const linuxKeys: Record<string, number> = {
  Escape: 1, Digit1: 2, Digit2: 3, Digit3: 4, Digit4: 5, Digit5: 6, Digit6: 7, Digit7: 8, Digit8: 9, Digit9: 10, Digit0: 11,
  Minus: 12, Equal: 13, Backspace: 14, Tab: 15,
  KeyQ: 16, KeyW: 17, KeyE: 18, KeyR: 19, KeyT: 20, KeyY: 21, KeyU: 22, KeyI: 23, KeyO: 24, KeyP: 25,
  BracketLeft: 26, BracketRight: 27, Enter: 28, ControlLeft: 29,
  KeyA: 30, KeyS: 31, KeyD: 32, KeyF: 33, KeyG: 34, KeyH: 35, KeyJ: 36, KeyK: 37, KeyL: 38,
  Semicolon: 39, Quote: 40, Backquote: 41, ShiftLeft: 42, Backslash: 43,
  KeyZ: 44, KeyX: 45, KeyC: 46, KeyV: 47, KeyB: 48, KeyN: 49, KeyM: 50,
  Comma: 51, Period: 52, Slash: 53, ShiftRight: 54, NumpadMultiply: 55, AltLeft: 56, Space: 57, CapsLock: 58,
  F1: 59, F2: 60, F3: 61, F4: 62, F5: 63, F6: 64, F7: 65, F8: 66, F9: 67, F10: 68,
  NumLock: 69, ScrollLock: 70, Numpad7: 71, Numpad8: 72, Numpad9: 73, NumpadSubtract: 74,
  Numpad4: 75, Numpad5: 76, Numpad6: 77, NumpadAdd: 78, Numpad1: 79, Numpad2: 80, Numpad3: 81, Numpad0: 82, NumpadDecimal: 83,
  IntlBackslash: 86, F11: 87, F12: 88, NumpadEnter: 96, ControlRight: 97, NumpadDivide: 98, PrintScreen: 99, AltRight: 100,
  Home: 102, ArrowUp: 103, PageUp: 104, ArrowLeft: 105, ArrowRight: 106, End: 107, ArrowDown: 108, PageDown: 109,
  Insert: 110, Delete: 111, Pause: 119, MetaLeft: 125, MetaRight: 126, ContextMenu: 127,
}

/** A key going down or up: [4, code lo, code hi, down]. */
export function keyMessage(code: number, down: boolean): Uint8Array {
  return new Uint8Array([4, code & 0xff, code >> 8, down ? 1 : 0])
}

/**
 * Where a point on the video element falls on the picture, from 0 to 65535
 * each way, with the picture fitted inside the element (object-contain).
 * Undefined off the picture.
 */
export function pictureSpot(
  x: number,
  y: number,
  box: { width: number; height: number },
  picture: { width: number; height: number },
): [number, number] | undefined {
  if (!picture.width || !picture.height || !box.width || !box.height) return undefined
  const scale = Math.min(box.width / picture.width, box.height / picture.height)
  const w = picture.width * scale
  const h = picture.height * scale
  const px = (x - (box.width - w) / 2) / w
  const py = (y - (box.height - h) / 2) / h
  if (px < 0 || px > 1 || py < 0 || py > 1) return undefined
  return [Math.round(px * 65535), Math.round(py * 65535)]
}

/** The pointer to a spot on the picture: [5, x lo, x hi, y lo, y hi]. */
export function pointerMessage([x, y]: [number, number]): Uint8Array {
  return new Uint8Array([5, x & 0xff, x >> 8, y & 0xff, y >> 8])
}

/** The pointer moved, with the mouse captured: [6, dx lo, dx hi, dy lo, dy hi]. */
export function moveMessage(dx: number, dy: number): Uint8Array {
  const c = (v: number) => Math.max(-32768, Math.min(32767, Math.round(v))) & 0xffff
  const x = c(dx)
  const y = c(dy)
  return new Uint8Array([6, x & 0xff, x >> 8, y & 0xff, y >> 8])
}

/** A mouse button, by the browser's number (0 left, 1 middle, 2 right, 3 back, 4 forward). */
export function buttonMessage(button: number, down: boolean): Uint8Array | undefined {
  if (button < 0 || button > 4) return undefined
  return new Uint8Array([7, button, down ? 1 : 0])
}

/** The wheel turned by notches: up and right are positive. */
export function wheelMessage(up: number, right: number): Uint8Array {
  return new Uint8Array([8, up & 0xff, right & 0xff])
}

/**
 * Sends the keyboard and mouse to a PC game on its own channel, ordered
 * and sure to arrive, so a key let go is never lost. Returns what stops it,
 * letting go of everything held.
 */
export function sendKeyboardAndMouse(pc: RTCPeerConnection, video: HTMLVideoElement): () => void {
  const ch = pc.createDataChannel('keys')
  const send = (b: Uint8Array | undefined) => {
    if (b && ch.readyState === 'open') ch.send(b)
  }
  const keys = new Set<number>()
  const buttons = new Set<number>()
  const letGo = () => {
    for (const k of keys) send(keyMessage(k, false))
    for (const b of buttons) send(buttonMessage(b, false))
    keys.clear()
    buttons.clear()
  }
  const onKey = (e: KeyboardEvent) => {
    const code = linuxKeys[e.code]
    if (code === undefined) return
    e.preventDefault()
    const down = e.type === 'keydown'
    if (down === keys.has(code)) return
    if (down) keys.add(code)
    else keys.delete(code)
    send(keyMessage(code, down))
  }
  const captured = () => document.pointerLockElement === video
  const onMove = (e: PointerEvent) => {
    if (captured()) {
      if (e.movementX || e.movementY) send(moveMessage(e.movementX, e.movementY))
      return
    }
    const r = video.getBoundingClientRect()
    const spot = pictureSpot(e.clientX - r.left, e.clientY - r.top, r, { width: video.videoWidth, height: video.videoHeight })
    if (spot) send(pointerMessage(spot))
  }
  const onButton = (e: PointerEvent) => {
    if (e.pointerType !== 'mouse' && e.pointerType !== 'pen' && e.pointerType !== 'touch') return
    e.preventDefault()
    const down = e.type === 'pointerdown'
    if (down) {
      onMove(e)
      // Letting go off the picture still counts.
      video.setPointerCapture?.(e.pointerId)
      buttons.add(e.button)
    } else buttons.delete(e.button)
    send(buttonMessage(e.button, down))
  }
  // The wheel in notches: a trackpad's many small scrolls add up to one.
  let wheelY = 0
  let wheelX = 0
  const onWheel = (e: WheelEvent) => {
    e.preventDefault()
    const per = e.deltaMode === 1 ? 1 : e.deltaMode === 2 ? 0.1 : 50
    wheelY += e.deltaY / per
    wheelX += e.deltaX / per
    const down = Math.trunc(wheelY)
    const right = Math.trunc(wheelX)
    if (down || right) {
      wheelY -= down
      wheelX -= right
      const up = -down
      send(wheelMessage(Math.max(-127, Math.min(127, up)), Math.max(-127, Math.min(127, right))))
    }
  }
  const noMenu = (e: Event) => e.preventDefault()
  addEventListener('keydown', onKey)
  addEventListener('keyup', onKey)
  addEventListener('blur', letGo)
  video.addEventListener('pointermove', onMove)
  video.addEventListener('pointerdown', onButton)
  video.addEventListener('pointerup', onButton)
  video.addEventListener('wheel', onWheel, { passive: false })
  video.addEventListener('contextmenu', noMenu)
  return () => {
    letGo()
    removeEventListener('keydown', onKey)
    removeEventListener('keyup', onKey)
    removeEventListener('blur', letGo)
    video.removeEventListener('pointermove', onMove)
    video.removeEventListener('pointerdown', onButton)
    video.removeEventListener('pointerup', onButton)
    video.removeEventListener('wheel', onWheel)
    video.removeEventListener('contextmenu', noMenu)
    if (captured()) document.exitPointerLock()
  }
}
