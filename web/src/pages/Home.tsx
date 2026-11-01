import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, pendingRefetch, type Item } from '../api'
import { ComingSoonRow } from '../components/Arr'
import { PosterCard } from '../components/Card'
import { Hero } from '../components/Hero'
import { Row, RowItem } from '../components/Row'
import { useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'
import { rowLink, rowTitle } from '../rows'

export default function Home() {
  const { t } = useTranslation()
  const { data: libraries } = useLibraries()
  const hide = useHideFromContinueWatching()
  const { data, isPending } = useQuery({
    queryKey: ['home'],
    queryFn: api.home,
    refetchInterval: (q) => pendingRefetch(q.state.data?.recentlyAdded),
  })

  if (libraries && libraries.length === 0) {
    return (
      <EmptyState title={t('home.welcome')}>
        <p>{t('home.welcomeHint')}</p>
        <Link to="/settings/libraries">
          <Button variant="primary" className="mt-6">
            {t('home.addLibrary')}
          </Button>
        </Link>
      </EmptyState>
    )
  }

  if (isPending || !data) {
    return (
      <div className="space-y-8">
        <div className="skeleton -mx-4 -mt-4 aspect-[16/7] md:-mx-8 md:-mt-8" />
      </div>
    )
  }

  const featured = data.continueWatching[0] ?? data.recentlyAdded.find((i) => i.hasBackdrop) ?? data.recentlyAdded[0]

  return (
    <div className="space-y-10">
      {featured && (
        <Hero item={featured} onRemove={featured === data.continueWatching[0] ? () => hide.mutate(featured.id) : undefined} />
      )}
      {data.continueWatching.length > 0 && (
        <Row title={t('home.continueWatching')} more={rowLink('continueWatching')} hasMore={data.more?.continueWatching} wide>
          {data.continueWatching.map((it) => (
            <RowItem key={it.id} wide>
              <ContinueWatchingCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.recentlyAdded.length > 0 && (
        <Row title={t('home.recentlyAdded')} more={rowLink('recentlyAdded')} hasMore={data.more?.recentlyAdded}>
          {data.recentlyAdded.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {/* Continue watching, recently added and new releases stay at the top. */}
      {!!data.newReleases?.length && (
        <Row title={t('home.newReleases')} more={rowLink('newReleases')} hasMore={data.more?.newReleases}>
          {data.newReleases.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.watchList.length > 0 && (
        <Row title={t('nav.watchList')} more={rowLink('watchList')} hasMore={data.more?.watchList}>
          {data.watchList.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {/* A holiday coming up, pinned collections, then a few genres, different each day. */}
      {data.rows?.map((row) => (
        <Row key={row.id} title={rowTitle(row)} more={row.collectionId || row.more ? rowLink(row.id) : undefined} hasMore={row.more}>
          {row.items.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      ))}
      <ComingSoonRow />
      {data.recentlyAdded.length === 0 && (
        <EmptyState title={t('home.scanning')}>{t('home.scanningHint')}</EmptyState>
      )}
    </div>
  )
}

type HomeRows = Awaited<ReturnType<typeof api.home>>

// Takes an item off Continue Watching right away, putting it back if the
// server says no. Its place is kept for when it's played again.
function useHideFromContinueWatching() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.hideFromContinueWatching(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: ['home'] })
      const before = qc.getQueryData<HomeRows>(['home'])
      if (before) qc.setQueryData<HomeRows>(['home'], { ...before, continueWatching: before.continueWatching.filter((i) => i.id !== id) })
      return { before }
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.before) qc.setQueryData(['home'], ctx.before)
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ['home'] }),
  })
}

// A Continue Watching card with a button to take it off the row. The button
// shows all the time on touch screens, and on hover with a mouse.
function ContinueWatchingCard({ item }: { item: Item }) {
  const { t } = useTranslation()
  const hide = useHideFromContinueWatching()
  return (
    <div className="group/cw relative">
      <PosterCard item={item} wide />
      <button
        onClick={() => hide.mutate(item.id)}
        className="absolute top-1 end-1 grid size-11 place-items-center rounded-full transition-opacity focus:opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/cw:opacity-100"
        aria-label={t('home.removeNamed', { title: item.showTitle || item.title })}
        title={t('home.remove')}
      >
        <span className="grid size-8 place-items-center rounded-full bg-black/60 text-white/90 backdrop-blur hover:bg-black/80 hover:text-white">
          <X className="size-4" />
        </span>
      </button>
    </div>
  )
}
