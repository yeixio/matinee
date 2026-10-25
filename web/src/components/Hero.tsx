import { Info, Play, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { Item } from '../api'
import { duration, episodeCode, progressFraction } from '../format'
import { Artwork } from './Artwork'
import { Button } from './ui'

// The big backdrop at the top of Home: the thing you're most likely to want next.
// onRemove takes it off Continue Watching.
export function Hero({ item, onRemove }: { item: Item; onRemove?: () => void }) {
  const resume = !!item.positionMs && !item.played
  const left = item.durationMs && item.positionMs ? item.durationMs - item.positionMs : 0
  return (
    <section className="relative -mx-4 -mt-4 overflow-hidden md:-mx-8 md:-mt-8">
      <Artwork item={item} type="backdrop" label={false} className="aspect-[4/3] w-full sm:aspect-[16/7]" />
      <div className="absolute inset-0 bg-gradient-to-t from-ink via-ink/40 to-transparent" />
      <div className="absolute inset-0 bg-gradient-to-r from-ink/80 via-ink/20 to-transparent" />
      <div className="absolute inset-x-0 bottom-0 max-w-2xl space-y-3 p-5 md:p-10">
        <span className="inline-block rounded-full bg-accent/15 px-3 py-1 text-xs font-medium text-accent">
          {resume ? 'Up next' : 'Just added'}
        </span>
        <h1 className="text-3xl font-semibold md:text-6xl">{item.showTitle || item.title}</h1>
        <div className="flex gap-3 text-sm text-muted">
          {item.showTitle && (
            <span className="text-text/90">
              {episodeCode(item.seasonIndex, item.index)} · {item.title}
            </span>
          )}
          {item.year && <span>{item.year}</span>}
          {item.durationMs && <span>{duration(item.durationMs)}</span>}
          {item.rating ? <span>★ {item.rating.toFixed(1)}</span> : null}
        </div>
        {item.overview && <p className="line-clamp-2 hidden text-sm text-text/80 sm:block md:text-base">{item.overview}</p>}
        {resume && (
          <div className="h-1 w-48 overflow-hidden rounded-full bg-white/20">
            <div className="h-full bg-accent" style={{ width: `${progressFraction(item.positionMs, item.durationMs) * 100}%` }} />
          </div>
        )}
        <div className="flex gap-3 pt-1">
          {item.playable && (
            <Link to={`/watch/${item.id}`}>
              <Button variant="primary" icon={<Play className="size-4 fill-current" />}>
                {resume ? `Resume${left ? ` · ${duration(left)} left` : ''}` : 'Play'}
              </Button>
            </Link>
          )}
          <Link to={`/item/${item.id}`}>
            <Button icon={<Info className="size-4" />}>Details</Button>
          </Link>
          {onRemove && (
            <Button
              onClick={onRemove}
              icon={<X className="size-4" />}
              aria-label="Remove from Continue watching"
              title="Remove from Continue watching"
              className="px-3"
            />
          )}
        </div>
      </div>
    </section>
  )
}
