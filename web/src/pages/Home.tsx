import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, pendingRefetch, type Item } from '../api'
import { PosterCard } from '../components/Card'
import { Hero } from '../components/Hero'
import { Row, RowItem } from '../components/Row'
import { useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'

export default function Home() {
  const { data: libraries } = useLibraries()
  const hide = useHideFromContinueWatching()
  const { data, isPending } = useQuery({
    queryKey: ['home'],
    queryFn: api.home,
    refetchInterval: (q) => pendingRefetch(q.state.data?.recentlyAdded),
  })

  if (libraries && libraries.length === 0) {
    return (
      <EmptyState title="Welcome to Matinee">
        <p>Add a folder of movies, shows or music to get started.</p>
        <Link to="/settings">
          <Button variant="primary" className="mt-6">
            Add a library
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
        <Row title="Continue watching">
          {data.continueWatching.map((it) => (
            <RowItem key={it.id} wide>
              <ContinueWatchingCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.watchList.length > 0 && (
        <Row title="Watch list" more="/watchlist">
          {data.watchList.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.recentlyAdded.length > 0 && (
        <Row title="Recently added">
          {data.recentlyAdded.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.recentlyAdded.length === 0 && (
        <EmptyState title="Scanning your library">New titles show up here as soon as they're found.</EmptyState>
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
  const hide = useHideFromContinueWatching()
  return (
    <div className="group/cw relative">
      <PosterCard item={item} wide />
      <button
        onClick={() => hide.mutate(item.id)}
        className="absolute top-1 right-1 grid size-11 place-items-center rounded-full transition-opacity focus:opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/cw:opacity-100"
        aria-label={`Remove ${item.showTitle || item.title} from Continue watching`}
        title="Remove from Continue watching"
      >
        <span className="grid size-8 place-items-center rounded-full bg-black/60 text-white/90 backdrop-blur hover:bg-black/80 hover:text-white">
          <X className="size-4" />
        </span>
      </button>
    </div>
  )
}
