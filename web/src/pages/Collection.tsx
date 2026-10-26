import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Shuffle, Sparkles, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { CollectionEditor, invalidateCollections, PinBadges } from '../components/Collections'
import { Button, EmptyState } from '../components/ui'

export default function CollectionPage() {
  const id = useParams().id ?? ''
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [editing, setEditing] = useState(false)
  const { data: c, isPending, error } = useQuery({
    queryKey: ['collection', id],
    queryFn: () => api.collection(id),
    refetchInterval: (q) => pendingRefetch(q.state.data?.items),
  })
  const remove = useMutation({
    mutationFn: () => api.deleteCollection(id),
    onSuccess: () => {
      invalidateCollections(qc)
      navigate('/collections', { replace: true })
    },
  })

  if (error) return <EmptyState title="Collection not found">It may have been deleted.</EmptyState>
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        {c?.kind === 'smart' && <Sparkles className="size-6 text-accent" aria-label="Smart collection" />}
        <h1 className="text-2xl font-bold tracking-tight">{c?.name ?? 'Collection'}</h1>
        {c && <span className="text-sm text-muted">{c.count}</span>}
        {c?.sort === 'shuffle' && <Shuffle className="size-4 text-muted" aria-label="Shuffled daily" />}
        {c && <PinBadges c={c} />}
        {c?.canEdit && (
          <div className="ml-auto flex gap-2">
            <Button onClick={() => setEditing(true)} icon={<Pencil className="size-4" />}>
              {c.kind === 'smart' ? 'Edit rules' : 'Edit'}
            </Button>
            <Button
              variant="danger"
              aria-label="Delete collection"
              onClick={() => confirm(`Delete ${c.name}? The titles stay in your library.`) && remove.mutate()}
              icon={<Trash2 className="size-4" />}
            />
          </div>
        )}
      </div>
      {isPending || !c ? (
        <GridSkeleton />
      ) : c.items.length > 0 ? (
        <PosterGrid items={c.items} />
      ) : c.kind === 'smart' ? (
        <EmptyState title="Nothing matches yet">Titles join by themselves when they match the rules.</EmptyState>
      ) : (
        <EmptyState title="Nothing here yet">Open a movie or show and choose Collections to add it.</EmptyState>
      )}
      {editing && c && <CollectionEditor collection={c} onClose={() => setEditing(false)} />}
    </div>
  )
}
