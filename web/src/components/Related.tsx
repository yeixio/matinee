import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type ItemDetail } from '../api'
import { PosterCard } from './Card'
import { boxSetLink } from './Collections'
import { Row, RowItem } from './Row'

// The movie's collection, then other titles in the library like it.
export function RelatedRows({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const enabled = item.kind === 'movie' || item.kind === 'show'
  const { data } = useQuery({ queryKey: ['related', item.id], queryFn: () => api.related(item.id), enabled })
  if (!enabled || !data) return null
  return (
    <>
      {data.collection && (
        <Row title={data.collection.name} more={boxSetLink(data.collection.id)}>
          {data.collection.items.map((it) => (
            <RowItem key={it.id}>
              <div className={it.id === item.id ? 'opacity-50' : ''}>
                <PosterCard item={it} />
              </div>
            </RowItem>
          ))}
        </Row>
      )}
      {data.items.length > 0 && (
        <Row title={t('related.moreLikeThis')}>
          {data.items.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
    </>
  )
}
