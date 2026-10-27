import { useQuery } from '@tanstack/react-query'
import { ArrowDownWideNarrow, ArrowUpNarrowWide, Search, SlidersHorizontal, X } from 'lucide-react'
import { type ReactNode, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type FilterOptions, type FilterPerson, type FilterValue } from '../api'
import {
  activeCount,
  clearPicks,
  decades,
  defaultDesc,
  type ItemFilter,
  runtimeLabel,
  type SortField,
  sortLabels,
  toggleIn,
  type Watched,
  watchedLabels,
  withinLabel,
  yearLabel,
} from '../filter'
import i18n from '../i18n'
import { filterTags } from '../media'
import { genreName } from '../people'
import { inputClass } from './ui'

type Props = {
  value: ItemFilter
  onChange: (f: ItemFilter) => void
  // The library being filtered, for the values its titles have. Without
  // one, only filters that don't depend on the library are offered.
  libraryId?: number
  libraryKind?: string
}

// FilterBar is a library's sort order, a Filters panel with every field
// the server can filter by, and the filters in use as removable chips.
export function FilterBar({ value: f, onChange, libraryId, libraryKind }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const { data: options } = useQuery({
    queryKey: ['library-filters', libraryId],
    queryFn: () => api.libraryFilters(libraryId!),
    enabled: libraryId !== undefined,
    staleTime: 60_000,
  })
  const sort = f.sort ?? 'title'
  const desc = f.order ? f.order === 'desc' : defaultDesc(sort)
  const count = activeCount(f)
  const set = (patch: Partial<ItemFilter>) => onChange({ ...f, ...patch })
  const music = libraryKind === 'music'

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          aria-label={count > 0 ? t('filters.inUse', { count }) : t('filters.title')}
          className={`inline-flex h-9 items-center gap-2 rounded-lg border px-3 text-sm transition-colors ${
            count > 0 ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint hover:text-text'
          }`}
        >
          <SlidersHorizontal className="size-4" />
          <span className="hidden sm:inline">{t('filters.title')}</span>
          {count > 0 && <span className="rounded-full bg-accent px-1.5 text-xs font-semibold text-on-accent">{count}</span>}
        </button>
        {!music && (
          <Chip active={f.watched === 'unwatched'} onClick={() => set({ watched: f.watched === 'unwatched' ? undefined : 'unwatched' })}>
            {t('common:watched.unwatched')}
          </Chip>
        )}
        <div className="ms-auto flex items-center gap-1">
          <label className="sr-only" htmlFor="library-sort">
            {t('filters.sortBy')}
          </label>
          <select
            id="library-sort"
            value={sort}
            onChange={(e) => set({ sort: e.target.value === 'title' ? undefined : (e.target.value as SortField), order: undefined })}
            className="h-9 max-w-36 rounded-lg border border-line bg-surface px-2 text-sm sm:max-w-none text-text focus:border-accent focus:outline-none"
          >
            {(Object.keys(sortLabels) as SortField[])
              .filter((s) => !(music && (s === 'released' || s === 'lastPlayed')))
              .map((s) => (
                <option key={s} value={s}>
                  {sortLabels[s]}
                </option>
              ))}
          </select>
          <button
            onClick={() => set({ order: desc === defaultDesc(sort) ? (desc ? 'asc' : 'desc') : undefined })}
            aria-label={desc ? t('filters.highestFirstSwitch') : t('filters.lowestFirstSwitch')}
            title={desc ? t('filters.highestFirst') : t('filters.lowestFirst')}
            className="grid size-9 place-items-center rounded-lg border border-line text-muted hover:border-faint hover:text-text"
          >
            {desc ? <ArrowDownWideNarrow className="size-4" /> : <ArrowUpNarrowWide className="size-4" />}
          </button>
        </div>
      </div>
      {count > 0 && <ActiveChips f={f} options={options} onChange={onChange} shows={libraryKind === 'tv'} />}
      {open && (
        <FilterPanel value={f} onChange={onChange} options={options} libraryId={libraryId} libraryKind={libraryKind} onClose={() => setOpen(false)} />
      )}
    </div>
  )
}

