// Audio delay, for lip sync: a soundbar, receiver or Bluetooth headphones
// often put the sound a little behind the picture, and some files are off
// themselves. The delay belongs to this browser (its speakers), unless
// it's kept for one title whose file is off.

export const DELAY_STEP_MS = 25
export const MAX_DELAY_MS = 2000

const deviceKey = 'matinee.audioDelay'
const titleKey = (titleId: number) => `matinee.audioDelay.${titleId}`

export type Delay = { ms: number; forTitle: boolean }

export function clampDelay(ms: number): number {
  return Math.max(-MAX_DELAY_MS, Math.min(MAX_DELAY_MS, Math.round(ms / DELAY_STEP_MS) * DELAY_STEP_MS))
}

function read(key: string): number | undefined {
  try {
    const v = localStorage.getItem(key)
    return v === null ? undefined : clampDelay(Number(v) || 0)
  } catch {
    return undefined
  }
}

/** loadDelay is the delay for a title: its own, else this browser's. */
export function loadDelay(titleId: number): Delay {
  const own = read(titleKey(titleId))
  if (own !== undefined) return { ms: own, forTitle: true }
  return { ms: read(deviceKey) ?? 0, forTitle: false }
}

/** saveDelay keeps a delay for this browser, or for one title only. */
export function saveDelay(titleId: number, d: Delay) {
  try {
    if (d.forTitle) localStorage.setItem(titleKey(titleId), String(d.ms))
    else {
      localStorage.removeItem(titleKey(titleId))
      localStorage.setItem(deviceKey, String(d.ms))
    }
  } catch {
    // Private windows can refuse; the delay then lasts while the page is open.
  }
}

/** delayLabel shows a delay as "+125 ms" or "−50 ms". */
export function delayLabel(ms: number, locale?: string): string {
  const n = new Intl.NumberFormat(locale).format(Math.abs(ms))
  return ms > 0 ? `+${n} ms` : ms < 0 ? `−${n} ms` : `${n} ms`
}

// Clear voice: dialogue brought forward and loud moments evened out, by the
// server. Kept for this browser, like the delay.
const voiceKey = 'matinee.clearVoice'

export function loadClearVoice(): boolean {
  try {
    return localStorage.getItem(voiceKey) === '1'
  } catch {
    return false
  }
}

export function saveClearVoice(on: boolean) {
  try {
    if (on) localStorage.setItem(voiceKey, '1')
    else localStorage.removeItem(voiceKey)
  } catch {
    // Private windows can refuse; it then lasts while the page is open.
  }
}
