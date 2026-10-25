import { useQuery } from '@tanstack/react-query'
import { Check, ChevronRight, Play, Shuffle } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { api, type Item, type ItemDetail } from '../api'
import { Artwork } from '../components/Artwork'
import { PosterGrid } from '../components/Card'
import { Button, Spinner } from '../components/ui'
import { clock, duration, progressFraction, resolution } from '../format'
import { useAudio } from '../player/AudioPlayer'

export default function ItemPage() {
  const id = Number(useParams().id)
  const { data: item, isPending } = useQuery({ queryKey: ['item', id], queryFn: () => api.item(id) })
  const { data: children } = useQuery({
    queryKey: ['children', id],
    queryFn: () => api.children(id),
    enabled: !!item && !item.playable,
  })

  if (isPending || !item) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="animate-fade space-y-10">
      <Header item={item} kids={children} />
      {/* relative keeps the list above the header's absolutely positioned backdrop */}
      {children && (
        <div className="relative">
          <Children item={item} items={children} />
        </div>
      )}
    </div>
  )
}

function Breadcrumbs({ parents }: { parents: Item[] }) {
  if (parents.length === 0) return null
  return (
    <nav className="flex flex-wrap items-center gap-1 text-sm text-muted">
      {parents.map((p) => (
        <span key={p.id} className="flex items-center gap-1">
          <Link to={`/item/${p.id}`} className="hover:text-text">
            {p.title}
          </Link>
          <ChevronRight className="size-4 text-faint" />
        </span>
      ))}
    </nav>
  )
}

