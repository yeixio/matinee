import { clock } from '../format'
import { sheetUrl, spotAt, type Thumbnails } from './thumbnails'

/**
 * The picture above the seek bar while someone hovers over it or drags
 * it: the frame at that point and its time. Without pictures, just the
 * time. It sits over the point on the bar, kept inside the bar's ends.
 */
export function SeekPreview({ id, thumbs, at, duration }: { id: number; thumbs?: Thumbnails; at: number; duration: number }) {
  const spot = spotAt(thumbs, at)
  const frac = duration ? Math.min(1, Math.max(0, at / duration)) : 0
  // Shown a little bigger than made on big screens, smaller on phones.
  const w = spot ? 'var(--seek-preview-w)' : '4rem'
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
      <span className="rounded bg-black/80 px-1.5 py-0.5 text-xs font-semibold text-white tabular-nums">{clock(at)}</span>
    </div>
  )
}

/** Loads every sheet ahead, so the pictures are there the moment the bar is touched. */
export function preloadSheets(id: number, thumbs?: Thumbnails) {
  if (thumbs?.state !== 'ready' || !thumbs.sheets) return
  for (let n = 0; n < thumbs.sheets; n++) new Image().src = sheetUrl(id, n, thumbs.tag)
}
