import { Check, Play } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Item } from '../api'
import { episodeCode, progressFraction } from '../format'
import { Artwork } from './Artwork'

export const itemLink = (item: Item) => `/item/${item.id}`

function subtitle(item: Item): string {
  switch (item.kind) {
    case 'episode':
      return item.showTitle ? `${episodeCode(item.seasonIndex, item.index)} · ${item.title}` : ''
    case 'season':
      return item.childCount ? `${item.childCount} episodes` : ''
    case 'show':
      return item.childCount === 1 ? '1 season' : item.childCount ? `${item.childCount} seasons` : ''
    case 'artist':
      return item.childCount === 1 ? '1 album' : item.childCount ? `${item.childCount} albums` : ''
    default:
      return item.year ? String(item.year) : ''
  }
}

// A poster card. Hovering prefetches the detail page so the click feels instant.
export function PosterCard({ item, wide = false }: { item: Item; wide?: boolean }) {
  const qc = useQueryClient()
  const prefetch = () => {
    qc.prefetchQuery({ queryKey: ['item', item.id], queryFn: () => api.item(item.id) })
    if (!item.playable) qc.prefetchQuery({ queryKey: ['children', item.id], queryFn: () => api.children(item.id) })
  }
  const square = item.kind === 'album' || item.kind === 'artist'
  const frac = progressFraction(item.positionMs, item.durationMs)
  return (
    <Link to={itemLink(item)} onMouseEnter={prefetch} onFocus={prefetch} className="group block animate-fade">
      <div className="relative">
        <Artwork
          item={item}
          type={wide ? 'backdrop' : 'poster'}
          className={`${wide ? 'aspect-video' : square ? 'aspect-square' : 'aspect-[2/3]'} rounded-lg ring-1 ring-white/5 transition duration-200 group-hover:ring-accent/70 group-hover:brightness-110`}
        />
        {item.playable && (
          <div className="absolute inset-0 grid place-items-center opacity-0 transition-opacity group-hover:opacity-100">
            <span className="grid size-12 place-items-center rounded-full bg-black/60 backdrop-blur">
              <Play className="size-5 translate-x-0.5 fill-white text-white" />
            </span>
          </div>
        )}
        {item.played && (
          <span className="absolute top-2 right-2 grid size-6 place-items-center rounded-full bg-accent text-ink shadow">
            <Check className="size-4" strokeWidth={3} />
          </span>
        )}
        {frac > 0 && (
          <div className="absolute inset-x-2 bottom-2 h-1 overflow-hidden rounded-full bg-black/60">
            <div className="h-full bg-accent" style={{ width: `${frac * 100}%` }} />
          </div>
        )}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium text-text group-hover:text-accent-strong">{item.showTitle || item.title}</div>
        <div className="truncate text-xs text-muted">{subtitle(item)}</div>
      </div>
    </Link>
  )
}

export function PosterGrid({ items, wide = false }: { items: Item[]; wide?: boolean }) {
  return (
    <div
      className="grid gap-x-4 gap-y-6"
      style={{ gridTemplateColumns: `repeat(auto-fill, minmax(${wide ? 240 : 140}px, 1fr))` }}
    >
      {items.map((it) => (
        <PosterCard key={it.id} item={it} wide={wide} />
      ))}
    </div>
  )
}

export function GridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))' }}>
      {Array.from({ length: count }, (_, i) => (
        <div key={i}>
          <div className="skeleton aspect-[2/3] rounded-lg" />
          <div className="skeleton mt-2 h-3 w-3/4 rounded" />
        </div>
      ))}
    </div>
  )
}
