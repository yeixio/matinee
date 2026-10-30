import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Check, ChevronLeft, Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, pendingRefetch, type BookGroup, type Item, type Library } from '../api'
import { Artwork } from '../components/Artwork'
import { GridSkeleton } from '../components/Card'
import { LibraryIcon } from '../components/Shell'
import { Row, RowItem } from '../components/Row'
import { ViewTabs } from '../components/ViewTabs'
import { EmptyState, Spinner } from '../components/ui'
import type { ItemFilter, SortField } from '../filter'
import { duration, progressFraction } from '../format'
import { useAudio } from '../player/AudioPlayer'
import { seriesLabel } from './BookItem'

type View = 'overview' | 'books' | 'authors' | 'series' | 'narrators'
const views: View[] = ['overview', 'books', 'authors', 'series', 'narrators']

const sorts: Record<'books' | 'authors', SortField[]> = {
  books: ['title', 'added', 'lastPlayed', 'year', 'runtime'],
  authors: ['title', 'added', 'lastPlayed'],
}

// A link to a series' or a narrator's books.
export const groupLink = (library: number, view: 'series' | 'narrators', name: string) =>
  `/library/${library}?${new URLSearchParams({ view, name })}`

// An audiobook library: what's being listened to and what's new, then every
// book, author, series or narrator.
export default function Books({ lib }: { lib: Library }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const view = (views.includes(params.get('view') as View) ? params.get('view') : 'overview') as View
  const name = params.get('name')
  const pick = (v: View) => setParams(v === 'overview' ? {} : { view: v }, { replace: true })

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <LibraryIcon kind={lib.kind} className="size-6 text-accent" />
        <h1 className="text-2xl font-bold tracking-tight">{lib.name}</h1>
        {lib.scanning && <span className="animate-pulse text-sm text-accent">{t('library.scanning')}</span>}
      </div>
      <ViewTabs views={views} view={view} label={(v) => t(`common:books.${v}`)} onPick={pick} />
      {view === 'overview' ? (
        <Overview lib={lib} onMore={pick} />
      ) : (view === 'series' || view === 'narrators') && name ? (
        <GroupBooks lib={lib} view={view} name={name} />
      ) : view === 'series' || view === 'narrators' ? (
        <Groups lib={lib} view={view} />
      ) : (
        <All lib={lib} view={view} />
      )}
    </div>
  )
}

function useBooks(lib: Library, filter: ItemFilter, key: string) {
  return useQuery({
    queryKey: ['books', lib.id, key, filter],
    queryFn: () => api.libraryItems(lib.id, filter),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (lib.scanning ? 3000 : pendingRefetch(q.state.data)),
  })
}