function Chip({ active, onClick, children, title }: { active: boolean; onClick: () => void; children: ReactNode; title?: string }) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      title={title}
      className={`h-8 rounded-full border px-3 text-sm whitespace-nowrap transition-colors ${
        active ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint hover:text-text'
      }`}
    >
      {children}
    </button>
  )
}

const tagLabel = (tag: string) => filterTags.find((t) => t.tag === tag)?.label ?? tag
const not = (label: string) => i18n.t('filters.not', { label })

function usePersonName(id: number, options?: FilterOptions) {
  const known = [...(options?.directors ?? []), ...(options?.cast ?? [])].find((p) => p.id === id)
  const { data } = useQuery({ queryKey: ['person', id], queryFn: () => api.person(id), enabled: !known, staleTime: Infinity })
  return known?.name ?? data?.name ?? '…'
}

function PersonChip({ id, kind, options, onRemove }: { id: number; kind: 'directedBy' | 'with'; options?: FilterOptions; onRemove: () => void }) {
  const { t } = useTranslation()
  return <Removable label={t(`filters.chip.${kind}`, { name: usePersonName(id, options) })} onRemove={onRemove} />
}

function Removable({ label, onRemove }: { label: string; onRemove: () => void }) {
  const { t } = useTranslation()
  return (
    <span className="inline-flex h-8 items-center gap-1 rounded-full bg-text/10 ps-3 pe-1 text-sm">
      {label}
      <button onClick={onRemove} aria-label={t('filters.remove', { label })} className="grid size-6 place-items-center rounded-full text-muted hover:bg-text/10 hover:text-text">
        <X className="size-3.5" />
      </button>
    </span>
  )
}

// ActiveChips shows each filter in use, with a way to drop it.
function ActiveChips({ f, options, onChange, shows }: { f: ItemFilter; options?: FilterOptions; onChange: (f: ItemFilter) => void; shows: boolean }) {
  const { t } = useTranslation()
  const set = (patch: Partial<ItemFilter>) => onChange({ ...f, ...patch })
  const chips: ReactNode[] = []
  const add = (key: string, label: string, patch: Partial<ItemFilter>) => chips.push(<Removable key={key} label={label} onRemove={() => set(patch)} />)
  const list = <K extends 'genre' | 'notGenre' | 'tag' | 'notTag' | 'studio' | 'country' | 'language' | 'certification'>(
    k: K,
    label: (v: string) => string,
  ) => f[k]?.forEach((v) => add(`${k}:${v}`, label(v), { [k]: toggleIn(f[k], v) }))

  if (f.q) add('q', t('filters.chip.words', { q: f.q }), { q: undefined })
  if (f.watched) add('watched', watchedLabels[f.watched], { watched: undefined })
  list('genre', genreName)
  list('notGenre', (v) => not(genreName(v)))
  const years = yearLabel(f)
  if (years) add('year', years, { yearMin: undefined, yearMax: undefined })
  if (f.releasedWithin) add('releasedWithin', t('filters.chip.releasedWithin', { within: withinLabel(f.releasedWithin) }), { releasedWithin: undefined })
  if (f.releasedAfter) add('releasedAfter', t('filters.chip.releasedAfter', { date: f.releasedAfter }), { releasedAfter: undefined })
  if (f.releasedBefore) add('releasedBefore', t('filters.chip.releasedBefore', { date: f.releasedBefore }), { releasedBefore: undefined })
  if (f.addedWithin) add('addedWithin', t('filters.chip.addedWithin', { within: withinLabel(f.addedWithin) }), { addedWithin: undefined })
  if (f.addedAfter) add('addedAfter', t('filters.chip.addedAfter', { date: f.addedAfter }), { addedAfter: undefined })
  if (f.addedBefore) add('addedBefore', t('filters.chip.addedBefore', { date: f.addedBefore }), { addedBefore: undefined })
  if (f.ratingMin) add('ratingMin', t('filters.chip.ratingMin', { rating: f.ratingMin }), { ratingMin: undefined })
  if (f.ratingMax) add('ratingMax', t('filters.chip.ratingMax', { rating: f.ratingMax }), { ratingMax: undefined })
  const runtime = runtimeLabel(f, shows)
  if (runtime) add('runtime', runtime, { runtimeMin: undefined, runtimeMax: undefined })
  list('tag', tagLabel)
  list('notTag', (v) => not(tagLabel(v)))
  f.director?.forEach((id) =>
    chips.push(<PersonChip key={`d${id}`} id={id} kind="directedBy" options={options} onRemove={() => set({ director: toggleIn(f.director, id) })} />),
  )
  f.person?.forEach((id) =>
    chips.push(<PersonChip key={`p${id}`} id={id} kind="with" options={options} onRemove={() => set({ person: toggleIn(f.person, id) })} />),
  )
  list('studio', (v) => v)
  list('country', (v) => v)
  list('language', (v) => v)
  list('certification', (v) => t('filters.chip.certification', { rating: v }))
  f.collection?.forEach((id) =>
    add(`c${id}`, options?.collections.find((c) => c.value === String(id))?.label ?? t('filters.collection'), { collection: toggleIn(f.collection, id) }),
  )
  if (f.watchList) add('watchList', t('filters.chip.watchList'), { watchList: undefined })

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('filters.active')}>
      {chips}
      <button onClick={() => onChange(clearPicks(f))} className="h-8 px-2 text-sm text-muted hover:text-text">
        {t('filters.clearAll')}
      </button>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2.5">
      <h3 className="text-xs font-medium tracking-wide text-muted uppercase">{title}</h3>
      {children}
    </section>
  )
}

