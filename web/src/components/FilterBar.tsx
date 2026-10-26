import { useQuery } from '@tanstack/react-query'
import { ArrowDownWideNarrow, ArrowUpNarrowWide, Search, SlidersHorizontal, X } from 'lucide-react'
import { type ReactNode, useEffect, useState } from 'react'
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
import { filterTags } from '../media'
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
          aria-label={count > 0 ? `Filters, ${count} in use` : 'Filters'}
          className={`inline-flex h-9 items-center gap-2 rounded-lg border px-3 text-sm transition-colors ${
            count > 0 ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint hover:text-text'
          }`}
        >
          <SlidersHorizontal className="size-4" />
          <span className="hidden sm:inline">Filters</span>
          {count > 0 && <span className="rounded-full bg-accent px-1.5 text-xs font-semibold text-on-accent">{count}</span>}
        </button>
        {!music && (
          <Chip active={f.watched === 'unwatched'} onClick={() => set({ watched: f.watched === 'unwatched' ? undefined : 'unwatched' })}>
            Unwatched
          </Chip>
        )}
        <div className="ml-auto flex items-center gap-1">
          <label className="sr-only" htmlFor="library-sort">
            Sort by
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
            aria-label={desc ? 'Highest first; switch to lowest first' : 'Lowest first; switch to highest first'}
            title={desc ? 'Highest first' : 'Lowest first'}
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

function usePersonName(id: number, options?: FilterOptions) {
  const known = [...(options?.directors ?? []), ...(options?.cast ?? [])].find((p) => p.id === id)
  const { data } = useQuery({ queryKey: ['person', id], queryFn: () => api.person(id), enabled: !known, staleTime: Infinity })
  return known?.name ?? data?.name ?? '…'
}

function PersonChip({ id, prefix, options, onRemove }: { id: number; prefix: string; options?: FilterOptions; onRemove: () => void }) {
  return <Removable label={`${prefix}${usePersonName(id, options)}`} onRemove={onRemove} />
}

function Removable({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex h-8 items-center gap-1 rounded-full bg-text/10 pr-1 pl-3 text-sm">
      {label}
      <button onClick={onRemove} aria-label={`Remove ${label}`} className="grid size-6 place-items-center rounded-full text-muted hover:bg-text/10 hover:text-text">
        <X className="size-3.5" />
      </button>
    </span>
  )
}

