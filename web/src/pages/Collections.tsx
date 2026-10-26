import { useQuery } from '@tanstack/react-query'
import { Plus, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type User } from '../api'
import { boxSetLink, CardGrid, CollectionCard, CollectionEditor, collectionLink, Mosaic } from '../components/Collections'
import { Button, EmptyState, Spinner } from '../components/ui'

export default function Collections() {
  const navigate = useNavigate()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data, isPending } = useQuery({ queryKey: ['collections'], queryFn: () => api.collections() })
  const [making, setMaking] = useState<'smart' | 'manual'>()

  if (isPending || !data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  const mine = data.collections.filter((c) => !c.shared)
  const shared = data.collections.filter((c) => c.shared)
  const empty = data.collections.length === 0 && data.boxSets.length === 0

  return (
    <div className="space-y-10">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-bold tracking-tight">Collections</h1>
        {!me?.guest && (
          <div className="ml-auto flex gap-2">
            <Button onClick={() => setMaking('manual')} icon={<Plus className="size-4" />}>
              New collection
            </Button>
            <Button variant="primary" onClick={() => setMaking('smart')} icon={<Sparkles className="size-4" />}>
              New smart collection
            </Button>
          </div>
        )}
      </div>
      {empty && (
        <EmptyState title="No collections yet">
          A smart collection fills itself from rules, like unwatched 80s action. Box sets show up here once two of their titles are in
          a library.
        </EmptyState>
      )}
      {mine.length > 0 && (
        <Section title="Yours">
          {mine.map((c) => (
            <CollectionCard key={c.id} c={c} />
          ))}
        </Section>
      )}
      {shared.length > 0 && (
        <Section title={mine.length > 0 ? 'Shared' : 'Collections for everyone'}>
          {shared.map((c) => (
            <CollectionCard key={c.id} c={c} />
          ))}
        </Section>
      )}
      {data.boxSets.length > 0 && (
        <Section title="Box sets">
          {data.boxSets.map((b) => (
            <Link key={b.id} to={boxSetLink(b.id)} className="group block animate-fade">
              <Mosaic ids={b.itemIds} title={b.name} />
              <div className="mt-2 px-0.5">
                <div className="truncate text-sm font-medium text-text group-hover:text-accent-strong">{b.name}</div>
                <div className="truncate text-xs text-muted">{b.count} titles</div>
              </div>
            </Link>
          ))}
        </Section>
      )}
      {making && <CollectionEditor kind={making} onClose={() => setMaking(undefined)} onSaved={(c) => navigate(collectionLink(c))} />}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <CardGrid>{children}</CardGrid>
    </section>
  )
}
