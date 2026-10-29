import { useQuery } from '@tanstack/react-query'
import { Search as SearchIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Item } from '../api'
import { useRequestOptions } from '../components/Arr'
import { PosterGrid } from '../components/Card'
import { Tracks } from '../components/Tracks'
import { EmptyState } from '../components/ui'

export default function Search() {
  const { t: tr } = useTranslation()
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
        <SearchIcon className="absolute top-1/2 start-4 size-5 -translate-y-1/2 text-faint" />
        <input
          autoFocus
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={tr('search.placeholder')}
          className="h-14 w-full rounded-xl border border-line bg-surface pe-4 ps-12 text-lg placeholder:text-faint focus:border-accent focus:outline-none"
        />
        {isFetching && <div className="absolute end-4 top-1/2 size-2 -translate-y-1/2 animate-pulse rounded-full bg-accent" />}
      </div>
      {q && data && (data.length > 0 ? <Results items={data} /> : <EmptyState title={tr('search.nothing', { q })} />)}
      {/* Not in the library? Ask for it. */}
      {q && data && opts?.discover ? (
        <p className="text-center text-sm text-muted">
          {tr('search.lookingForNew')}{' '}
          <Link to={`/discover?q=${encodeURIComponent(q)}`} className="text-accent hover:text-accent-strong">
            {tr('discover.findIt')}
          </Link>
        </p>
      ) : (
        q &&
        data &&
        opts?.canRequest && (
        <p className="text-center text-sm text-muted">
          {tr('search.lookingForNew')}{' '}
          <Link to={`/requests?q=${encodeURIComponent(q)}`} className="text-accent hover:text-accent-strong">
            {opts.autoApprove ? tr('search.addIt') : tr('search.askForIt')}
          </Link>
        </p>
        )
      )}
    </div>
  )
}

// Songs are a list that plays from a click; everything else is posters.
function Results({ items }: { items: Item[] }) {
  const { t } = useTranslation()
  const songs = items.filter((i) => i.kind === 'track')
  const rest = items.filter((i) => i.kind !== 'track')
  if (!songs.length) return <PosterGrid items={items} />
  return (
    <div className="space-y-8">
      {rest.length > 0 && <PosterGrid items={rest} />}
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">{t('common:music.songs')}</h2>
        <Tracks items={songs} showArt showArtist showAlbum numbered={false} />
      </section>
    </div>
  )
}
