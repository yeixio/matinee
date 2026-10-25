import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { PosterGrid } from '../components/Card'
import { Button, EmptyState, Spinner } from '../components/ui'

export default function WatchList() {
  const { data, isPending } = useQuery({
    queryKey: ['watchlist'],
    queryFn: api.watchList,
    refetchInterval: (q) => pendingRefetch(q.state.data),
  })

  if (isPending || !data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Watch list</h1>
      {data.length > 0 ? (
        <PosterGrid items={data} />
      ) : (
        <EmptyState title="Nothing saved yet">
          <p>Open a movie or show and choose Watch list to keep it here for later. Movies leave the list once you've watched them.</p>
          <Link to="/">
            <Button className="mt-6">Browse</Button>
          </Link>
        </EmptyState>
      )}
    </div>
  )
}
