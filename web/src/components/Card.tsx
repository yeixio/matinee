import { Check, Play } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Item } from '../api'
import { episodeCode, progressFraction, seasonTitle } from '../format'
import i18n from '../i18n'
import { Artwork } from './Artwork'

export const itemLink = (item: Item) => `/item/${item.id}`

function subtitle(item: Item): string {
  switch (item.kind) {
    case 'episode':
      return item.showTitle ? `${episodeCode(item.seasonIndex, item.index)} · ${item.title}` : ''
    case 'season':
      return item.childCount ? i18n.t('common:count.episodes', { count: item.childCount }) : ''
    case 'show':
      return item.childCount ? i18n.t('common:count.seasons', { count: item.childCount }) : ''
    case 'artist':
      return item.childCount ? i18n.t('common:count.albums', { count: item.childCount }) : ''
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
          className={`${wide ? 'aspect-video' : square ? 'aspect-square' : 'aspect-[2/3]'} rounded-xl ring-1 ring-text/5 transition duration-200 group-hover:ring-accent/70 group-hover:brightness-110`}
        />
        {item.playable && (
          <div className="absolute inset-0 grid place-items-center opacity-0 transition-opacity group-hover:opacity-100">
            <span className="grid size-12 place-items-center rounded-full bg-black/60 backdrop-blur">
              <Play className="size-5 translate-x-0.5 fill-white text-white" />
            </span>
          </div>
        )}
        {item.played && (
          <span className="absolute top-2 end-2 grid size-6 place-items-center rounded-full bg-accent text-on-accent shadow">
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
        <div className="truncate text-sm font-medium text-text group-hover:text-accent-strong">{item.showTitle || seasonTitle(item)}</div>
        <div className="truncate text-xs text-muted">{subtitle(item)}</div>
      </div>
    </Link>
  )
}

// Big libraries only draw the rows near the screen; a few thousand cards at
// once make switching libraries slow.
export function PosterGrid({ items, wide = false }: { items: Item[]; wide?: boolean }) {
  if (items.length > 120) return <VirtualPosterGrid items={items} wide={wide} />
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

const gapX = 16 // gap-x-4
const gapY = 24 // gap-y-6
const captionHeight = 44 // mt-2 plus the title and subtitle lines

function VirtualPosterGrid({ items, wide }: { items: Item[]; wide: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [top, setTop] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => {
      setWidth(el.clientWidth)
      setTop(el.getBoundingClientRect().top + window.scrollY)
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // The same columns the CSS grid above would make.
  const min = wide ? 240 : 140
  const cols = Math.max(1, Math.floor((width + gapX) / (min + gapX)))
  const colWidth = width ? (width - gapX * (cols - 1)) / cols : min
  const square = items[0]?.kind === 'album' || items[0]?.kind === 'artist'
  const ratio = wide ? 9 / 16 : square ? 1 : 3 / 2
  const rowHeight = Math.round(colWidth * ratio + captionHeight + gapY)

  const rows = useWindowVirtualizer({
    count: Math.ceil(items.length / cols),
    estimateSize: () => rowHeight,
    overscan: 4,
    scrollMargin: top,
  })
  useEffect(() => rows.measure(), [rows, rowHeight])

  return (
    <div ref={ref} className="relative" style={{ height: rows.getTotalSize() }}>
      {width > 0 &&
        rows.getVirtualItems().map((row) => (
          <div
            key={row.key}
            className="absolute inset-x-0 top-0 grid gap-x-4"
            style={{
              gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
              transform: `translateY(${row.start - top}px)`,
            }}
          >
            {items.slice(row.index * cols, (row.index + 1) * cols).map((it) => (
              <PosterCard key={it.id} item={it} wide={wide} />
            ))}
          </div>
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
