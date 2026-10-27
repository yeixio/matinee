import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Shuffle, Sparkles, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { GridSkeleton, PosterGrid } from '../components/Card'
import { CollectionEditor, collectionName, invalidateCollections, PinBadges } from '../components/Collections'
import { Button, EmptyState } from '../components/ui'

export default function CollectionPage() {
  const { t } = useTranslation()
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

  if (error) return <EmptyState title={t('collection.notFound')}>{t('collection.notFoundHint')}</EmptyState>
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        {c?.kind === 'smart' && <Sparkles className="size-6 text-accent" aria-label={t('collection.smart')} />}
        <h1 className="text-2xl font-bold tracking-tight">{c ? collectionName(c) : t('collection.collection')}</h1>
        {c && <span className="text-sm text-muted">{c.count}</span>}
        {c?.sort === 'shuffle' && <Shuffle className="size-4 text-muted" aria-label={t('collection.shuffledDaily')} />}
        {c && <PinBadges c={c} />}
        {c?.canEdit && (
          <div className="ms-auto flex gap-2">
            <Button onClick={() => setEditing(true)} icon={<Pencil className="size-4" />}>
              {c.kind === 'smart' ? t('collection.editRules') : t('collection.edit')}
            </Button>
            <Button
              variant="danger"
              aria-label={t('collection.delete')}
              onClick={() => confirm(t('collection.deleteConfirm', { name: collectionName(c) })) && remove.mutate()}
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
        <EmptyState title={t('collection.nothingMatchesYet')}>{t('collection.joinByRules')}</EmptyState>
      ) : (
        <EmptyState title={t('nothingHereYet')}>{t('collection.howToAdd')}</EmptyState>
      )}
      {editing && c && <CollectionEditor collection={c} onClose={() => setEditing(false)} />}
    </div>
  )
}
