// A small WebVTT reader for the subtitles the server sends. The player draws
// cues itself rather than using <track>, so they stay in sync when a
// transcode starts partway into a video, and move clear of the controls.

export type Cue = { start: number; end: number; text: string }

const timing = /^((?:\d+:)?\d{1,2}:\d{2}[.,]\d{3})\s+-->\s+((?:\d+:)?\d{1,2}:\d{2}[.,]\d{3})/

function seconds(ts: string): number {
  const parts = ts.replace(',', '.').split(':').map(Number)
  return parts.reduce((acc, p) => acc * 60 + p, 0)
}

export function parseVTT(text: string): Cue[] {
  const cues: Cue[] = []
  const blocks = text.replace(/^﻿/, '').replace(/\r\n?/g, '\n').split(/\n{2,}/)
  for (const block of blocks) {
    const lines = block.split('\n')
    const at = lines.findIndex((l) => timing.test(l))
    if (at < 0) continue // header, NOTE, STYLE or REGION blocks
    const m = timing.exec(lines[at])!
    const body = lines.slice(at + 1).join('\n').trim()
    if (body) cues.push({ start: seconds(m[1]), end: seconds(m[2]), text: body })
  }
  return cues.sort((a, b) => a.start - b.start)
}

// activeCues returns the cues showing at time t, in start order.
export function activeCues(cues: Cue[], t: number): Cue[] {
  // Binary search for the first cue starting after t; anything showing starts before it.
  let lo = 0
  let hi = cues.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (cues[mid].start <= t) lo = mid + 1
    else hi = mid
  }
  const out: Cue[] = []
  // Cues rarely last more than a few seconds; look back a bounded way for long ones.
  for (let i = lo - 1; i >= 0 && i >= lo - 50; i--) {
    if (cues[i].end > t) out.unshift(cues[i])
  }
  return out
}

export type Span = { text: string; italic?: boolean; bold?: boolean; underline?: boolean }

const entities: Record<string, string> = { amp: '&', lt: '<', gt: '>', nbsp: ' ', lrm: '‎', rlm: '‏' }

// spans splits cue text into styled runs. Only <i>, <b> and <u> are kept;
// voice, class and timestamp tags are dropped, and nothing is parsed as HTML.
export function spans(text: string): Span[] {
  const out: Span[] = []
  const style = { i: 0, b: 0, u: 0 }
  for (const part of text.split(/(<[^>]*>)/)) {
    if (!part) continue
    const tag = /^<(\/?)([a-z]+)[^>]*>$/i.exec(part)
    if (tag) {
      const name = tag[2].toLowerCase() as keyof typeof style
      if (name in style) style[name] = Math.max(0, style[name] + (tag[1] ? -1 : 1))
      continue
    }
    if (part.startsWith('<')) continue // timestamps like <00:01.000>
    out.push({
      text: part.replace(/&(amp|lt|gt|nbsp|lrm|rlm);/g, (_, e: string) => entities[e]),
      italic: style.i > 0 || undefined,
      bold: style.b > 0 || undefined,
      underline: style.u > 0 || undefined,
    })
  }
  return out
}
