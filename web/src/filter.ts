// Library filters: the same fields as the server's store.ItemFilter, kept in
// the page's URL with the same names, so a filtered library can be shared,
// bookmarked or saved as it is.
import { useCallback, useEffect, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import i18n from './i18n'

// started (anything played) only lists what was listened to; Filters don't offer it.
export type Watched = 'unwatched' | 'inProgress' | 'watched' | 'notStarted' | 'started'
// series is a book's place in its series.
export type SortField = 'title' | 'added' | 'released' | 'year' | 'rating' | 'runtime' | 'lastPlayed' | 'series'

export type ItemFilter = {
  library?: number[]
  kind?: string[]
  q?: string
  genre?: string[]
  notGenre?: string[]
  yearMin?: number
  yearMax?: number
  releasedAfter?: string
  releasedBefore?: string
  releasedWithin?: number
  addedAfter?: string
  addedBefore?: string
  addedWithin?: number
  ratingMin?: number
  ratingMax?: number
  runtimeMin?: number
  runtimeMax?: number
  watched?: Watched
  tag?: string[]
  notTag?: string[]
  person?: number[]
  director?: number[]
  studio?: string[]
  country?: string[]
  language?: string[]
  certification?: string[]
  collection?: number[]
  watchList?: boolean
  series?: string[]
  narrator?: string[]
  sort?: SortField
  order?: 'asc' | 'desc'
  limit?: number
}

type FieldType = 'strings' | 'numbers' | 'number' | 'string' | 'bool'

const fields: Record<keyof ItemFilter, FieldType> = {
  library: 'numbers',
  kind: 'strings',
  q: 'string',
  genre: 'strings',
  notGenre: 'strings',
  yearMin: 'number',
  yearMax: 'number',
  releasedAfter: 'string',
  releasedBefore: 'string',
  releasedWithin: 'number',
  addedAfter: 'string',
  addedBefore: 'string',
  addedWithin: 'number',
  ratingMin: 'number',
  ratingMax: 'number',
  runtimeMin: 'number',
  runtimeMax: 'number',
  watched: 'string',
  tag: 'strings',
  notTag: 'strings',
  person: 'numbers',
  director: 'numbers',
  studio: 'strings',
  country: 'strings',
  language: 'strings',
  certification: 'strings',
  collection: 'numbers',
  watchList: 'bool',
  series: 'strings',
  narrator: 'strings',
  sort: 'string',
  order: 'string',
  limit: 'number',
}

export function toSearchParams(f: ItemFilter): URLSearchParams {
  const p = new URLSearchParams()
  for (const [key, type] of Object.entries(fields) as [keyof ItemFilter, FieldType][]) {
    const v = f[key]
    if (v === undefined || v === '' || v === false || v === 0) continue
    if (Array.isArray(v)) v.forEach((x) => p.append(key, String(x)))
    else p.set(key, type === 'bool' ? 'true' : String(v))
  }
  return p
}

// fromSearchParams reads a filter back, ignoring other parameters and bad numbers.
export function fromSearchParams(p: URLSearchParams): ItemFilter {
  const f: Record<string, unknown> = {}
  for (const [key, type] of Object.entries(fields)) {
    const all = p.getAll(key).filter((v) => v !== '')
    if (all.length === 0) continue
    if (type === 'strings') f[key] = all
    else if (type === 'numbers') {
      const nums = all.flatMap((v) => v.split(',')).map(Number).filter(Number.isFinite)
      if (nums.length) f[key] = nums
    } else if (type === 'number') {
      const n = Number(all[0])
      if (Number.isFinite(n) && n > 0) f[key] = n
    } else if (type === 'bool') {
      if (all[0] === 'true' || all[0] === '1') f[key] = true
    } else f[key] = all[0]
  }
  return f as ItemFilter
}

// Labels in the page's language, read when shown.
const labels = <K extends string>(prefix: string, keys: K[]) =>
  Object.defineProperties(
    {} as Record<K, string>,
    Object.fromEntries(keys.map((k) => [k, { enumerable: true, get: () => i18n.t(`${prefix}.${k}`) }])),
  )

export const sortLabels = labels<SortField>('common:sort', ['title', 'added', 'released', 'year', 'rating', 'runtime', 'lastPlayed'])

// Title and runtime run low to high unless asked, the rest high to low (as on the server).
export const defaultDesc = (s: SortField) => s !== 'title' && s !== 'runtime'

export const watchedLabels = labels<Watched>('common:watched', ['unwatched', 'inProgress', 'watched', 'notStarted'])

// Fields that pick titles, as opposed to sorting them.
const pickFields = (Object.keys(fields) as (keyof ItemFilter)[]).filter((k) => !['library', 'sort', 'order', 'limit'].includes(k))

// activeCount is how many filters are in use; a range counts once.
export function activeCount(f: ItemFilter): number {
  const ranges: (keyof ItemFilter)[] = ['yearMax', 'ratingMax', 'runtimeMax']
  return pickFields.reduce((n, k) => {
    const v = f[k]
    if (ranges.includes(k) && f[k.replace('Max', 'Min') as keyof ItemFilter]) return n
    return n + (Array.isArray(v) ? v.length : v ? 1 : 0)
  }, 0)
}

export function clearPicks(f: ItemFilter): ItemFilter {
  return { sort: f.sort, order: f.order }
}

export function toggleIn<T>(list: T[] | undefined, v: T): T[] | undefined {
  const next = list?.includes(v) ? list.filter((x) => x !== v) : [...(list ?? []), v]
  return next.length ? next : undefined
}

export function decades(min?: number, max?: number): number[] {
  if (!min || !max) return []
  const out = []
  for (let d = Math.floor(max / 10) * 10; d >= Math.floor(min / 10) * 10; d -= 10) out.push(d)
  return out
}

export function yearLabel(f: ItemFilter): string | undefined {
  const { yearMin: a, yearMax: b } = f
  if (a && b && a % 10 === 0 && b === a + 9) return i18n.t('common:filter.decade', { decade: a })
  if (a && b) return a === b ? `${a}` : i18n.t('common:filter.yearRange', { from: a, to: b })
  if (a) return i18n.t('common:filter.yearOrLater', { year: a })
  if (b) return i18n.t('common:filter.yearOrEarlier', { year: b })
}

// withinLabel is a stretch of time up to now, as in "Added in the last week".
export function withinLabel(days: number): string {
  if (days === 365) return i18n.t('common:filter.lastYear')
  if (days % 7 === 0 && days < 28) return i18n.t('common:filter.lastWeeks', { count: days / 7 })
  return i18n.t('common:filter.lastDays', { count: days })
}

export function runtimeLabel(f: ItemFilter, shows: boolean): string | undefined {
  const { runtimeMin: a, runtimeMax: b } = f
  const which = a && b ? 'between' : a ? 'over' : b ? 'under' : undefined
  if (!which) return
  return i18n.t(`common:filter.runtime.${shows ? 'episodes' : 'titles'}.${which}`, { min: a, max: b })
}

const storageKey = (libraryId: number) => `matinee-library-filter:${libraryId}`

// useLibraryFilter keeps a library page's filter in its URL, and remembers
// it per library, so coming back to the library shows it the same way.
export function useLibraryFilter(libraryId: number): [ItemFilter, (f: ItemFilter) => void] {
  const [params, setParams] = useSearchParams()
  const filter = useMemo(() => fromSearchParams(params), [params])

  useEffect(() => {
    if (params.size > 0) return
    try {
      const saved = localStorage.getItem(storageKey(libraryId))
      if (saved) setParams(new URLSearchParams(saved), { replace: true })
    } catch {
      // Storage can be off; the page works without it.
    }
    // Only when arriving at a library.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraryId])

  const setFilter = useCallback(
    (f: ItemFilter) => {
      const p = toSearchParams({ ...f, library: undefined })
      try {
        if (p.size > 0) localStorage.setItem(storageKey(libraryId), p.toString())
        else localStorage.removeItem(storageKey(libraryId))
      } catch {
        // As above.
      }
      setParams(p, { replace: true })
    },
    [libraryId, setParams],
  )
  return [filter, setFilter]
}