// ActiveChips shows each filter in use, with a way to drop it.
function ActiveChips({ f, options, onChange, shows }: { f: ItemFilter; options?: FilterOptions; onChange: (f: ItemFilter) => void; shows: boolean }) {
  const set = (patch: Partial<ItemFilter>) => onChange({ ...f, ...patch })
  const chips: ReactNode[] = []
  const add = (key: string, label: string, patch: Partial<ItemFilter>) => chips.push(<Removable key={key} label={label} onRemove={() => set(patch)} />)
  const list = <K extends 'genre' | 'notGenre' | 'tag' | 'notTag' | 'studio' | 'country' | 'language' | 'certification'>(
    k: K,
    label: (v: string) => string,
  ) => f[k]?.forEach((v) => add(`${k}:${v}`, label(v), { [k]: toggleIn(f[k], v) }))

  if (f.q) add('q', `“${f.q}”`, { q: undefined })
  if (f.watched) add('watched', watchedLabels[f.watched], { watched: undefined })
  list('genre', (v) => v)
  list('notGenre', (v) => `Not ${v}`)
  const years = yearLabel(f)
  if (years) add('year', years, { yearMin: undefined, yearMax: undefined })
  if (f.releasedWithin) add('releasedWithin', `Released in ${withinLabel(f.releasedWithin)}`, { releasedWithin: undefined })
  if (f.releasedAfter) add('releasedAfter', `Released after ${f.releasedAfter}`, { releasedAfter: undefined })
  if (f.releasedBefore) add('releasedBefore', `Released before ${f.releasedBefore}`, { releasedBefore: undefined })
  if (f.addedWithin) add('addedWithin', `Added in ${withinLabel(f.addedWithin)}`, { addedWithin: undefined })
  if (f.addedAfter) add('addedAfter', `Added after ${f.addedAfter}`, { addedAfter: undefined })
  if (f.addedBefore) add('addedBefore', `Added before ${f.addedBefore}`, { addedBefore: undefined })
  if (f.ratingMin) add('ratingMin', `Rated ${f.ratingMin}+`, { ratingMin: undefined })
  if (f.ratingMax) add('ratingMax', `Rated ${f.ratingMax} or less`, { ratingMax: undefined })
  const runtime = runtimeLabel(f, shows)
  if (runtime) add('runtime', runtime, { runtimeMin: undefined, runtimeMax: undefined })
  list('tag', tagLabel)
  list('notTag', (v) => `Not ${tagLabel(v)}`)
  f.director?.forEach((id) =>
    chips.push(<PersonChip key={`d${id}`} id={id} prefix="Directed by " options={options} onRemove={() => set({ director: toggleIn(f.director, id) })} />),
  )
  f.person?.forEach((id) =>
    chips.push(<PersonChip key={`p${id}`} id={id} prefix="With " options={options} onRemove={() => set({ person: toggleIn(f.person, id) })} />),
  )
  list('studio', (v) => v)
  list('country', (v) => v)
  list('language', (v) => v)
  list('certification', (v) => `Rated ${v}`)
  f.collection?.forEach((id) =>
    add(`c${id}`, options?.collections.find((c) => c.value === String(id))?.label ?? 'Collection', { collection: toggleIn(f.collection, id) }),
  )
  if (f.watchList) add('watchList', 'On my watch list', { watchList: undefined })

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Filters in use">
      {chips}
      <button onClick={() => onChange(clearPicks(f))} className="h-8 px-2 text-sm text-muted hover:text-text">
        Clear all
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
  const [all, setAll] = useState(false)
  const shown = all ? values : values.filter((v, i) => i < 12 || selected.includes(v.value))
  return (
    <div className="flex flex-wrap gap-2">
      {shown.map((v) => (
        <Chip key={v.value} active={selected.includes(v.value)} onClick={() => onToggle(v.value)} title={`${v.count} ${v.count === 1 ? 'title' : 'titles'}`}>
          {label(v)}
        </Chip>
      ))}
      {!all && values.length > shown.length && (
        <button onClick={() => setAll(true)} className="h-8 px-2 text-sm text-accent hover:text-accent-strong">
          {values.length - shown.length} more
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
  const set = (patch: Partial<ItemFilter>) => onChange({ ...f, ...patch })
  const shows = libraryKind === 'tv'
  const music = libraryKind === 'music'
  const strs = (k: 'genre' | 'tag' | 'studio' | 'country' | 'language' | 'certification') => ({
    selected: f[k] ?? [],
    onToggle: (v: string) => set({ [k]: toggleIn(f[k], v) }),
  })
  const runtimeChoices: [string | undefined, string][] = shows
    ? [[undefined, 'Any'], ['0-30', 'Under 30 min'], ['30-60', '30–60 min'], ['60-0', 'Over an hour']]
    : [[undefined, 'Any'], ['0-90', 'Under 90 min'], ['90-120', '90 min – 2 hours'], ['120-0', 'Over 2 hours']]
  const runtime = f.runtimeMin || f.runtimeMax ? `${f.runtimeMin ?? 0}-${f.runtimeMax ?? 0}` : undefined
  const tags = options?.tags.length ? options.tags : []

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/60 backdrop-blur-sm" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Filters"
        onClick={(e) => e.stopPropagation()}
        className="mt-auto flex max-h-[90vh] w-full flex-col rounded-t-2xl border border-line/60 bg-surface shadow-2xl sm:mt-0 sm:max-h-none sm:max-w-md sm:rounded-none sm:border-y-0 sm:border-r-0"
      >
        <div className="flex items-center justify-between border-b border-line/60 px-5 py-4">
          <h2 className="text-lg font-semibold">Filters</h2>
          <div className="flex items-center gap-3">
            {activeCount(f) > 0 && (
              <button onClick={() => onChange(clearPicks(f))} className="text-sm text-muted hover:text-text">
                Clear all
              </button>
            )}
            <button onClick={onClose} aria-label="Close" className="text-muted hover:text-text">
              <X className="size-5" />
            </button>
          </div>
        </div>
        <div className="flex-1 space-y-6 overflow-y-auto px-5 py-5">
          <Section title="Title">
            <input
              type="search"
              value={f.q ?? ''}
              onChange={(e) => set({ q: e.target.value || undefined })}
              placeholder="Words in the title"
              className={inputClass}
            />
          </Section>
          {!music && (
            <Section title="Watched">
              <Choice<Watched>
                choices={[[undefined, 'All'], ['unwatched', 'Unwatched'], ['inProgress', 'In progress'], ['watched', 'Watched'], ['notStarted', 'Not started']]}
                value={f.watched}
                onChange={(watched) => set({ watched })}
              />
              <label className="flex items-center gap-2 pt-1 text-sm">
                <input type="checkbox" checked={!!f.watchList} onChange={(e) => set({ watchList: e.target.checked || undefined })} className="size-4 accent-[var(--color-accent)]" />
                Only titles on my watch list
              </label>
            </Section>
          )}
          {!!options?.genres.length && (
            <Section title="Genre">
              <Values values={options.genres} {...strs('genre')} />
            </Section>
          )}
          <Section title="Year">
            {options && (
              <Choice<number>
                choices={[[undefined, 'Any'], ...decades(options.yearMin, options.yearMax).map((d): [number, string] => [d, `${d}s`])]}
                value={f.yearMin && f.yearMax === f.yearMin + 9 && f.yearMin % 10 === 0 ? f.yearMin : !f.yearMin && !f.yearMax ? undefined : -1}
                onChange={(d) => set({ yearMin: d, yearMax: d ? d + 9 : undefined })}
              />
            )}
            <div className="flex items-center gap-2 text-sm text-muted">
              <input
                type="number"
                inputMode="numeric"
                aria-label="From year"
                placeholder={options?.yearMin ? String(options.yearMin) : 'From'}
                value={f.yearMin ?? ''}
                onChange={(e) => set({ yearMin: Number(e.target.value) || undefined })}
                className={`${inputClass} !h-9 !w-28`}
              />
              to
              <input
                type="number"
                inputMode="numeric"
                aria-label="To year"
                placeholder={options?.yearMax ? String(options.yearMax) : 'To'}
                value={f.yearMax ?? ''}
                onChange={(e) => set({ yearMax: Number(e.target.value) || undefined })}
                className={`${inputClass} !h-9 !w-28`}
              />
            </div>
          </Section>
          {!music && (
            <Section title={shows ? 'First aired' : 'Released'}>
              <Choice<number>
                choices={[[undefined, 'Any time'], [30, 'Last 30 days'], [90, 'Last 3 months'], [365, 'Last year']]}
                value={f.releasedWithin}
                onChange={(releasedWithin) => set({ releasedWithin })}
              />
            </Section>
          )}
          <Section title="Added">
            <Choice<number>
              choices={[[undefined, 'Any time'], [7, 'Last week'], [30, 'Last 30 days'], [90, 'Last 3 months'], [365, 'Last year']]}
              value={f.addedWithin}
              onChange={(addedWithin) => set({ addedWithin })}
            />
          </Section>
          {(options?.hasRatings ?? true) && !music && (
            <Section title="Rating">
              <Choice<number>
                choices={[[undefined, 'Any'], [6, '6+'], [7, '7+'], [8, '8+'], [9, '9+']]}
                value={f.ratingMin}
                onChange={(ratingMin) => set({ ratingMin })}
              />
            </Section>
          )}
          {(options?.hasRuntimes ?? true) && !music && (
            <Section title={shows ? 'Episode length' : 'Runtime'}>
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
            <Section title="Quality">
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
            <Section title={shows ? 'Network or studio' : 'Studio'}>
              <Values values={options.studios} {...strs('studio')} />
            </Section>
          )}
          {!!options?.collections.length && (
            <Section title="Collection">
              <Values
                values={options.collections}
                selected={(f.collection ?? []).map(String)}
                onToggle={(v) => set({ collection: toggleIn(f.collection, Number(v)) })}
              />
            </Section>
          )}
          {!!options?.certifications.length && (
            <Section title="Age rating">
              <Values values={options.certifications} {...strs('certification')} />
            </Section>
          )}
          {!!options?.languages.length && (
            <Section title="Original language">
              <Values values={options.languages} {...strs('language')} />
            </Section>
          )}
          {!!options?.countries.length && (
            <Section title="Country">
              <Values values={options.countries} {...strs('country')} />
            </Section>
          )}
        </div>
        <div className="border-t border-line/60 px-5 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          <button onClick={onClose} className="h-11 w-full rounded-lg bg-accent text-sm font-semibold text-on-accent hover:bg-accent-strong">
            Show results
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
        <Section title={shows ? 'Created or directed by' : 'Director'}>
          <Values values={asValues(directors)} selected={(f.director ?? []).map(String)} onToggle={toggle('director')} />
        </Section>
      )}
      <Section title="Cast and crew">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-faint" />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a person" className={`${inputClass} pl-9`} />
        </div>
        {q.trim().length >= 2 ? (
          found && found.length === 0 ? (
            <p className="text-sm text-muted">Nobody by that name in this library.</p>
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
