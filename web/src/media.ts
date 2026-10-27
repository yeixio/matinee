// Turning the server's media analysis into words people use: badges for
// the details page and filters, and labels for the File details panel.
import type { AudioStream, HDR, Item, ItemDetail, MediaInfo, VideoStream } from './api'
import { resolution } from './format'
import i18n, { formatLocale } from './i18n'

export const hdrLabel: Record<HDR, string> = {
  'dolby-vision': 'Dolby Vision',
  'hdr10+': 'HDR10+',
  hdr10: 'HDR10',
  hlg: 'HLG',
}

// Tags the library can filter on, in the order the chips show.
export const filterTags: { tag: string; label: string }[] = [
  { tag: '4k', label: '4K' },
  { tag: 'hdr', label: 'HDR' },
  { tag: 'dolby-vision', label: 'Dolby Vision' },
  { tag: 'atmos', label: 'Dolby Atmos' },
  { tag: '5.1', label: '5.1' },
  { tag: '7.1', label: '7.1' },
  {
    tag: 'lossless',
    get label() {
      return i18n.t('common:media.lossless')
    },
  },
  {
    tag: 'hi-res',
    get label() {
      return i18n.t('common:media.hiRes')
    },
  },
]

export function channelLabel(n: number, layout?: string): string {
  if (n <= 0) return ''
  if (n === 1) return i18n.t('common:media.mono')
  if (n === 2) return i18n.t('common:media.stereo')
  if (layout && /^\d\.\d/.test(layout)) return layout.replace(/\(.*\)$/, '')
  return n <= 5 ? `${n}.0` : `${n - 1}.1`
}

// The main audio track: the default one, else the first that isn't commentary.
export function mainAudio(mi: MediaInfo): AudioStream | undefined {
  const usable = mi.audio.filter((a) => !a.commentary && !a.descriptive)
  return usable.find((a) => a.default) ?? usable[0] ?? mi.audio[0]
}

export type Badge = { label: string; title?: string }

// The handful of facts everyone cares about, for the main info pane:
// resolution, HDR, and the best sound on offer.
export function qualityBadges(item: Item | ItemDetail): Badge[] {
  const mi = 'mediaInfo' in item ? item.mediaInfo : undefined
  if (mi && !mi.error && (mi.video.length > 0 || mi.audio.length > 0)) {
    const out: Badge[] = []
    const v = mi.video[0]
    if (v?.resolution) out.push({ label: v.resolution, title: `${v.width}×${v.height}` })
    if (v?.hdr) out.push({ label: hdrLabel[v.hdr], title: hdrTitle(v) })
    const best = [...mi.audio].sort((a, b) => audioRank(b) - audioRank(a))[0]
    if (best?.atmos) out.push({ label: 'Dolby Atmos' })
    else if (best?.dtsx) out.push({ label: 'DTS:X' })
    if (best && best.channels > 2) out.push({ label: channelLabel(best.channels, best.channelLayout) })
    if (!v && best && isLossless(best.codec)) {
      out.push({
        label: (best.bitDepth ?? 0) > 16 || (best.sampleRate ?? 0) > 48000 ? i18n.t('common:media.hiRes') : i18n.t('common:media.lossless'),
        title: audioTitle(best),
      })
    }
    return out
  }
  // A show or season (its episodes' tags), or a file not analyzed yet.
  const tags = item.mediaTags ?? []
  if (tags.length > 0) {
    const first = (opts: string[]) => opts.find((t) => tags.includes(t))
    const res = first(['8k', '4k', '1080p', '720p', '576p', '480p', 'sd'])
    const hdr = first(Object.keys(hdrLabel)) as HDR | undefined
    const ch = first(['7.1', '5.1'])
    return [
      res && { label: res === 'sd' ? 'SD' : res.toUpperCase().replace('P', 'p') },
      hdr && { label: hdrLabel[hdr] },
      tags.includes('atmos') ? { label: 'Dolby Atmos' } : tags.includes('dts-x') && { label: 'DTS:X' },
      ch && { label: ch },
      tags.includes('hi-res') ? { label: i18n.t('common:media.hiRes') } : tags.includes('lossless') && { label: i18n.t('common:media.lossless') },
    ].filter((b): b is Badge => !!b)
  }
  const res = resolution(item.width, item.height)
  return res ? [{ label: res }] : []
}

function audioRank(a: AudioStream): number {
  return (a.atmos || a.dtsx ? 100 : 0) + a.channels - (a.commentary ? 1000 : 0)
}

function hdrTitle(v: VideoStream): string {
  if (v.hdr !== 'dolby-vision') return hdrLabel[v.hdr!]
  const dv = v.dvProfile ? i18n.t('common:media.dolbyVisionProfile', { profile: v.dvProfile }) : 'Dolby Vision'
  return v.hdrBase ? i18n.t('common:media.withBase', { format: dv, base: hdrLabel[v.hdrBase] }) : dv
}

export function isLossless(codec: string) {
  return ['flac', 'alac', 'wavpack', 'ape', 'tta', 'truehd', 'mlp'].includes(codec) || codec.startsWith('pcm_')
}

const displayNames = new Map<string, Intl.DisplayNames | undefined>()

// Language names in the page's language.
function names(): Intl.DisplayNames | undefined {
  const locale = formatLocale()
  if (!displayNames.has(locale)) {
    try {
      displayNames.set(locale, new Intl.DisplayNames([locale], { type: 'language' }))
    } catch {
      displayNames.set(locale, undefined)
    }
  }
  return displayNames.get(locale)
}

