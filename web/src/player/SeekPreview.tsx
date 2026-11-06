import { clock } from '../format'
import { sheetUrl, spotAt, type Thumbnails } from './thumbnails'

/**
 * The picture above the seek bar while someone hovers over it or drags
 * it: the frame at that point and its time. Without pictures, just the
 * time. It sits over the point on the bar, kept inside the bar's ends.
 */
export function SeekPreview({ id, thumbs, at, duration, chapter }: { id: number; thumbs?: Thumbnails; at: number; duration: number; chapter?: string }) {
  const spot = spotAt(thumbs, at)
  const frac = duration ? Math.min(1, Math.max(0, at / duration)) : 0
  // Shown a little bigger than made on big screens, smaller on phones.
  const w = spot ? 'var(--seek-preview-w)' : chapter ? '10rem' : '4rem'
  const scale = (n: number) => `calc(${n} * var(--seek-preview-w) / ${thumbs?.width ?? 1})`
  return (
    <div
      className="pointer-events-none absolute bottom-full mb-3 flex flex-col items-center gap-1 [--seek-preview-w:10rem] md:[--seek-preview-w:15rem]"
      style={{ left: `clamp(0px, calc(${frac * 100}% - ${w} / 2), calc(100% - ${w}))`, width: w }}
      aria-hidden="true"
      data-testid="seek-preview"
    >
      {spot && thumbs && (
        <div
          className="w-full rounded-md bg-black bg-no-repeat shadow-2xl ring-1 ring-white/30"
          style={{
            aspectRatio: `${thumbs.width} / ${thumbs.height}`,
            backgroundImage: `url(${sheetUrl(id, spot.sheet, thumbs.tag)})`,
            backgroundSize: `${scale(thumbs.columns! * thumbs.width!)} ${scale(thumbs.rows! * thumbs.height!)}`,
            backgroundPosition: `calc(-1 * ${scale(spot.x)}) calc(-1 * ${scale(spot.y)})`,
          }}
        />
      )}
      {chapter && (
        <span className="max-w-full truncate rounded bg-black/80 px-1.5 py-0.5 text-xs text-white/90" data-testid="seek-chapter">
          {chapter}
        </span>
      )}
      <span className="rounded bg-black/80 px-1.5 py-0.5 text-xs font-semibold text-white tabular-nums">{clock(at)}</span>
    </div>
  )
}

/** Loads every sheet ahead, so the pictures are there the moment the bar is touched. */
export function preloadSheets(id: number, thumbs?: Thumbnails) {
  if (thumbs?.state !== 'ready' || !thumbs.sheets) return
  for (let n = 0; n < thumbs.sheets; n++) new Image().src = sheetUrl(id, n, thumbs.tag)
}

/**
 * What stays on screen while a far seek starts the stream again, instead of
 * black: the preview picture of where it's going, or else the last frame
 * that was showing. The spinner shows over it.
 */
export function HeldFrame({ id, thumbs, at, still }: { id: number; thumbs?: Thumbnails; at: number; still?: string }) {
  const spot = spotAt(thumbs, at)
  if (spot && thumbs && thumbs.columns! > 1 && thumbs.rows! > 1) {
    const col = spot.x / thumbs.width!
    const row = spot.y / thumbs.height!
    return (
      <div className="pointer-events-none absolute inset-0 grid place-items-center bg-black" aria-hidden="true" data-testid="held-frame">
        <div
          className="max-h-full w-full bg-no-repeat"
          style={{
            aspectRatio: `${thumbs.width} / ${thumbs.height}`,
            maxWidth: `calc(100vh * ${thumbs.width} / ${thumbs.height})`,
            backgroundImage: `url(${sheetUrl(id, spot.sheet, thumbs.tag)})`,
            backgroundSize: `${thumbs.columns! * 100}% ${thumbs.rows! * 100}%`,
            backgroundPosition: `${(col / (thumbs.columns! - 1)) * 100}% ${(row / (thumbs.rows! - 1)) * 100}%`,
          }}
        />
      </div>
    )
  }
  if (!still) return null
  return (
    <div className="pointer-events-none absolute inset-0 bg-black" aria-hidden="true" data-testid="held-frame">
      <img src={still} alt="" className="size-full object-contain" />
    </div>
  )
}

/** The frame a video is showing now, as an image URL, or undefined when it can't be read. */
export function grabFrame(el: HTMLVideoElement | null): string | undefined {
  if (!el || !el.videoWidth || !el.videoHeight || el.readyState < 2) return undefined
  try {
    // A 4K frame is far more than a stand-in needs.
    const k = Math.min(1, 1280 / el.videoWidth)
    const c = document.createElement('canvas')
    c.width = Math.round(el.videoWidth * k)
    c.height = Math.round(el.videoHeight * k)
    c.getContext('2d')?.drawImage(el, 0, 0, c.width, c.height)
    return c.toDataURL('image/jpeg', 0.8)
  } catch {
    return undefined
  }
}
