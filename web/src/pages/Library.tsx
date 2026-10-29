import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch, type Library as LibraryInfo } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { PinnedCollections, SaveAsCollectionButton } from '../components/Collections'
import { FilterBar } from '../components/FilterBar'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'
import Music from './Music'
import { activeCount, clearPicks, toSearchParams, useLibraryFilter } from '../filter'

export default function Library() {
  const id = Number(useParams().id)
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === id)
  // Music has its own page: artists, albums and songs.
  if (lib?.kind === 'music') return <Music lib={lib} />
  return <Titles id={id} lib={lib} />
}

function Titles({ id, lib }: { id: number; lib?: LibraryInfo }) {
  const { t } = useTranslation()
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
        <h1 className="text-2xl font-bold tracking-tight">{lib?.name ?? t('library.library')}</h1>
        {items && (
          <span className={`text-sm text-muted transition-opacity ${isPlaceholderData ? 'opacity-50' : ''}`}>
            {filtered ? t('library.matches', { count: items.length }) : items.length}
          </span>
        )}
        {lib?.scanning && <span className="animate-pulse text-sm text-accent">{t('library.scanning')}</span>}
        {filtered && (
          <div className="ms-auto">
            <SaveAsCollectionButton filter={filter} library={id} />
          </div>
        )}
      </div>
      {/* Collections pinned to this library, while no filter is on. */}
      {!filtered && <PinnedCollections library={id} />}
      <FilterBar value={filter} onChange={setFilter} libraryId={id} libraryKind={lib?.kind} />
      {isPending ? (
        <GridSkeleton />
      ) : items && items.length > 0 ? (
        <div className={`transition-opacity ${isPlaceholderData ? 'opacity-60' : ''}`}>
          <PosterGrid items={items} />
        </div>
      ) : filtered ? (
        <EmptyState title={onlyUnwatched ? t('library.nothingUnwatched') : t('library.nothingMatches')}>
          {onlyUnwatched ? (
            t('library.seenEverything')
          ) : (
            <Button className="mt-2" onClick={() => setFilter(clearPicks(filter))}>
              {t('library.clearFilters')}
            </Button>
          )}
        </EmptyState>
      ) : (
        <EmptyState title={lib?.scanning ? t('library.scanning') : t('nothingHereYet')}>
          {lib?.scanning ? t('library.appearAsFound') : t('library.noMedia', { paths: lib?.paths.join(', ') })}
        </EmptyState>
      )}
    </div>
  )
}