// Values lists the most common values first and hides the long tail until asked.
function Values({ values, selected, onToggle, label = (v) => v.label ?? v.value }: {
  values: FilterValue[]
  selected: string[]
  onToggle: (v: string) => void
  label?: (v: FilterValue) => string
}) {
  const { t } = useTranslation()
  const [all, setAll] = useState(false)
  const shown = all ? values : values.filter((v, i) => i < 12 || selected.includes(v.value))
  return (
    <div className="flex flex-wrap gap-2">
      {shown.map((v) => (
        <Chip key={v.value} active={selected.includes(v.value)} onClick={() => onToggle(v.value)} title={t('common:count.titles', { count: v.count })}>
          {label(v)}
        </Chip>
      ))}
      {!all && values.length > shown.length && (
        <button onClick={() => setAll(true)} className="h-8 px-2 text-sm text-accent hover:text-accent-strong">
          {t('filters.more', { count: values.length - shown.length })}
        </button>
      )}
    </div>
  )
}

function Choice<T extends string | number>({ choices, value, onChange }: { choices: [T | undefined, string][]; value: T | undefined; onChange: (v: T | undefined) => void }) {
  return (
    <div className="flex flex-wrap gap-2">
      {choices.map(([v, label]) => (
        <Chip key={label} active={value === v} onClick={() => onChange(v)}>
          {label}
        </Chip>
      ))}
    </div>
  )
}