function Overview({ lib, onMore }: { lib: Library; onMore: (v: View) => void }) {
  const { t } = useTranslation()
  const listening = useBooks(lib, { kind: ['book'], watched: 'inProgress', sort: 'lastPlayed', limit: 12 }, 'listening')
  const added = useBooks(lib, { kind: ['book'], sort: 'added', limit: 12 }, 'added')
  const authors = useBooks(lib, { kind: ['author'], sort: 'lastPlayed', limit: 12 }, 'authors')
  if (added.isPending) return <GridSkeleton />
  if (!added.data?.length) {
    return (
      <EmptyState title={lib.scanning ? t('library.scanning') : t('nothingHereYet')}>
        {lib.scanning ? t('library.appearAsFound') : t('library.noMedia', { paths: lib.paths.join(', ') })}
      </EmptyState>
    )
  }
  return (
    <div className="space-y-10">
      {!!listening.data?.length && (
        <Row title={t('common:books.continueListening')}>
          {listening.data.map((b) => (
            <RowItem key={b.id}>
              <BookCard book={b} />
            </RowItem>
          ))}
        </Row>
      )}
      <Row title={t('common:music.recentlyAdded')}>
        {added.data.map((b) => (
          <RowItem key={b.id}>
            <BookCard book={b} />
          </RowItem>
        ))}
      </Row>
      {!!authors.data?.length && (
        <section>
          <button onClick={() => onMore('authors')} className="mb-3 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {t('common:books.authors')}
          </button>
          <div className="scrollbar-none -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8">
            {authors.data.map((a) => (
              <div key={a.id} className="w-28 shrink-0 md:w-36">
                <AuthorCard author={a} />
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function All({ lib, view }: { lib: Library; view: 'books' | 'authors' }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const sort = (sorts[view].includes(params.get('sort') as SortField) ? params.get('sort') : sorts[view][0]) as SortField
  const items = useBooks(lib, { kind: [view === 'books' ? 'book' : 'author'], sort }, view)
  const setSort = (s: SortField) => setParams({ view, ...(s === sorts[view][0] ? {} : { sort: s }) }, { replace: true })
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <label className="ms-auto flex items-center gap-2 text-sm text-muted">
          {t('filters.sortBy')}
          <select
            value={sort}
            onChange={(e) => setSort(e.target.value as SortField)}
            className="rounded-lg border border-line bg-surface px-2 py-1.5 text-text focus:border-accent focus:outline-none"
          >
            {sorts[view].map((s) => (
              <option key={s} value={s}>
                {t(s === 'runtime' ? 'common:books.length' : `common:sort.${s}`)}
              </option>
            ))}
          </select>
        </label>
      </div>
      {items.isPending ? (
        <GridSkeleton />
      ) : !items.data?.length ? (
        <EmptyState title={t('nothingHereYet')} />
      ) : (
        <BookGrid>{items.data.map((it) => (view === 'authors' ? <AuthorCard key={it.id} author={it} /> : <BookCard key={it.id} book={it} />))}</BookGrid>
      )}
    </div>
  )
}

export function BookGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(150px, 1fr))' }}>
      {children}
    </div>
  )
}

// Every series, or every narrator, with a few covers each.
function Groups({ lib, view }: { lib: Library; view: 'series' | 'narrators' }) {
  const { t } = useTranslation()
  const groups = useQuery({
    queryKey: ['bookGroups', lib.id, view],
    queryFn: () => (view === 'series' ? api.series(lib.id) : api.narrators(lib.id)),
  })
  if (groups.isPending) return <GridSkeleton />
  if (!groups.data?.length) {
    return <EmptyState title={t(view === 'series' ? 'common:books.noSeries' : 'common:books.noNarrators')}>{t('common:books.fromTags')}</EmptyState>
  }
  return (
    <BookGrid>
      {groups.data.map((g) => (
        <GroupCard key={g.name} group={g} to={groupLink(lib.id, view, g.name)} />
      ))}
    </BookGrid>
  )
}

// A series' books in reading order, or a narrator's books.
function GroupBooks({ lib, view, name }: { lib: Library; view: 'series' | 'narrators'; name: string }) {
  const { t } = useTranslation()
  const filter: ItemFilter = view === 'series' ? { kind: ['book'], series: [name], sort: 'series' } : { kind: ['book'], narrator: [name] }
  const books = useBooks(lib, filter, `${view}:${name}`)
  const total = books.data?.reduce((sum, b) => sum + (b.durationMs ?? 0), 0)
  return (
    <div className="space-y-5">
      <div>
        <Link to={`/library/${lib.id}?view=${view}`} className="inline-flex items-center gap-1 text-sm text-muted hover:text-text">
          <ChevronLeft className="size-4 rtl:rotate-180" />
          {t(`common:books.${view}`)}
        </Link>
        <h2 className="mt-1 text-2xl font-semibold tracking-tight">{view === 'series' ? name : t('common:books.narratedBy', { names: name })}</h2>
        {books.data && (
          <p className="text-sm text-muted">
            {t('common:count.books', { count: books.data.length })}
            {!!total && ` · ${duration(total)}`}
          </p>
        )}
      </div>
      {books.isPending ? (
        <Spinner />
      ) : (
        <BookGrid>
          {books.data?.map((b) => (
            <BookCard key={b.id} book={b} sub={view === 'series' ? seriesLabel(b, false) : undefined} />
          ))}
        </BookGrid>
      )}
    </div>
  )
}

// A book's cover, with how far along it is and a button that picks up
// where the person left off.
export function BookCard({ book, sub }: { book: Item; sub?: string }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const frac = progressFraction(book.positionMs, book.durationMs)
  const play = (e: React.MouseEvent) => {
    e.preventDefault()
    audio.playBook(book.id)
  }
  return (
    <div className="group relative animate-fade">
      <Link to={`/item/${book.id}`} className="block">
        <div className="relative">
          <Artwork item={book} type="poster" className="aspect-square rounded-lg shadow-md ring-1 ring-text/5 transition duration-200 group-hover:brightness-110" />
          {book.played && (
            <span className="absolute end-2 top-2 grid size-6 place-items-center rounded-full bg-accent text-on-accent shadow" title={t('common:books.finished')}>
              <Check className="size-4" strokeWidth={3} />
            </span>
          )}
          {frac > 0 && (
            <div className="absolute inset-x-2 bottom-2 h-1 overflow-hidden rounded-full bg-black/60">
              <div className="h-full bg-accent" style={{ width: `${frac * 100}%` }} />
            </div>
          )}
        </div>
        <div className="mt-2 px-0.5">
          <div className="truncate text-sm font-medium group-hover:text-accent-strong">{book.title}</div>
          <div className="truncate text-xs text-muted">{sub ?? (book.author || ' ')}</div>
        </div>
      </Link>
      <button
        onClick={play}
        aria-label={`${t('item.play')} ${book.title}`}
        className="absolute end-2 top-[calc(100%-4.5rem)] grid size-11 translate-y-1 place-items-center rounded-full bg-accent text-on-accent opacity-0 shadow-xl transition group-hover:translate-y-0 group-hover:opacity-100 focus:translate-y-0 focus:opacity-100"
      >
        <Play className="size-5 translate-x-0.5 fill-current" />
      </button>
    </div>
  )
}

// An author's photo, round.
export function AuthorCard({ author }: { author: Item }) {
  const { t } = useTranslation()
  return (
    <Link to={`/item/${author.id}`} className="group block animate-fade text-center">
      <Artwork item={author} type="poster" className="aspect-square rounded-full shadow-md ring-1 ring-text/5 transition duration-200 group-hover:ring-accent/70" />
      <div className="mt-2 truncate text-sm font-medium group-hover:text-accent-strong">{author.title}</div>
      <div className="truncate text-xs text-muted">{author.childCount ? t('common:count.books', { count: author.childCount }) : ' '}</div>
    </Link>
  )
}

// A series or a narrator: its first covers fanned out, its name and how many books.
function GroupCard({ group, to }: { group: BookGroup; to: string }) {
  const { t } = useTranslation()
  const covers = group.itemIds.slice(0, 3)
  return (
    <Link to={to} className="group block animate-fade">
      <div className="relative aspect-square">
        {covers
          .map((id, i) => (
            <div
              key={id}
              className="absolute size-[78%] overflow-hidden rounded-lg shadow-lg ring-1 ring-text/10 transition duration-200 group-hover:brightness-110"
              style={{ insetInlineStart: `${i * 11}%`, top: `${(covers.length - 1 - i) * 11}%`, zIndex: i }}
            >
              <Artwork item={{ id, title: group.name, kind: 'book', hasPoster: true } as Item} type="poster" label={false} className="size-full" />
            </div>
          ))
          .reverse()}
      </div>
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium group-hover:text-accent-strong">{group.name}</div>
        <div className="truncate text-xs text-muted">{t('common:count.books', { count: group.books })}</div>
      </div>
    </Link>
  )
}