// "eng" or "en" to "English" (or "Englisch" in German). Unknown or undetermined codes give "".
export function languageName(code?: string): string {
  if (!code || code === 'und' || code === 'mis' || code === 'zxx') return ''
  try {
    const n = names()?.of(code)
    if (n && n !== code && n !== 'root') return n
  } catch {
    // not a language code
  }
  return code.length <= 3 ? code.toUpperCase() : code
}

// "English, French" for the audio languages people can pick, leaving out
// commentaries and audio description.
export function audioLanguages(mi: MediaInfo): string[] {
  const out: string[] = []
  for (const a of mi.audio) {
    if (a.commentary || a.descriptive) continue
    const n = languageName(a.language)
    if (n && !out.includes(n)) out.push(n)
  }
  return out
}

const codecNames: Record<string, string> = {
  h264: 'H.264',
  hevc: 'HEVC (H.265)',
  av1: 'AV1',
  vp9: 'VP9',
  vp8: 'VP8',
  mpeg2video: 'MPEG-2',
  mpeg4: 'MPEG-4',
  vc1: 'VC-1',
  truehd: 'Dolby TrueHD',
  eac3: 'Dolby Digital Plus',
  ac3: 'Dolby Digital',
  dts: 'DTS',
  aac: 'AAC',
  mp3: 'MP3',
  opus: 'Opus',
  vorbis: 'Vorbis',
  flac: 'FLAC',
  alac: 'ALAC',
  subrip: 'SRT',
  ass: 'ASS',
  webvtt: 'WebVTT',
  mov_text: 'MP4 text',
  hdmv_pgs_subtitle: 'PGS',
  dvd_subtitle: 'VobSub',
  dvb_subtitle: 'DVB',
}

export function codecName(codec: string, profile?: string): string {
  // DTS-HD MA and friends are told apart only by profile.
  if (codec === 'dts' && profile && profile !== 'DTS') return profile.replace(' + DTS:X', '')
  if (codec.startsWith('pcm_')) return 'PCM'
  return codecNames[codec] ?? codec.toUpperCase()
}

export function containerName(c: string): string {
  return { matroska: 'MKV', mov: 'MP4', mpegts: 'MPEG-TS', avi: 'AVI', flac: 'FLAC', mp3: 'MP3', ogg: 'Ogg', wav: 'WAV' }[c] ?? c.toUpperCase()
}

export function bitrateLabel(bps?: number): string {
  if (!bps) return ''
  const digits = bps >= 10_000_000 ? 0 : 1
  if (bps >= 1_000_000) return `${decimal(bps / 1_000_000, digits)} Mb/s`
  return `${decimal(Math.round(bps / 1000), 0)} kb/s`
}

export function sizeLabel(bytes?: number): string {
  if (!bytes) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000
    i++
  }
  return `${decimal(n, n >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// A number with exactly this many decimals, in the page's format: 1.5 or 1,5.
function decimal(n: number, digits: number): string {
  return new Intl.NumberFormat(formatLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(n)
}

export function audioTitle(a: AudioStream): string {
  const parts = [codecName(a.codec, a.profile)]
  if (a.atmos) parts.push('Atmos')
  if (a.dtsx) parts.push('DTS:X')
  parts.push(channelLabel(a.channels, a.channelLayout))
  if (a.bitDepth && isLossless(a.codec)) parts.push(i18n.t('common:media.bitDepth', { bits: a.bitDepth }))
  if (a.sampleRate) parts.push(`${decimal(a.sampleRate / 1000, a.sampleRate % 1000 ? 1 : 0)} kHz`)
  return parts.filter(Boolean).join(' · ')
}

// A subtitle track's name in the page's language, such as "Deutsch (Erzwungen)".
export function subtitleLabel(s: { label: string; language?: string; forced?: boolean; sdh?: boolean; note?: string }): string {
  const lang = languageName(s.language)
  if (!lang) return s.label
  const notes = [s.forced && i18n.t('common:media.forced'), s.sdh && i18n.t('common:media.sdh'), s.note].filter(Boolean)
  return notes.length ? `${lang} (${notes.join(', ')})` : lang
}

// The awards worth naming in a title's one-line summary, in order of preference.
const majorAwards: [string, string][] = [
  ['Academy Award for', 'academy'],
  ['Primetime Emmy Award for', 'emmy'],
  ['Golden Globe Award for', 'globe'],
  ['BAFTA Award for', 'bafta'],
  ['British Academy Film Award for', 'bafta'],
]

// Sums awards up in a line, such as "Won 4 Academy Awards · 31 wins and 62
// nominations", in the page's language.
export function awardSummary(awards: { name: string; won: boolean }[]): string {
  if (awards.length === 0) return ''
  const parts: string[] = []
  for (const won of [true, false]) {
    for (const [prefix, key] of majorAwards) {
      const n = awards.filter((a) => a.won === won && a.name.startsWith(prefix)).length
      if (n === 0) continue
      parts.push(i18n.t(`common:awards.${key}.${won ? 'won' : 'nominated'}`, { count: n }))
      break
    }
    if (parts.length > 0) break
  }
  const wins = awards.filter((a) => a.won).length
  const noms = awards.length - wins
  const total =
    wins && noms
      ? i18n.t('common:awards.winsAndNominations', { wins: i18n.t('common:awards.wins', { count: wins }), nominations: i18n.t('common:awards.nominations', { count: noms }) })
      : wins
        ? i18n.t('common:awards.wins', { count: wins })
        : i18n.t('common:awards.nominations', { count: noms })
  return [...parts, total].join(' · ')
}
