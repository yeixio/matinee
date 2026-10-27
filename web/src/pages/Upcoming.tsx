import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Upcoming as UpcomingItem, type User } from '../api'
import { dayLabel, RemotePoster, releaseLabel, upcomingTitle, useRequestOptions } from '../components/Arr'
import { Button, EmptyState, Spinner } from '../components/ui'
import { episodeCode } from '../format'
import { formatList, formatLocale } from '../i18n'

// Coming soon: what Radarr and Sonarr are waiting for, by day.
export default function Upcoming() {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({
    queryKey: ['me'],
    queryFn: api.me,
    staleTime: Infinity,
  })
  const { data: opts } = useRequestOptions()
  const connected = !!opts && (opts.movies || opts.shows)
  const { data, isPending } = useQuery({
    queryKey: ['upcoming', 60],
    queryFn: () => api.upcoming(60),
    enabled: connected,
    staleTime: 5 * 60_000,
  })

  if (opts && !connected) {
    return (
      <EmptyState title={t('upcoming.nothingYet')}>
        {me?.isAdmin ? (
          <>
            <p>{t('upcoming.connect')}</p>
            <Link to="/settings">
              <Button variant="primary" className="mt-6">
                {t('upcoming.openSettings')}
              </Button>
            </Link>
          </>
        ) : (
          <p>{t('upcoming.notConnected')}</p>
        )}
      </EmptyState>
    )
  }
  if (!opts || isPending || !data) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }

  const days: { label: string; items: UpcomingItem[] }[] = []
  for (const u of data.items) {
    const label = dayLabel(new Date(u.date))
    if (days.at(-1)?.label !== label) days.push({ label, items: [] })
    days.at(-1)!.items.push(u)
  }

  return (
    <div className="max-w-3xl space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t('nav.comingSoon')}</h1>
        <p className="mt-1 text-sm text-muted">{t('upcoming.intro')}</p>
        {data.unavailable && (
          <p className="mt-2 text-sm text-danger">{t('upcoming.unavailable', { apps: formatList(data.unavailable) })}</p>
        )}
      </div>
      {days.length === 0 && <p className="text-muted">{t('upcoming.nothing')}</p>}
      {days.map((d) => (
        <section key={d.label} className="space-y-2">
          <h2 className="sticky top-14 z-10 -mx-4 bg-ink/90 px-4 py-2 text-sm font-semibold tracking-wide text-accent uppercase backdrop-blur md:top-16 md:-mx-8 md:px-8">
            {d.label}
          </h2>
          {d.items.map((u, i) => (
            <div key={i} className="flex gap-4 rounded-xl p-2">
              <RemotePoster src={u.poster} title={upcomingTitle(u)} className="w-16 shrink-0 rounded-lg md:w-20" label={false} />
              <div className="min-w-0 flex-1 py-1">
                <div className="truncate font-medium">{upcomingTitle(u)}</div>
                <div className="mt-0.5 text-sm text-muted">
                  {u.kind === 'episode' ? (
                    <>
                      {episodeCode(u.season, u.episode)}
                      {u.title && ` · ${u.title}`} ·{' '}
                      {new Date(u.date).toLocaleTimeString(formatLocale(), {
                        hour: 'numeric',
                        minute: '2-digit',
                      })}
                    </>
                  ) : (
                    <>
                      {releaseLabel(u.release)}
                      {u.year ? ` · ${u.year}` : ''}
                    </>
                  )}
                </div>
                {u.overview && <p className="mt-1 line-clamp-2 text-sm text-faint">{u.overview}</p>}
              </div>
            </div>
          ))}
        </section>
      ))}
    </div>
  )
}
