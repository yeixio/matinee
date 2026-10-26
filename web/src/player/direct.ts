// What a file needs to say for the browser to decide whether it plays as-is.
type Playable = { mimeType?: string; videoCodec?: string; audioCodec?: string }

const videoCodecs: Record<string, string> = {
  h264: 'avc1.640028',
  hevc: 'hvc1.1.6.L120.90',
  vp9: 'vp09.00.40.08',
  av1: 'av01.0.08M.08',
}
const audioCodecs: Record<string, string> = { aac: 'mp4a.40.2', mp3: 'mp4a.6B', opus: 'opus', vorbis: 'vorbis', flac: 'flac' }

// canDirectPlay asks the browser whether it can decode the file as-is.
// Anything uncertain goes through the server, which only re-encodes video
// when it has to.
export function canDirectPlay(item: Playable, el: HTMLVideoElement): boolean {
  if (!item.mimeType || item.mimeType === 'video/x-matroska') return false
  const v = item.videoCodec ? videoCodecs[item.videoCodec] : undefined
  const a = item.audioCodec ? audioCodecs[item.audioCodec] : undefined
  if ((item.videoCodec && !v) || (item.audioCodec && !a)) return false
  const codecs = [v, a].filter(Boolean).join(', ')
  return el.canPlayType(`${item.mimeType}; codecs="${codecs}"`) !== ''
}