function Header({ item, kids }: { item: ItemDetail; kids?: Item[] }) {
  const audio = useAudio()
  const square = item.kind === 'album' || item.kind === 'artist'
  const resume = !!item.positionMs && !item.played
  // Next up in a season: the episode in progress, else the first unwatched one.
  const firstUnwatched =
    kids?.find((c) => c.playable && !c.played && !!c.positionMs) ?? kids?.find((c) => c.playable && !c.played)

  const facts = [
    item.year,
    item.durationMs && duration(item.durationMs),
    resolution(item.width, item.height),
    item.rating ? `★ ${item.rating.toFixed(1)}` : '',
    item.kind === 'album' && kids ? `${kids.length} tracks` : '',
  ].filter(Boolean)

  return (
    <section className="relative -mx-4 -mt-4 md:-mx-8 md:-mt-8">
      {item.hasBackdrop || item.kind === 'episode' || item.kind === 'season' ? (
        <div className="absolute inset-x-0 top-0 h-[70vh] max-h-[640px] overflow-hidden">
          <Artwork item={item} type="backdrop" label={false} className="size-full opacity-50" />
          <div className="absolute inset-0 bg-gradient-to-t from-ink via-ink/70 to-ink/20" />
        </div>
      ) : (
        <div className="absolute inset-x-0 top-0 h-96 bg-gradient-to-b from-raised to-ink" />
      )}

      <div className="relative flex flex-col gap-6 px-4 pt-8 sm:flex-row sm:items-end md:gap-10 md:px-8 md:pt-24">
        <Artwork
          item={item}
          type="poster"
          className={`w-40 shrink-0 rounded-xl shadow-2xl shadow-black/60 ring-1 ring-white/10 md:w-56 ${
            square ? 'aspect-square' : item.kind === 'episode' ? 'aspect-video w-64 md:w-96' : 'aspect-[2/3]'
          }`}
        />
        <div className="min-w-0 flex-1 space-y-4 pb-1">
          <Breadcrumbs parents={item.parents} />
          <h1 className="text-3xl font-semibold md:text-5xl">{item.title}</h1>
          {facts.length > 0 && (
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted">
              {facts.map((f, i) => (
                <span key={i}>{f}</span>
              ))}
              {item.played && (
                <span className="flex items-center gap-1 text-accent">
                  <Check className="size-4" /> Watched
                </span>
              )}
            </div>
          )}
          {item.overview && <p className="max-w-3xl leading-relaxed text-text/85">{item.overview}</p>}

          <div className="flex flex-wrap gap-3">
            {item.playable && item.kind !== 'track' && (
              <Link to={`/watch/${item.id}`}>
                <Button variant="primary" icon={<Play className="size-4 fill-current" />}>
                  {resume ? `Resume from ${clock(item.positionMs! / 1000)}` : 'Play'}
                </Button>
              </Link>
            )}
            {resume && (
              <div className="flex items-center">
                <div className="h-1 w-32 overflow-hidden rounded-full bg-white/15">
                  <div className="h-full bg-accent" style={{ width: `${progressFraction(item.positionMs, item.durationMs) * 100}%` }} />
                </div>
              </div>
            )}
            {item.kind === 'season' && firstUnwatched && (
              <Link to={`/watch/${firstUnwatched.id}`}>
                <Button variant="primary" icon={<Play className="size-4 fill-current" />}>
                  {firstUnwatched.positionMs ? 'Resume' : 'Play'} episode {firstUnwatched.index ?? ''}
                </Button>
              </Link>
            )}
            {item.kind === 'album' && kids && kids.length > 0 && (
              <>
                <Button variant="primary" icon={<Play className="size-4 fill-current" />} onClick={() => audio.play(kids)}>
                  Play
                </Button>
                <Button icon={<Shuffle className="size-4" />} onClick={() => audio.play([...kids].sort(() => Math.random() - 0.5))}>
                  Shuffle
                </Button>
              </>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}

function Children({ item, items }: { item: ItemDetail; items: Item[] }) {
  if (item.kind === 'season') return <EpisodeList items={items} />
  if (item.kind === 'album') return <TrackList items={items} />
  const title = item.kind === 'show' ? 'Seasons' : item.kind === 'artist' ? 'Albums' : ''
  return (
    <section className="space-y-4">
      {title && <h2 className="text-lg font-semibold">{title}</h2>}
      <PosterGrid items={items} />
    </section>
  )
}

function EpisodeList({ items }: { items: Item[] }) {
  return (
    <section className="space-y-2">
      <h2 className="mb-4 text-lg font-semibold">Episodes</h2>
      {items.map((ep) => (
        <Link
          key={ep.id}
          to={`/watch/${ep.id}`}
          className="group flex gap-4 rounded-xl p-2 transition-colors hover:bg-white/5 md:p-3"
        >
          <div className="relative w-36 shrink-0 md:w-56">
            <Artwork item={ep} type="poster" label={false} className="aspect-video rounded-lg ring-1 ring-white/5" />
            <div className="absolute inset-0 grid place-items-center opacity-0 transition-opacity group-hover:opacity-100">
              <span className="grid size-10 place-items-center rounded-full bg-black/60 backdrop-blur">
                <Play className="size-4 translate-x-0.5 fill-white text-white" />
              </span>
            </div>
            {!!ep.positionMs && !ep.played && (
              <div className="absolute inset-x-2 bottom-2 h-1 overflow-hidden rounded-full bg-black/60">
                <div className="h-full bg-accent" style={{ width: `${progressFraction(ep.positionMs, ep.durationMs) * 100}%` }} />
              </div>
            )}
          </div>
          <div className="min-w-0 flex-1 py-1">
            <div className="flex items-baseline gap-2">
              <span className="text-sm text-faint tabular-nums">{ep.index ?? '–'}</span>
              <span className="truncate font-medium group-hover:text-accent-strong">{ep.title}</span>
              {ep.played && <Check className="size-4 shrink-0 text-accent" />}
            </div>
            <div className="mt-0.5 text-xs text-muted">{duration(ep.durationMs)}</div>
            {ep.overview && <p className="mt-2 line-clamp-2 hidden text-sm text-muted sm:block">{ep.overview}</p>}
          </div>
        </Link>
      ))}
    </section>
  )
}

function TrackList({ items }: { items: Item[] }) {
  const audio = useAudio()
  const discs = new Set(items.map((t) => t.disc ?? 1)).size > 1
  return (
    <section className="divide-y divide-line/50 rounded-xl border border-line/50">
      {items.map((t, i) => {
        const current = audio.current?.id === t.id
        return (
          <button
            key={t.id}
            onClick={() => audio.play(items, i)}
            className={`flex w-full items-center gap-4 px-4 py-3 text-left transition-colors hover:bg-white/5 ${current ? 'text-accent' : ''}`}
          >
            <span className="w-8 text-right text-sm text-faint tabular-nums">
              {current && audio.playing ? '▶' : discs ? `${t.disc ?? 1}.${t.index ?? ''}` : (t.index ?? i + 1)}
            </span>
            <span className="min-w-0 flex-1 truncate">{t.title}</span>
            <span className="text-sm text-muted tabular-nums">{t.durationMs ? clock(t.durationMs / 1000) : ''}</span>
          </button>
        )
      })}
    </section>
  )
}
