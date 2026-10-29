import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Pause, Play, RotateCcw } from 'lucide-react'
import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Item, type ItemDetail } from '../api'
import { AddToCollectionButton } from '../components/Collections'
import { Equalizer } from '../components/Tracks'
import { Row, RowItem } from '../components/Row'
import { Button } from '../components/ui'
import { clock, duration } from '../format'
import i18n from '../i18n'
import { useAudio, useBookTime } from '../player/AudioPlayer'
import * as B from '../player/book'
import { BookCard, BookGrid, groupLink } from './Books'
import { Hero, Overview } from './MusicItem'

// "Book 2 of Night Watch", or "Book 2" in the series' own list.
export function seriesLabel(b: Item, withName = true): string {
  if (!b.series) return ''
  if (b.seriesIndex === undefined) return withName ? b.series : ''
  const n = b.seriesIndex.toLocaleString(i18n.language)
  return withName ? i18n.t('common:books.bookOf', { n, series: b.series }) : i18n.t('common:books.bookN', { n })
}

// An audiobook: its cover, who wrote and reads it, a button that picks up
// where the person left off, its chapters, and more in the series and by
// the author.
export function BookPage({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const qc = useQueryClient()
  const book = useQuery({ queryKey: ['book', item.id, item.positionMs, item.played], queryFn: () => api.book(item.id) })
  const playingThis = audio.book?.id === item.id
  const { time } = useBookTime()
  const length = item.durationMs ?? 0
  // While it plays here, the place is the player's; otherwise the saved one.
  const at = playingThis ? time * 1000 : item.played ? 0 : (item.positionMs ?? 0)
  const series = useQuery({
    queryKey: ['books', item.libraryId, 'series', item.series],
    queryFn: () => api.libraryItems(item.libraryId, { kind: ['book'], series: [item.series!], sort: 'series' }),
    enabled: !!item.series,
  })
  const byAuthor = useQuery({ queryKey: ['children', item.authorId], queryFn: () => api.children(item.authorId!), enabled: !!item.authorId })
  const inSeries = new Set(series.data?.map((b) => b.id))
  const others = byAuthor.data?.filter((b) => b.id !== item.id && !inSeries.has(b.id))
  const finish = useMutation({
    mutationFn: (played: boolean) => api.setProgress(item.id, 0, played),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['item', item.id] })
      qc.invalidateQueries({ queryKey: ['books'] })
    },
  })

  return (
    <div className="animate-fade space-y-10">
      <Hero item={item} label={t('common:books.audiobook')} round={false}>
        {item.authorId && (
          <Link to={`/item/${item.authorId}`} className="font-semibold text-text hover:underline">
            {item.author}
          </Link>
        )}
        {item.series && (
          <Link to={groupLink(item.libraryId, 'series', item.series)} className="hover:text-text hover:underline">
            {seriesLabel(item)}
          </Link>
        )}
        {item.year && <span>{item.year}</span>}
        {!!length && <span>{duration(length)}</span>}
      </Hero>
      {!!item.narrators?.length && (
        <p className="relative -mt-6 text-sm text-muted">
          {t('common:books.narratedByLabel')}{' '}
          {item.narrators.map((n, i) => (
            <Fragment key={n}>
              {i > 0 && ', '}
              <Link to={groupLink(item.libraryId, 'narrators', n)} className="font-medium text-text hover:underline">
                {n}
              </Link>
            </Fragment>
          ))}
        </p>
      )}

      <div className="relative flex flex-wrap items-center gap-3">
        <button
          onClick={() => (playingThis ? audio.toggle() : audio.playBook(item.id))}
          className="flex h-14 items-center gap-2 rounded-full bg-accent ps-5 pe-6 font-semibold text-on-accent shadow-lg transition hover:scale-105 hover:bg-accent-strong"
        >
          {playingThis && audio.playing ? <Pause className="size-6 fill-current" /> : <Play className="size-6 fill-current" />}
          {playingThis && audio.playing ? t('player.pause') : at > 0 ? t('common:books.resume') : t('item.play')}
        </button>
        {at > 0 && length > 0 && (
          <div className="min-w-40 space-y-1">
            <div className="h-1.5 overflow-hidden rounded-full bg-text/10">
              <div className="h-full bg-accent" style={{ width: `${Math.min(100, (at / length) * 100)}%` }} />
            </div>
            <div className="text-xs text-muted">{t('common:books.left', { time: duration(B.timeLeft(at / 1000, length / 1000, audio.speed) * 1000) })}</div>
          </div>
        )}
        {item.played || at > 0 ? (
          <Button className="h-11 rounded-full" icon={item.played ? <RotateCcw className="size-4" /> : <Check className="size-4" />} onClick={() => finish.mutate(!item.played)}>
            {item.played ? t('common:books.markUnfinished') : t('common:books.markFinished')}
          </Button>
        ) : null}
        <AddToCollectionButton item={item} />
      </div>

      {item.overview && <Overview text={item.overview} />}

      {!!book.data?.chapters.length && (
        <section className="relative space-y-3">
          <h2 className="text-lg font-semibold">{t('common:books.chapters')}</h2>
          <ol className="divide-y divide-line/50 rounded-xl border border-line/60">
            {book.data.chapters.map((c, i) => {
              const now = playingThis && time * 1000 >= c.startMs && time * 1000 < c.endMs
              return (
                <li key={`${c.partId}-${c.startMs}`}>
                  <button
                    onClick={() => audio.playBook(item.id, c.startMs / 1000)}
                    className={`flex w-full items-center gap-3 px-4 py-3 text-start hover:bg-text/5 ${now ? 'text-accent' : ''}`}
                  >
                    <span className="w-7 shrink-0 text-end text-sm text-muted tabular-nums">{now ? <Equalizer playing={audio.playing} /> : i + 1}</span>
                    <span className="min-w-0 flex-1 truncate text-sm font-medium">{c.title}</span>
                    <span className="shrink-0 text-xs text-muted tabular-nums">{clock((c.endMs - c.startMs) / 1000)}</span>
                  </button>
                </li>
              )
            })}
          </ol>
        </section>
      )}

      {(series.data?.length ?? 0) > 1 && (
        <Row title={t('common:books.inSeries', { series: item.series })}>
          {series.data!.map((b) => (
            <RowItem key={b.id}>
              <BookCard book={b} sub={seriesLabel(b, false)} />
            </RowItem>
          ))}
        </Row>
      )}
      {!!others?.length && (
        <Row title={t('common:music.moreBy', { artist: item.author })}>
          {others.map((b) => (
            <RowItem key={b.id}>
              <BookCard book={b} />
            </RowItem>
          ))}
        </Row>
      )}
    </div>
  )
}

