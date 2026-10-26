import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { FilterBar } from '../components/FilterBar'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'
import { activeCount, clearPicks, toSearchParams, useLibraryFilter } from '../filter'

export default function Library() {
  const id = Number(useParams().id)
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === id)
  const [filter, setFilter] = useLibraryFilter(id)
  const { data: items, isPending, isPlaceholderData } = useQuery({
    queryKey: ['library', id, toSearchParams(filter).toString()],
    queryFn: () => api.libraryItems(id, filter),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (lib?.scanning ? 3000 : pendingRefetch(q.state.data)),
  })
  const filtered = activeCount(filter) > 0
  const onlyUnwatched = filtered && activeCount(filter) === 1 && filter.watched === 'unwatched'

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        {lib && <LibraryIcon kind={lib.kind} className="size-6 text-accent" />}
        <h1 className="text-2xl font-bold tracking-tight">{lib?.name ?? 'Library'}</h1>
        {items && (
          <span className={`text-sm text-muted transition-opacity ${isPlaceholderData ? 'opacity-50' : ''}`}>
            {items.length} {filtered ? (items.length === 1 ? 'match' : 'matches') : ''}
          </span>
        )}
        {lib?.scanning && <span className="animate-pulse text-sm text-accent">Scanning…</span>}
      </div>
      <FilterBar value={filter} onChange={setFilter} libraryId={id} libraryKind={lib?.kind} />
      {isPending ? (
        <GridSkeleton />
      ) : items && items.length > 0 ? (
        <div className={`transition-opacity ${isPlaceholderData ? 'opacity-60' : ''}`}>
          <PosterGrid items={items} />
        </div>
      ) : filtered ? (
        <EmptyState title={onlyUnwatched ? 'Nothing unwatched here' : 'Nothing matches'}>
          {onlyUnwatched ? (
            'You’ve seen everything in this library.'
          ) : (
            <Button className="mt-2" onClick={() => setFilter(clearPicks(filter))}>
              Clear filters
            </Button>
          )}
        </EmptyState>
      ) : (
        <EmptyState title={lib?.scanning ? 'Scanning…' : 'Nothing here yet'}>
          {lib?.scanning ? 'Titles appear as they are found.' : `Matinee looked in ${lib?.paths.join(', ')} and found no media.`}
        </EmptyState>
      )}
    </div>
  )
}
