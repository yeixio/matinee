import { describe, expect, it } from 'vitest'
import { activeCues, parseVTT, spans } from './vtt'

const sample = `WEBVTT

NOTE made by ffmpeg

STYLE
::cue { color: yellow }

1
00:00:01.000 --> 00:00:04.000
Hello <i>world</i>

00:00:03.500 --> 00:00:05.250 align:start
Second line
and more

01:02:03.004 --> 01:02:04.000
<v Narrator>Much later &amp; &lt;done&gt;
`

describe('parseVTT', () => {
  it('reads cues and skips other blocks', () => {
    const cues = parseVTT(sample.replace(/\n/g, '\r\n'))
    expect(cues).toEqual([
      { start: 1, end: 4, text: 'Hello <i>world</i>' },
      { start: 3.5, end: 5.25, text: 'Second line\nand more' },
      { start: 3723.004, end: 3724, text: '<v Narrator>Much later &amp; &lt;done&gt;' },
    ])
  })

  it('finds the cues showing at a time', () => {
    const cues = parseVTT(sample)
    expect(activeCues(cues, 0.5)).toEqual([])
    expect(activeCues(cues, 2).map((c) => c.start)).toEqual([1])
    expect(activeCues(cues, 3.75).map((c) => c.start)).toEqual([1, 3.5])
    expect(activeCues(cues, 4.5).map((c) => c.start)).toEqual([3.5])
    expect(activeCues(cues, 3723.5)).toHaveLength(1)
    expect(activeCues(cues, 9999)).toEqual([])
  })
})

describe('spans', () => {
  it('keeps simple styling and drops other tags', () => {
    expect(spans('Hello <i>wor<b>ld</b></i>!')).toEqual([
      { text: 'Hello ' },
      { text: 'wor', italic: true },
      { text: 'ld', italic: true, bold: true },
      { text: '!' },
    ])
    expect(spans('<v Narrator>Much later &amp; &lt;done&gt;<00:01.000>')).toEqual([{ text: 'Much later & <done>' }])
    expect(spans('<script>alert(1)</script>')).toEqual([{ text: 'alert(1)' }])
  })
})
