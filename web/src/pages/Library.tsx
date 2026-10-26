import { useQuery } from '@tanstack/react-query'
import { ArrowDownAZ, Clock } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { LibraryIcon, useLibraries } from '../components/Shell'
import { EmptyState } from '../components/ui'
import { filterTags } from '../media'

export default function Library() {
  const id = Number(useParams().id)
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === id)
  const { data: items, isPending } = useQuery({
    queryKey: ['library', id],
    queryFn: () => api.libraryItems(id),
    refetchInterval: (q) => (lib?.scanning ? 3000 : pendingRefetch(q.state.data)),
  })
  const [sort, setSort] = useState<'title' | 'added'>('title')
  // Quality filters, offered only for tags some item in the library has.
  const [filters, setFilters] = useState<string[]>([])
  const available = useMemo(() => filterTags.filter((f) => items?.some((i) => i.mediaTags?.includes(f.tag))), [items])
  const sorted = useMemo(() => {
    const kept = items?.filter((i) => filters.every((f) => i.mediaTags?.includes(f)))
    return sort === 'title' ? kept : kept && [...kept].sort((a, b) => b.addedAt - a.addedAt)
  }, [items, sort, filters])
  const toggle = (tag: string) => setFilters((f) => (f.includes(tag) ? f.filter((t) => t !== tag) : [...f, tag]))

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        {lib && <LibraryIcon kind={lib.kind} className="size-6 text-accent" />}
        <h1 className="text-2xl font-bold tracking-tight">{lib?.name ?? 'Library'}</h1>
        {sorted && <span className="text-sm text-muted">{filters.length > 0 && items ? `${sorted.length} of ${items.length}` : sorted.length}</span>}
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
      {available.length > 0 && (
        <div className="-mt-2 flex flex-wrap gap-2" role="group" aria-label="Filter by quality">
          {available.map((f) => (
            <button
              key={f.tag}
              onClick={() => toggle(f.tag)}
              aria-pressed={filters.includes(f.tag)}
              className={`rounded-full border px-3 py-1 text-sm transition-colors ${
                filters.includes(f.tag) ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint hover:text-text'
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      )}
      {isPending ? (
        <GridSkeleton />
      ) : sorted && sorted.length > 0 ? (
        <PosterGrid items={sorted} />
      ) : filters.length > 0 ? (
        <EmptyState title="Nothing matches">Try fewer filters.</EmptyState>
      ) : (
        <EmptyState title={lib?.scanning ? 'Scanning…' : 'Nothing here yet'}>
          {lib?.scanning ? 'Titles appear as they are found.' : `Matinee looked in ${lib?.paths.join(', ')} and found no media.`}
        </EmptyState>
      )}
    </div>
  )
}
