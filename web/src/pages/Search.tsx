import { useQuery } from '@tanstack/react-query'
import { Search as SearchIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useRequestOptions } from '../components/Arr'
import { PosterGrid } from '../components/Card'
import { EmptyState } from '../components/ui'

export default function Search() {
  const { data: opts } = useRequestOptions()
  const [params, setParams] = useSearchParams()
  const [text, setText] = useState(params.get('q') ?? '')
  const [q, setQ] = useState(text)

  // Search as you type, after a short pause.
  useEffect(() => {
    const t = window.setTimeout(() => {
      setQ(text.trim())
      setParams(text.trim() ? { q: text.trim() } : {}, { replace: true })
    }, 150)
    return () => window.clearTimeout(t)
  }, [text, setParams])

  const { data, isFetching } = useQuery({
    queryKey: ['search', q],
    queryFn: () => api.search(q),
    enabled: q.length > 0,
    placeholderData: (prev) => prev,
  })

  return (
    <div className="space-y-6">
      <div className="relative max-w-2xl">
        <SearchIcon className="absolute top-1/2 left-4 size-5 -translate-y-1/2 text-faint" />
        <input
          autoFocus
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Movies, shows, artists, albums…"
          className="h-14 w-full rounded-xl border border-line bg-surface pr-4 pl-12 text-lg placeholder:text-faint focus:border-accent focus:outline-none"
        />
        {isFetching && <div className="absolute right-4 top-1/2 size-2 -translate-y-1/2 animate-pulse rounded-full bg-accent" />}
      </div>
      {q && data && (data.length > 0 ? <PosterGrid items={data} /> : <EmptyState title={`Nothing matches "${q}"`} />)}
      {/* Not in the library? Ask for it. */}
      {q && data && opts?.discover ? (
        <p className="text-center text-sm text-muted">
          Looking for something new?{' '}
          <Link to={`/discover?q=${encodeURIComponent(q)}`} className="text-accent hover:text-accent-strong">
            Find it in Discover
          </Link>
        </p>
      ) : (
        q &&
        data &&
        opts?.canRequest && (
        <p className="text-center text-sm text-muted">
          Looking for something new?{' '}
          <Link to={`/requests?q=${encodeURIComponent(q)}`} className="text-accent hover:text-accent-strong">
            {opts.autoApprove ? 'Add it' : 'Ask for it'}
          </Link>
        </p>
        )
      )}
    </div>
  )
}
