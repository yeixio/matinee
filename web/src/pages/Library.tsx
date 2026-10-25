import { useQuery } from '@tanstack/react-query'
import { ArrowDownAZ, Clock } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { EmptyState } from '../components/ui'

export default function Library() {
  const id = Number(useParams().id)
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === id)
  const { data: items, isPending } = useQuery({
    queryKey: ['library', id],
    queryFn: () => api.libraryItems(id),
    refetchInterval: lib?.scanning ? 3000 : false,
  })
  const [sort, setSort] = useState<'title' | 'added'>('title')
  const sorted = useMemo(
    () => (sort === 'title' ? items : items && [...items].sort((a, b) => b.addedAt - a.addedAt)),
    [items, sort],
  )

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        {lib && <LibraryIcon kind={lib.kind} className="size-6 text-accent" />}
        <h1 className="text-2xl font-bold tracking-tight">{lib?.name ?? 'Library'}</h1>
        {items && <span className="text-sm text-muted">{items.length}</span>}
        {lib?.scanning && <span className="animate-pulse text-sm text-accent">Scanning…</span>}
        <div className="ml-auto flex rounded-lg bg-surface p-1 text-sm">
          <button
            onClick={() => setSort('title')}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 ${sort === 'title' ? 'bg-white/10 text-text' : 'text-muted'}`}
          >
            <ArrowDownAZ className="size-4" /> <span className="hidden sm:inline">Title</span>
          </button>
          <button
            onClick={() => setSort('added')}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 ${sort === 'added' ? 'bg-white/10 text-text' : 'text-muted'}`}
          >
            <Clock className="size-4" /> <span className="hidden sm:inline">Added</span>
          </button>
        </div>
      </div>
      {isPending ? (
        <GridSkeleton />
      ) : sorted && sorted.length > 0 ? (
        <PosterGrid items={sorted} />
      ) : (
        <EmptyState title={lib?.scanning ? 'Scanning…' : 'Nothing here yet'}>
          {lib?.scanning ? 'Titles appear as they are found.' : `Matinee looked in ${lib?.paths.join(', ')} and found no media.`}
        </EmptyState>
      )}
    </div>
  )
}
