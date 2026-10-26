import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { EmptyState } from '../components/ui'

// One of TMDB's collections, such as a trilogy, filled in from metadata.
export default function BoxSetPage() {
  const id = Number(useParams().id)
  const { data, isPending, error } = useQuery({
    queryKey: ['box-set', id],
    queryFn: () => api.boxSet(id),
    refetchInterval: (q) => pendingRefetch(q.state.data?.items),
  })
  if (error) return <EmptyState title="Box set not found">None of its titles are in a library now.</EmptyState>
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-bold tracking-tight">{data?.name ?? 'Box set'}</h1>
        {data && <span className="text-sm text-muted">{data.items.length}</span>}
      </div>
      {isPending || !data ? <GridSkeleton /> : <PosterGrid items={data.items} />}
    </div>
  )
}
