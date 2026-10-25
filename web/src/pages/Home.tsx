import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, pendingRefetch } from '../api'
import { PosterCard } from '../components/Card'
import { Hero } from '../components/Hero'
import { Row, RowItem } from '../components/Row'
import { useLibraries } from '../components/Shell'
import { Button, EmptyState } from '../components/ui'

export default function Home() {
  const { data: libraries } = useLibraries()
  const { data, isPending } = useQuery({
    queryKey: ['home'],
    queryFn: api.home,
    refetchInterval: (q) => pendingRefetch(q.state.data?.recentlyAdded),
  })

  if (libraries && libraries.length === 0) {
    return (
      <EmptyState title="Welcome to Matinee">
        <p>Add a folder of movies, shows or music to get started.</p>
        <Link to="/settings">
          <Button variant="primary" className="mt-6">
            Add a library
          </Button>
        </Link>
      </EmptyState>
    )
  }

  if (isPending || !data) {
    return (
      <div className="space-y-8">
        <div className="skeleton -mx-4 -mt-4 aspect-[16/7] md:-mx-8 md:-mt-8" />
      </div>
    )
  }

  const featured = data.continueWatching[0] ?? data.recentlyAdded.find((i) => i.hasBackdrop) ?? data.recentlyAdded[0]

  return (
    <div className="space-y-10">
      {featured && <Hero item={featured} />}
      {data.continueWatching.length > 0 && (
        <Row title="Continue watching">
          {data.continueWatching.map((it) => (
            <RowItem key={it.id} wide>
              <PosterCard item={it} wide />
            </RowItem>
          ))}
        </Row>
      )}
      {data.recentlyAdded.length > 0 && (
        <Row title="Recently added">
          {data.recentlyAdded.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      )}
      {data.recentlyAdded.length === 0 && (
        <EmptyState title="Scanning your library">New titles show up here as soon as they're found.</EmptyState>
      )}
    </div>
  )
}
