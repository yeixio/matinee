// Playing a game the server runs: its pictures and sound come over WebRTC,
// and the buttons held go back over a data channel (see
// internal/gamestream on the server).

import { api } from '../api'

/** libretro's joypad buttons, as bits of the mask the server takes. */
export const Btn = { B: 0, Y: 1, Select: 2, Start: 3, Up: 4, Down: 5, Left: 6, Right: 7, A: 8, X: 9, L: 10, R: 11, L2: 12, R2: 13, L3: 14, R3: 15 } as const

/**
 * A standard-layout gamepad's buttons (the Gamepad API's order) to
 * libretro's, by where they sit: the bottom face button is libretro's B,
 * the right one A, as on the consoles the emulators play.
 */
const padButtons = [Btn.B, Btn.A, Btn.Y, Btn.X, Btn.L, Btn.R, Btn.L2, Btn.R2, Btn.Select, Btn.Start, Btn.L3, Btn.R3, Btn.Up, Btn.Down, Btn.Left, Btn.Right]

/** Keys for playing without a controller. */
export const keyButtons: Record<string, number> = {
  ArrowUp: Btn.Up, ArrowDown: Btn.Down, ArrowLeft: Btn.Left, ArrowRight: Btn.Right,
  KeyZ: Btn.B, KeyX: Btn.A, KeyA: Btn.Y, KeyS: Btn.X, KeyQ: Btn.L, KeyW: Btn.R,
  Enter: Btn.Start, ShiftRight: Btn.Select, ShiftLeft: Btn.Select,
}

/**
 * padMask is the buttons held on a standard-layout gamepad. The left stick
 * is the d-pad too, unless stickAsDpad is false (for a console that has
 * sticks of its own, which get them as sticks).
 */
export function padMask(pad: Pick<Gamepad, 'buttons' | 'axes'>, stickAsDpad = true): number {
  let mask = 0
  pad.buttons.forEach((b, i) => {
    if (i < padButtons.length && (b.pressed || b.value > 0.5)) mask |= 1 << padButtons[i]
  })
  if (!stickAsDpad) return mask
  const [x = 0, y = 0] = pad.axes
  if (x < -0.5) mask |= 1 << Btn.Left
  if (x > 0.5) mask |= 1 << Btn.Right
  if (y < -0.5) mask |= 1 << Btn.Up
  if (y > 0.5) mask |= 1 << Btn.Down
  return mask
}

/**
 * stickValues are a standard-layout gamepad's sticks (left x, left y, right
 * x, right y) as the signed bytes the server takes, with a small dead zone
 * so a stick at rest is still.
 */
export function stickValues(pad: Pick<Gamepad, 'axes'>): number[] {
  return [0, 1, 2, 3].map((i) => {
    const v = pad.axes[i] ?? 0
    if (Math.abs(v) < 0.12) return 0
    return Math.max(-128, Math.min(127, Math.round(v * 127)))
  })
}

/** How long things take, in milliseconds, for the lag numbers. */
export interface Lag {
  /** There and back over the network. */
  roundTrip?: number
  /** The server running the frame and encoding it. */
  server?: number
  /** Decoding a frame. */
  decode?: number
  /** Frames a second arriving. */
  fps?: number
  /**
   * In the lag test, timed for real: from pressing A or B to the white
   * picture arriving in this player, the median of the last few presses.
   */
  measured?: number
  controller?: string
}

export interface GameStream {
  /** Ends the game; its save is kept. */
  stop: () => void
}

export interface StreamOptions {
  path?: string
  lagTest?: boolean
  /** Called once the server says which system plays the game. */
  onStart?: (game: { system: string; analog: boolean }) => void
  video: HTMLVideoElement
  onLag: (lag: Lag) => void
  onEnd: (error?: string) => void
}

