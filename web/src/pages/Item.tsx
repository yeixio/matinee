import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronRight, ImageOff, LoaderCircle, Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useParams } from 'react-router-dom'
import { api, pendingRefetch, type Item, type ItemDetail, type User } from '../api'
import { ItemQuality, ReportProblem } from '../components/Arr'
import { Artwork } from '../components/Artwork'
import { FileDetailsButton, Languages, QualityBadges } from '../components/MediaInfo'
import { PosterGrid } from '../components/Card'
import { AddToCollectionButton } from '../components/Collections'
import { CastRows, KeyPeople, MoreDetailsButton } from '../components/Credits'
import { RelatedRows } from '../components/Related'
import { StarRating } from '../components/StarRating'
import { TrailerButton } from '../components/Trailer'
import { Button, Spinner } from '../components/ui'
import { WatchListButton } from '../components/WatchListButton'
import { clock, duration, progressFraction, ratingLabel, seasonTitle } from '../format'
import { genreName } from '../people'
import { AlbumPage, ArtistPage } from './MusicItem'
import { AuthorPage, BookPage } from './BookItem'
import { EpisodePage, PodcastPage } from './PodcastItem'

export default function ItemPage() {
  const id = Number(useParams().id)
  const { data: item, isPending } = useQuery({
    queryKey: ['item', id],
    queryFn: () => api.item(id),
    refetchInterval: (q) => pendingRefetch(q.state.data && [q.state.data]),
  })
  const { data: children } = useQuery({
    queryKey: ['children', id],
    queryFn: () => api.children(id),
    enabled: !!item && !item.playable,
    refetchInterval: (q) => pendingRefetch(q.state.data),
  })

  if (isPending || !item) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  if (item.kind === 'album') return <AlbumPage item={item} tracks={children} />
  if (item.kind === 'artist') return <ArtistPage item={item} albums={children} />
  if (item.kind === 'book') return <BookPage item={item} />
  if (item.kind === 'author') return <AuthorPage item={item} books={children} />
  if (item.kind === 'podcast') return <PodcastPage item={item} episodes={children} />
  if (item.kind === 'podcastEpisode') return <EpisodePage item={item} />
  // A song opens its album, and a book's part its book.
  if ((item.kind === 'track' || item.kind === 'part') && item.parentId) return <Navigate to={`/item/${item.parentId}`} replace />

  return (
    <div className="animate-fade space-y-10">
      <Header item={item} kids={children} />
      {/* relative keeps the list above the header's absolutely positioned backdrop */}
      {children && (
        <div className="relative">
          <Children item={item} items={children} />
        </div>
      )}
      <div className="relative space-y-10">
        <CastRows item={item} />
        <RelatedRows item={item} />
      </div>
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
            {seasonTitle(p)}
          </Link>
          <ChevronRight className="size-4 text-faint rtl:-scale-x-100" />
        </span>
      ))}
    </nav>
  )
}