// An author: their photo, what's known about them, and their books, series
// by series.
export function AuthorPage({ item, books }: { item: ItemDetail; books?: Item[] }) {
  const { t } = useTranslation()
  const total = books?.reduce((sum, b) => sum + (b.durationMs ?? 0), 0)
  // Books outside a series first, then each series in reading order (the server's order).
  const groups: { series?: string; books: Item[] }[] = []
  for (const b of books ?? []) {
    const last = groups[groups.length - 1]
    if (last && last.series === b.series) last.books.push(b)
    else groups.push({ series: b.series, books: [b] })
  }
  return (
    <div className="animate-fade space-y-10">
      <Hero item={item} label={t('common:books.author')} round>
        {books && <span>{t('common:count.books', { count: books.length })}</span>}
        {!!total && <span>{duration(total)}</span>}
      </Hero>
      {item.overview && <Overview text={item.overview} />}
      {groups.map((g) => (
        <section key={g.series ?? ''} className="relative space-y-4">
          {g.series ? (
            <Link to={groupLink(item.libraryId, 'series', g.series)} className="text-lg font-semibold hover:text-accent-strong">
              {g.series}
            </Link>
          ) : (
            groups.length > 1 && <h2 className="text-lg font-semibold">{t('common:books.books')}</h2>
          )}
          <BookGrid>
            {g.books.map((b) => (
              <BookCard key={b.id} book={b} sub={g.series ? seriesLabel(b, false) : b.year ? String(b.year) : ''} />
            ))}
          </BookGrid>
        </section>
      ))}
    </div>
  )
}