export async function startGame(o: StreamOptions): Promise<GameStream> {
  const pc = new RTCPeerConnection()
  pc.addTransceiver('video', { direction: 'recvonly' })
  pc.addTransceiver('audio', { direction: 'recvonly' })
  // Buttons are sent again and again, so a lost message needn't be resent:
  // unordered with no retries, the newest always gets through first.
  const dc = pc.createDataChannel('input', { ordered: false, maxRetransmits: 0 })
  const media = new MediaStream()
  pc.ontrack = (e) => {
    media.addTrack(e.track)
    // Play as soon as a frame arrives instead of buffering for smoothness.
    const r = e.receiver as RTCRtpReceiver & { jitterBufferTarget?: number | null; playoutDelayHint?: number }
    if ('jitterBufferTarget' in r) r.jitterBufferTarget = 0
    r.playoutDelayHint = 0
    o.video.srcObject = media
  }
  await pc.setLocalDescription(await pc.createOffer())
  await gathered(pc)
  let id = ''
  let analog = false
  try {
    const res = await api.playGame({ path: o.path, lagTest: o.lagTest, offer: pc.localDescription!.sdp })
    id = res.id
    analog = !!res.analog
    o.onStart?.({ system: res.system, analog })
    await pc.setRemoteDescription({ type: 'answer', sdp: res.answer })
  } catch (e) {
    pc.close()
    throw e
  }

  let ended = false
  const end = (error?: string) => {
    if (ended) return
    ended = true
    cancelAnimationFrame(frame)
    clearInterval(timer)
    removeEventListener('keydown', onKey)
    removeEventListener('keyup', onKey)
    pc.close()
    api.stopGame(id).catch(() => {})
    o.onEnd(error)
  }
  pc.onconnectionstatechange = () => {
    if (pc.connectionState === 'failed') end('failed')
  }

  // Buttons: whatever is held on the keyboard and every controller, sent
  // when it changes and every 50 ms besides, so a lost message is soon
  // made good.
  const keys = new Set<number>()
  let sent = -1
  let sentAt = 0
  let controller: string | undefined
  // The lag test turns white while A or B is held: time from the press to
  // the first white frame.
  const flash = (1 << Btn.A) | (1 << Btn.B)
  let pressedAt: number | undefined
  const measured: number[] = []
  const send = (mask: number) => {
    if (dc.readyState !== 'open') return
    if (o.lagTest && mask & flash && !(sent & flash) && sent >= 0) pressedAt = performance.now()
    dc.send(new Uint8Array([1, 0, mask & 0xff, mask >> 8]))
    sent = mask
    sentAt = performance.now()
  }
  const onKey = (e: KeyboardEvent) => {
    const b = keyButtons[e.code]
    if (b === undefined) return
    e.preventDefault()
    if (e.type === 'keydown') keys.add(b)
    else keys.delete(b)
  }
  addEventListener('keydown', onKey)
  addEventListener('keyup', onKey)
  if (o.lagTest && 'requestVideoFrameCallback' in o.video) {
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 4
    const g = canvas.getContext('2d', { willReadFrequently: true })!
    const onFrame = () => {
      if (ended) return
      if (pressedAt !== undefined) {
        g.drawImage(o.video, 0, 0, 4, 4)
        const [r, gr, b] = g.getImageData(0, 0, 1, 1).data
        if (r > 200 && gr > 200 && b > 200) {
          measured.push(performance.now() - pressedAt)
          if (measured.length > 5) measured.shift()
          lag.measured = [...measured].sort((a, b) => a - b)[measured.length >> 1]
          pressedAt = undefined
        }
      }
      o.video.requestVideoFrameCallback(onFrame)
    }
    o.video.requestVideoFrameCallback(onFrame)
  }
  // Sticks, for a console that has them: each sent when it moves, all of
  // them every 50 ms besides.
  const sticks = [0, 0, 0, 0]
  let sticksAt = 0
  const sendStick = (axis: number, v: number) => {
    if (dc.readyState !== 'open') return
    dc.send(new Uint8Array([3, 0, axis, v & 0xff]))
    sticks[axis] = v
  }
  let frame = 0
  const poll = () => {
    let mask = 0
    let held = [0, 0, 0, 0]
    for (const k of keys) {
      // With sticks, the arrow keys are the left stick.
      if (analog && k === Btn.Left) held[0] = -127
      else if (analog && k === Btn.Right) held[0] = 127
      else if (analog && k === Btn.Up) held[1] = -127
      else if (analog && k === Btn.Down) held[1] = 127
      else mask |= 1 << k
    }
    for (const pad of navigator.getGamepads?.() ?? []) {
      if (!pad) continue
      controller ??= pad.id
      mask |= padMask(pad, !analog)
      if (analog) {
        const v = stickValues(pad)
        // The first controller with a stick off centre has its way.
        if (held.every((h) => h === 0)) held = v
      }
    }
    if (mask !== sent || performance.now() - sentAt > 50) send(mask)
    if (analog) {
      const again = performance.now() - sticksAt > 50
      held.forEach((v, axis) => {
        if (v !== sticks[axis] || again) sendStick(axis, v)
      })
      if (again) sticksAt = performance.now()
    }
    frame = requestAnimationFrame(poll)
  }
  frame = requestAnimationFrame(poll)

  // Lag: pings over the data channel for the network, the server's own
  // numbers, and the browser's for decoding.
  const lag: Lag = {}
  let lastStats: { frames: number; at: number; decode: number; decoded: number } | undefined
  dc.onmessage = (e) => {
    if (typeof e.data === 'string') {
      try {
        const s = JSON.parse(e.data) as { encodeMs: number; frameMs: number }
        // Pressing waits for the next frame, which takes a frame's time
        // to run, then it's encoded.
        lag.server = s.encodeMs + s.frameMs
      } catch {
        // not ours
      }
      return
    }
    const b = new DataView(e.data as ArrayBuffer)
    if (b.byteLength === 9 && b.getUint8(0) === 2) lag.roundTrip = performance.now() - b.getFloat64(1)
  }
  dc.binaryType = 'arraybuffer'
  const timer = setInterval(async () => {
    if (dc.readyState === 'open') {
      const ping = new DataView(new ArrayBuffer(9))
      ping.setUint8(0, 2)
      ping.setFloat64(1, performance.now())
      dc.send(ping.buffer)
    }
    const stats = await pc.getStats()
    stats.forEach((r) => {
      if (r.type !== 'inbound-rtp' || r.kind !== 'video') return
      const now = { frames: r.framesDecoded ?? 0, at: r.timestamp, decode: r.totalDecodeTime ?? 0, decoded: r.framesDecoded ?? 0 }
      if (lastStats) {
        const frames = now.frames - lastStats.frames
        const secs = (now.at - lastStats.at) / 1000
        if (secs > 0) lag.fps = frames / secs
        const decoded = now.decoded - lastStats.decoded
        if (decoded > 0) lag.decode = ((now.decode - lastStats.decode) / decoded) * 1000
      }
      lastStats = now
    })
    lag.controller = controller
    o.onLag({ ...lag })
  }, 1000)

  return { stop: () => end() }
}

/** Waits until the offer has every address the server could reach us at. */
function gathered(pc: RTCPeerConnection): Promise<void> {
  if (pc.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise((resolve) => {
    const done = () => {
      if (pc.iceGatheringState === 'complete') resolve()
    }
    pc.addEventListener('icegatheringstatechange', done)
    // Don't wait forever on an address that never answers.
    setTimeout(resolve, 2000)
  })
}
