import { useQuery } from '@tanstack/react-query'
import { api, type ItemDetail } from '../api'
import { PosterCard } from './Card'
import { Row, RowItem } from './Row'

// The movie's collection, then other titles in the library like it.
export function RelatedRows({ item }: { item: ItemDetail }) {
  const enabled = item.kind === 'movie' || item.kind === 'show'
  const { data } = useQuery({ queryKey: ['related', item.id], queryFn: () => api.related(item.id), enabled })
  if (!enabled || !data) return null
  return (
    <>
      {data.collection && (
        <Row title={data.collection.name}>
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
        <Row title="More like this">
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