function FilterPanel({ value: f, onChange, options, libraryId, libraryKind, onClose }: Props & { options?: FilterOptions; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const { t } = useTranslation()
  const set = (patch: Partial<ItemFilter>) => onChange({ ...f, ...patch })
  const shows = libraryKind === 'tv'
  const music = libraryKind === 'music'
  const strs = (k: 'genre' | 'tag' | 'studio' | 'country' | 'language' | 'certification') => ({
    selected: f[k] ?? [],
    onToggle: (v: string) => set({ [k]: toggleIn(f[k], v) }),
  })
  const runtimeChoices: [string | undefined, string][] = shows
    ? [[undefined, t('filters.any')], ['0-30', t('filters.runtime.under30')], ['30-60', t('filters.runtime.30to60')], ['60-0', t('filters.runtime.overHour')]]
    : [[undefined, t('filters.any')], ['0-90', t('filters.runtime.under90')], ['90-120', t('filters.runtime.90to120')], ['120-0', t('filters.runtime.over2Hours')]]
  const runtime = f.runtimeMin || f.runtimeMax ? `${f.runtimeMin ?? 0}-${f.runtimeMax ?? 0}` : undefined
  const tags = options?.tags.length ? options.tags : []

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/60 backdrop-blur-sm" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('filters.title')}
        onClick={(e) => e.stopPropagation()}
        className="mt-auto flex max-h-[90vh] w-full flex-col rounded-t-2xl border border-line/60 bg-surface shadow-2xl sm:mt-0 sm:max-h-none sm:max-w-md sm:rounded-none sm:border-y-0 sm:border-e-0"
      >
        <div className="flex items-center justify-between border-b border-line/60 px-5 py-4">
          <h2 className="text-lg font-semibold">{t('filters.title')}</h2>
          <div className="flex items-center gap-3">
            {activeCount(f) > 0 && (
              <button onClick={() => onChange(clearPicks(f))} className="text-sm text-muted hover:text-text">
                {t('filters.clearAll')}
              </button>
            )}
            <button onClick={onClose} aria-label={t('close')} className="text-muted hover:text-text">
              <X className="size-5" />
            </button>
          </div>
        </div>
        <div className="flex-1 space-y-6 overflow-y-auto px-5 py-5">
          <Section title={t('filters.section.title')}>
            <input
              type="search"
              value={f.q ?? ''}
              onChange={(e) => set({ q: e.target.value || undefined })}
              placeholder={t('filters.wordsInTitle')}
              className={inputClass}
            />
          </Section>
          {!music && (
            <Section title={t('filters.section.watched')}>
              <Choice<Watched>
                choices={[[undefined, t('filters.all')], ...(Object.keys(watchedLabels) as Watched[]).map((w): [Watched, string] => [w, watchedLabels[w]])]}
                value={f.watched}
                onChange={(watched) => set({ watched })}
              />
              <label className="flex items-center gap-2 pt-1 text-sm">
                <input type="checkbox" checked={!!f.watchList} onChange={(e) => set({ watchList: e.target.checked || undefined })} className="size-4 accent-[var(--color-accent)]" />
                {t('filters.onlyWatchList')}
              </label>
            </Section>
          )}
          {!!options?.genres.length && (
            <Section title={t('filters.section.genre')}>
              <Values values={options.genres} label={(v) => genreName(v.value)} {...strs('genre')} />
            </Section>
          )}
          <Section title={t('filters.section.year')}>
            {options && (
              <Choice<number>
                choices={[[undefined, t('filters.any')], ...decades(options.yearMin, options.yearMax).map((d): [number, string] => [d, t('common:filter.decade', { decade: d })])]}
                value={f.yearMin && f.yearMax === f.yearMin + 9 && f.yearMin % 10 === 0 ? f.yearMin : !f.yearMin && !f.yearMax ? undefined : -1}
                onChange={(d) => set({ yearMin: d, yearMax: d ? d + 9 : undefined })}
              />
            )}
            <div className="flex items-center gap-2 text-sm text-muted">
              <input
                type="number"
                inputMode="numeric"
                aria-label={t('filters.fromYear')}
                placeholder={options?.yearMin ? String(options.yearMin) : t('filters.from')}
                value={f.yearMin ?? ''}
                onChange={(e) => set({ yearMin: Number(e.target.value) || undefined })}
                className={`${inputClass} !h-9 !w-28`}
              />
              {t('filters.to')}
              <input
                type="number"
                inputMode="numeric"
                aria-label={t('filters.toYear')}
                placeholder={options?.yearMax ? String(options.yearMax) : t('filters.toPlaceholder')}
                value={f.yearMax ?? ''}
                onChange={(e) => set({ yearMax: Number(e.target.value) || undefined })}
                className={`${inputClass} !h-9 !w-28`}
              />
            </div>
          </Section>
          {!music && (
            <Section title={shows ? t('filters.section.firstAired') : t('filters.section.released')}>
              <Choice<number>
                choices={[[undefined, t('filters.anyTime')], [30, t('filters.last30Days')], [90, t('filters.last3Months')], [365, t('filters.lastYear')]]}
                value={f.releasedWithin}
                onChange={(releasedWithin) => set({ releasedWithin })}
              />
            </Section>
          )}
          <Section title={t('filters.section.added')}>
            <Choice<number>
              choices={[[undefined, t('filters.anyTime')], [7, t('filters.lastWeek')], [30, t('filters.last30Days')], [90, t('filters.last3Months')], [365, t('filters.lastYear')]]}
              value={f.addedWithin}
              onChange={(addedWithin) => set({ addedWithin })}
            />
          </Section>
          {(options?.hasRatings ?? true) && !music && (
            <Section title={t('filters.section.rating')}>
              <Choice<number>
                choices={[[undefined, t('filters.any')], ...[6, 7, 8, 9].map((n): [number, string] => [n, t('filters.ratingAtLeast', { rating: n })])]}
                value={f.ratingMin}
                onChange={(ratingMin) => set({ ratingMin })}
              />
            </Section>
          )}
          {(options?.hasRuntimes ?? true) && !music && (
            <Section title={shows ? t('filters.section.episodeLength') : t('filters.section.runtime')}>
              <Choice<string>
                choices={runtimeChoices}
                value={runtime}
                onChange={(v) => {
                  const [a, b] = (v ?? '0-0').split('-').map(Number)
                  set({ runtimeMin: a || undefined, runtimeMax: b || undefined })
                }}
              />
            </Section>
          )}
          {tags.length > 0 && (
            <Section title={t('filters.section.quality')}>
              <Values values={tags} label={(v) => tagLabel(v.value)} {...strs('tag')} />
            </Section>
          )}
          {libraryId !== undefined && !music && (
            <People
              libraryId={libraryId}
              directors={options?.directors ?? []}
              cast={options?.cast ?? []}
              f={f}
              set={set}
              shows={shows}
            />
          )}
          {!!options?.studios.length && (
            <Section title={shows ? t('filters.section.network') : t('filters.section.studio')}>
              <Values values={options.studios} {...strs('studio')} />
            </Section>
          )}
          {!!options?.collections.length && (
            <Section title={t('filters.collection')}>
              <Values
                values={options.collections}
                selected={(f.collection ?? []).map(String)}
                onToggle={(v) => set({ collection: toggleIn(f.collection, Number(v)) })}
              />
            </Section>
          )}
          {!!options?.certifications.length && (
            <Section title={t('filters.section.ageRating')}>
              <Values values={options.certifications} {...strs('certification')} />
            </Section>
          )}
          {!!options?.languages.length && (
            <Section title={t('filters.section.language')}>
              <Values values={options.languages} {...strs('language')} />
            </Section>
          )}
          {!!options?.countries.length && (
            <Section title={t('filters.section.country')}>
              <Values values={options.countries} {...strs('country')} />
            </Section>
          )}
        </div>
        <div className="border-t border-line/60 px-5 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          <button onClick={onClose} className="h-11 w-full rounded-lg bg-accent text-sm font-semibold text-on-accent hover:bg-accent-strong">
            {t('filters.showResults')}
          </button>
        </div>
      </div>
    </div>
  )
}

