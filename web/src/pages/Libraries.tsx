import { useQuery } from '@tanstack/react-query'
import { Bookmark, CalendarClock, ChevronRight, Inbox, Layers, LogOut } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type User } from '../api'
import { useRequestOptions } from '../components/Arr'
import { LibraryIcon, useLibraries, useSignOut } from '../components/Shell'
import { EmptyState, Spinner } from '../components/ui'
import { isKids } from '../profiles'

// The phone tab bar's Libraries tab, as in the phone app: every library,
// then the other places the desktop bar links to.
export default function Libraries() {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data: libraries, isPending } = useLibraries()
  const kids = !!me && isKids(me)
  const { data: opts } = useRequestOptions(!kids)
  const arr = !kids && !!opts && (opts.movies || opts.shows)
  const waiting = kids ? 0 : (opts?.pendingRequests ?? 0) + (opts?.openIssues ?? 0)
  const signOut = useSignOut()

  if (isPending) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  return (
    <div className="mx-auto max-w-xl space-y-8">
      <h1 className="text-2xl font-bold tracking-tight">{t('nav.libraries')}</h1>
      {libraries?.length ? (
        <ul className="space-y-2.5">
          {libraries.map((l) => (
            <Place key={l.id} to={`/library/${l.id}`} icon={<LibraryIcon kind={l.kind} className="size-5" />}>
              {l.name}
              {l.scanning && <span className="size-1.5 animate-pulse rounded-full bg-accent" title={t('nav.scanning')} />}
            </Place>
          ))}
        </ul>
      ) : (
        <EmptyState title={t('settings.libraries.none')} />
      )}
      <ul className="space-y-2.5">
        <Place to="/watchlist" icon={<Bookmark className="size-5" />}>
          {t('nav.watchList')}
        </Place>
        <Place to="/collections" icon={<Layers className="size-5" />}>
          {t('nav.collections')}
        </Place>
        {arr && (
          <Place to="/upcoming" icon={<CalendarClock className="size-5" />}>
            {t('nav.comingSoon')}
          </Place>
        )}
        {(arr || waiting > 0) && !me?.guest && (
          <Place to="/requests" icon={<Inbox className="size-5" />}>
            {t('nav.requests')}
            {waiting > 0 && (
              <span className="grid h-5 min-w-5 place-items-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-on-accent">{waiting}</span>
            )}
          </Place>
        )}
      </ul>
      {me && !me.guest && !kids && (
        <button
          onClick={signOut}
          className="flex w-full items-center gap-3.5 rounded-2xl bg-surface p-4 text-start font-semibold text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <span className="grid size-11 place-items-center rounded-xl bg-raised">
            <LogOut className="size-5 rtl:-scale-x-100" />
          </span>
          {t('nav.signOut')}
        </button>
      )}
    </div>
  )
}

function Place({ to, icon, children }: { to: string; icon: ReactNode; children: ReactNode }) {
  return (
    <li>
      <Link to={to} className="flex items-center gap-3.5 rounded-2xl bg-surface p-4 transition-colors hover:bg-raised">
        <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-raised text-accent">{icon}</span>
        <span className="flex min-w-0 flex-1 items-center gap-2 truncate font-semibold">{children}</span>
        <ChevronRight className="size-4 shrink-0 text-faint rtl:-scale-x-100" />
      </Link>
    </li>
  )
}
