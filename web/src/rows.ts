import type { HomeRow } from './api'
import { collectionLink, collectionName } from './components/Collections'
import i18n from './i18n'
import { genreRowTitle } from './people'

// Home's own rows, by the ids the server gives them, with their names.
const ownRows: Record<string, string> = {
  continueWatching: 'home.continueWatching',
  continueListening: 'common:books.continueListening',
  newEpisodes: 'common:podcasts.newEpisodes',
  recentlyAdded: 'home.recentlyAdded',
  newReleases: 'home.newReleases',
  watchList: 'nav.watchList',
}

// A row's name in the page's language.
export function rowTitle(row: Pick<HomeRow, 'id' | 'title' | 'genre' | 'holiday' | 'starter' | 'relatedTo'>): string {
  if (ownRows[row.id]) return i18n.t(ownRows[row.id])
  if (row.holiday) return i18n.t(`common:holidays.${row.holiday}`, { defaultValue: row.title })
  if (row.genre) return genreRowTitle(row.genre)
  if (row.relatedTo) return i18n.t('common:rows.moreLike', { title: row.relatedTo })
  return collectionName({ name: row.title, starter: row.starter })
}

// The page with all of a row's titles: the watch list's and collections'
// own pages, or the row's.
export function rowLink(id: string): string {
  if (id === 'watchList') return '/watchlist'
  if (id.startsWith('collection:')) return collectionLink({ id: id.slice('collection:'.length) })
  return `/row/${encodeURIComponent(id)}`
}