// People offers the most credited directors and actors, and a search for anyone else.
function People({ libraryId, directors, cast, f, set, shows }: {
  libraryId: number
  directors: FilterPerson[]
  cast: FilterPerson[]
  f: ItemFilter
  set: (patch: Partial<ItemFilter>) => void
  shows: boolean
}) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const { data: found } = useQuery({
    queryKey: ['library-people', libraryId, q.trim()],
    queryFn: () => api.libraryPeople(libraryId, q.trim()),
    enabled: q.trim().length >= 2,
    staleTime: 60_000,
  })
  const asValues = (people: FilterPerson[]): FilterValue[] => people.map((p) => ({ value: String(p.id), label: p.name, count: p.count }))
  const toggle = (k: 'person' | 'director') => (v: string) => set({ [k]: toggleIn(f[k], Number(v)) })
  return (
    <>
      {directors.length > 0 && (
        <Section title={shows ? t('filters.section.creator') : t('filters.section.director')}>
          <Values values={asValues(directors)} selected={(f.director ?? []).map(String)} onToggle={toggle('director')} />
        </Section>
      )}
      <Section title={t('filters.section.people')}>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 start-3 size-4 -translate-y-1/2 text-faint" />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('filters.findPerson')} className={`${inputClass} ps-9`} />
        </div>
        {q.trim().length >= 2 ? (
          found && found.length === 0 ? (
            <p className="text-sm text-muted">{t('filters.nobody')}</p>
          ) : (
            <Values values={asValues(found ?? [])} selected={(f.person ?? []).map(String)} onToggle={toggle('person')} />
          )
        ) : (
          cast.length > 0 && <Values values={asValues(cast)} selected={(f.person ?? []).map(String)} onToggle={toggle('person')} />
        )}
      </Section>
    </>
  )
}
