import { Play, X } from 'lucide-react'
import type { Item } from '../api'
import { Artwork } from '../components/Artwork'
import { episodeCode } from '../format'
import { COUNTDOWN_SECONDS } from './upnext'

export type UpNextMode = 'countdown' | 'still' | 'similar'

// The card in the corner when an episode's credits start: the next episode
// counting down, "Still watching?" after a few in a row, or after a show's
// last episode a similar title.
export function UpNextCard({
  mode,
  item,
  left,
  onPlay,
  onCancel,
  lifted,
}: {
  mode: UpNextMode
  item: Item
  left: number // seconds, while counting down
  onPlay: () => void
  onCancel: () => void
  lifted: boolean // the player's controls are showing
}) {
  const heading = { countdown: 'Up next', still: 'Still watching?', similar: 'You might like' }[mode]
  const sub = item.kind === 'episode' ? `${episodeCode(item.seasonIndex, item.index)} · ${item.title}` : item.year ? String(item.year) : ''
  const title = item.kind === 'episode' ? item.showTitle || item.title : item.title
  return (
    <div
      role="dialog"
      aria-label={heading}
      className={`absolute right-4 z-10 w-[min(20rem,calc(100%-2rem))] overflow-hidden rounded-xl bg-black/85 shadow-2xl ring-1 ring-white/10 backdrop-blur transition-[bottom] duration-300 md:right-8 ${
        lifted ? 'bottom-32' : 'bottom-8'
      }`}
      onClick={(e) => e.stopPropagation()}
    >
      {/* Phones keep to the words, so the card doesn't cover the picture. */}
      <div className="relative hidden sm:block">
        <Artwork item={item} type={item.kind === 'episode' ? 'poster' : 'backdrop'} className="aspect-video w-full" label={false} />
        {mode === 'countdown' && (
          <div className="absolute inset-x-0 bottom-0 h-1 bg-white/20">
            <div
              className="h-full bg-accent transition-[width] duration-1000 ease-linear"
              style={{ width: `${(left / COUNTDOWN_SECONDS) * 100}%` }}
            />
          </div>
        )}
      </div>
      <div className="space-y-3 p-4">
        <div>
          <div className="text-xs font-semibold tracking-wide text-accent uppercase">{heading}</div>
          <div className="mt-1 truncate font-semibold">{title}</div>
          {sub && <div className="truncate text-sm text-white/70">{sub}</div>}
          {mode === 'countdown' && (
            <div className="mt-1 text-sm text-white/60 tabular-nums sm:hidden" aria-hidden>
              <div className="mb-2 h-1 overflow-hidden rounded-full bg-white/20">
                <div className="h-full bg-accent transition-[width] duration-1000 ease-linear" style={{ width: `${(left / COUNTDOWN_SECONDS) * 100}%` }} />
              </div>
            </div>
          )}
          {mode === 'countdown' && (
            <div className="mt-1 text-sm text-white/60 tabular-nums" aria-live="polite">
              Playing in {left}
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <button
            autoFocus
            onClick={onPlay}
            className="inline-flex h-10 flex-1 items-center justify-center gap-2 rounded-lg bg-accent px-4 text-sm font-semibold text-on-accent hover:bg-accent-strong"
          >
            <Play className="size-4 fill-current" />
            {{ countdown: 'Play now', still: 'Keep watching', similar: 'Have a look' }[mode]}
          </button>
          <button
            onClick={onCancel}
            className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-white/10 px-4 text-sm hover:bg-white/15"
          >
            {mode === 'similar' ? <X className="size-4" /> : null}
            {mode === 'similar' ? 'Close' : mode === 'still' ? 'Stop' : 'Cancel'}
          </button>
        </div>
      </div>
    </div>
  )
}
