import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api'
import { PosterCard } from '../components/Card'
import { PersonPhoto } from '../components/Credits'
import { EmptyState, Spinner } from '../components/ui'
import { lifeLabel } from '../people'

export default function PersonPage() {
  const id = Number(useParams().id)
  const { data: p, isPending, isError } = useQuery({ queryKey: ['person', id], queryFn: () => api.person(id) })
  const [more, setMore] = useState(false)

  if (isError) return <EmptyState title="Person not found" />
  if (isPending || !p) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  const life = lifeLabel(p)
  const long = (p.biography?.length ?? 0) > 600

  return (
    <div className="animate-fade space-y-10">
      <section className="flex flex-col items-center gap-6 pt-4 text-center sm:flex-row sm:items-start sm:text-left md:gap-10 md:pt-8">
        <PersonPhoto id={p.id} name={p.name} hasPhoto={p.hasPhoto} className="aspect-square w-40 shrink-0 shadow-2xl md:w-52" />
        <div className="min-w-0 flex-1 space-y-3">
          <h1 className="text-3xl font-semibold md:text-5xl">{p.name}</h1>
          <div className="flex flex-wrap justify-center gap-x-4 gap-y-1 text-sm text-muted sm:justify-start">
            {p.knownFor && <span>{p.knownFor === 'Acting' ? 'Actor' : p.knownFor}</span>}
            {life && <span>{life}</span>}
          </div>
          {p.biography && (
            <div className="max-w-3xl">
              <p className={`leading-relaxed whitespace-pre-line text-text/85 ${long && !more ? 'line-clamp-6' : ''}`}>{p.biography}</p>
              {long && (
                <button onClick={() => setMore(!more)} className="mt-1 text-sm text-accent hover:text-accent-strong">
                  {more ? 'Show less' : 'Read more'}
                </button>
              )}
            </div>
          )}
        </div>
      </section>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">In your library</h2>
        <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))' }}>
          {p.titles.map(({ item, roles }) => (
            <div key={item.id}>
              <PosterCard item={item} />
              {roles.length > 0 && <div className="mt-0.5 line-clamp-2 px-0.5 text-xs text-faint">{roles.join(', ')}</div>}
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}
