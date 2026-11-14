import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch, type Library as LibraryInfo } from '../api'
import { AlphaBar } from '../components/AlphaBar'
import { GridSkeleton, type JumpTo, PosterGrid } from '../components/Card'
import { PinnedCollections, SaveAsCollectionButton } from '../components/Collections'
import { FilterBar } from '../components/FilterBar'
import { SkippedFiles } from '../components/SkippedFiles'
import { PCGames } from '../games/PCGames'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'
import Books from './Books'
import Music from './Music'
import Podcasts from './Podcasts'
import { activeCount, clearPicks, isDesc, toSearchParams, useLibraryFilter } from '../filter'

// Lists this long get the A to Z bar.
const LETTERS_FROM = 60

export default function Library() {
  const id = Number(useParams().id)
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === id)
  // Music has its own page: artists, albums and songs.
  if (lib?.kind === 'music') return <Music lib={lib} />
  // So do audiobooks: books, authors, series and narrators.
  if (lib?.kind === 'audiobooks') return <Books lib={lib} />
  // And podcasts: shows, the latest episodes, and finding shows to add.
  if (lib?.kind === 'podcasts') return <Podcasts lib={lib} />
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
  // Sorted by title, a long list gets letters to jump to.
  const byTitle = (filter.sort ?? 'title') === 'title'
  const { data: letters } = useQuery({
    queryKey: ['library-letters', id, toSearchParams(filter).toString()],
    queryFn: () => api.libraryLetters(id, filter),
    enabled: byTitle && (items?.length ?? 0) >= LETTERS_FROM,
    refetchInterval: lib?.scanning ? 3000 : false,
  })
  const jumpTo = useRef<JumpTo>(null)
  const showLetters = byTitle && !isPlaceholderData && (items?.length ?? 0) >= LETTERS_FROM && (letters?.length ?? 0) > 1
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
      <SkippedFiles skipped={lib?.skipped} />
      {/* Steam on the game host, when there is one, beside the library's own games. */}
      {lib?.kind === 'games' && !filtered && <PCGames />}
      {/* Collections pinned to this library, while no filter is on. */}
      {!filtered && <PinnedCollections library={id} />}
      <FilterBar value={filter} onChange={setFilter} libraryId={id} libraryKind={lib?.kind} />
      {isPending ? (
        <GridSkeleton />
      ) : items && items.length > 0 ? (
        <div className={`transition-opacity ${isPlaceholderData ? 'opacity-60' : ''} ${showLetters ? 'pe-4 md:pe-5' : ''}`}>
          <PosterGrid items={items} jumpRef={jumpTo} />
          {showLetters && letters && <AlphaBar letters={letters} desc={isDesc(filter)} onJump={(i) => jumpTo.current?.(i)} />}
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
