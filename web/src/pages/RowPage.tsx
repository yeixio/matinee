import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { PosterGrid } from '../components/Card'
import { EmptyState, Spinner } from '../components/ui'
import { rowTitle } from '../rows'

// Every title in one of the rows, such as a genre's or Recently added, for
// the See more at the end of the row.
export default function RowPage() {
  const { t } = useTranslation()
  const id = useParams().id ?? ''
  const { data, error } = useQuery({
    queryKey: ['row', id],
    queryFn: () => api.row(id),
    refetchInterval: (q) => pendingRefetch(q.state.data?.items),
  })

  if (error) return <EmptyState title={t('common:rows.unavailable')}>{error.message}</EmptyState>
  if (!data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{rowTitle(data)}</h1>
      {data.items.length > 0 ? (
        <PosterGrid items={data.items} wide={id === 'continueWatching'} />
      ) : (
        <EmptyState title={t('common:rows.empty')} />
      )}
    </div>
  )
}