function Header({ item, kids }: { item: ItemDetail; kids?: Item[] }) {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const resume = !!item.positionMs && !item.played
  // Next up in a season: the episode in progress, else the first unwatched one.
  const firstUnwatched =
    kids?.find((c) => c.playable && !c.played && !!c.positionMs) ?? kids?.find((c) => c.playable && !c.played)

  const d = item.details
  const facts = [
    item.year,
    d?.certification,
    item.durationMs && duration(item.durationMs),
    item.rating ? `★ ${ratingLabel(item.rating)}` : '',
    d?.genres?.slice(0, 3).map(genreName).join(', '),
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
          className={`w-40 shrink-0 rounded-xl shadow-2xl shadow-black/60 ring-1 ring-text/10 md:w-56 ${
            item.kind === 'episode' ? 'aspect-video w-64 md:w-96' : 'aspect-[2/3]'
          }`}
        />
        <div className="min-w-0 flex-1 space-y-4 pb-1">
          <Breadcrumbs parents={item.parents} />
          <h1 className="text-3xl font-semibold md:text-5xl">{seasonTitle(item)}</h1>
          {facts.length > 0 && (
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted">
              {facts.map((f, i) => (
                <span key={i}>{f}</span>
              ))}
              {item.played && (
                <span className="flex items-center gap-1 text-accent">
                  <Check className="size-4" /> {t('common:watched.watched')}
                </span>
              )}
            </div>
          )}
          <QualityBadges item={item} />
          <Languages item={item} />
          {d?.tagline && item.overview && <p className="max-w-3xl text-text/70 italic">{d.tagline}</p>}
          {item.overview ? (
            <p className="max-w-3xl leading-relaxed text-text/85">{item.overview}</p>
          ) : (
            item.pending && (
              <p className="flex items-center gap-2 text-sm text-muted">
                <LoaderCircle className="size-4 animate-spin" /> {t('artwork.fetching')}
              </p>
            )
          )}

          <KeyPeople item={item} />
          <Facts item={item} />

          <div className="flex flex-wrap gap-3">
            {item.playable && (
              <Link to={`/watch/${item.id}`}>
                <Button variant="primary" icon={<Play className="size-4 fill-current" />}>
                  {resume ? t('item.resumeFrom', { time: clock(item.positionMs! / 1000) }) : t('item.play')}
                </Button>
              </Link>
            )}
            {resume && (
              <div className="flex items-center">
                <div className="h-1 w-32 overflow-hidden rounded-full bg-text/15">
                  <div className="h-full bg-accent" style={{ width: `${progressFraction(item.positionMs, item.durationMs) * 100}%` }} />
                </div>
              </div>
            )}
            {item.kind === 'season' && firstUnwatched && (
              <Link to={`/watch/${firstUnwatched.id}`}>
                <Button variant="primary" icon={<Play className="size-4 fill-current" />}>
                  {t(firstUnwatched.positionMs ? 'item.resumeEpisode' : 'item.playEpisode', { episode: firstUnwatched.index ?? '' })}
                </Button>
              </Link>
            )}
            <TrailerButton item={item} />
            <WatchListButton item={item} />
            <AddToCollectionButton item={item} />
            <ItemQuality item={item} user={me} />
            <ReportProblem item={item} />
            <MoreDetailsButton item={item} />
            <FileDetailsButton item={item} user={me} />
            <StarRating item={item} />
            {me?.isAdmin && item.posterTag && <ResetPosterButton item={item} />}
          </div>
        </div>
      </div>
    </section>
  )
}

// A poster of its own, such as one imported from another media server, can
// give way to the automatic one.
function ResetPosterButton({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const reset = useMutation({
    mutationFn: () => api.resetCover(item.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['item', item.id] }),
  })
  return (
    <Button variant="ghost" icon={<ImageOff className="size-4" />} disabled={reset.isPending} onClick={() => reset.mutate()}>
      {t('item.resetPoster')}
    </Button>
  )
}

function Children({ item, items }: { item: ItemDetail; items: Item[] }) {
  const { t } = useTranslation()
  if (item.kind === 'season') return <EpisodeList items={items} />
  const title = item.kind === 'show' ? t('item.seasons') : ''
  return (
    <section className="space-y-4">
      {title && <h2 className="text-lg font-semibold">{title}</h2>}
      <PosterGrid items={items} />
    </section>
  )
}

function EpisodeList({ items }: { items: Item[] }) {
  const { t } = useTranslation()
  return (
    <section className="space-y-2">
      <h2 className="mb-4 text-lg font-semibold">{t('item.episodes')}</h2>
      {items.map((ep) => (
        <Link
          key={ep.id}
          to={`/watch/${ep.id}`}
          className="group flex gap-4 rounded-xl p-2 transition-colors hover:bg-text/5 md:p-3"
        >
          <div className="relative w-36 shrink-0 md:w-56">
            <Artwork item={ep} type="poster" label={false} className="aspect-video rounded-lg ring-1 ring-text/5" />
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

// Facts are labelled lines a plugin found for an item of its kinds, such as
// who made a game.
function Facts({ item }: { item: Item }) {
  if (!item.facts?.length) return null
  return (
    <dl className="grid max-w-3xl grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
      {item.facts.map((f, i) => (
        <div key={i} className="contents">
          <dt className="text-muted">{f.label}</dt>
          <dd className="text-text/85">{f.value}</dd>
        </div>
      ))}
    </dl>
  )
}
