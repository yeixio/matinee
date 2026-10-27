import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { PosterGrid } from '../components/Card'
import { Button, EmptyState, Spinner } from '../components/ui'

export default function WatchList() {
  const { t } = useTranslation()
  const { data, isPending } = useQuery({
    queryKey: ['watchlist'],
    queryFn: api.watchList,
    refetchInterval: (q) => pendingRefetch(q.state.data),
  })

  if (isPending || !data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t('nav.watchList')}</h1>
      {data.length > 0 ? (
        <PosterGrid items={data} />
      ) : (
        <EmptyState title={t('watchList.empty')}>
          <p>{t('watchList.emptyHint')}</p>
          <Link to="/">
            <Button className="mt-6">{t('watchList.browse')}</Button>
          </Link>
        </EmptyState>
      )}
    </div>
  )
}
