import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Layers, LibraryBig, Pin, Plus, Sparkles, X } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  api,
  imageUrl,
  type Collection,
  type CollectionInput,
  type CollectionSort,
  type ItemDetail,
  type ItemFilterJSON,
  type User,
} from '../api'
import { FilterBar } from './FilterBar'
import type { ItemFilter } from '../filter'
import { PosterCard } from './Card'
import { Row, RowItem } from './Row'
import { useLibraries } from './Shell'
import { Button, Field, inputClass } from './ui'

export const collectionLink = (c: { id: string }) => `/collection/${encodeURIComponent(c.id)}`
export const boxSetLink = (id: number) => `/box-set/${id}`

/** Everything that shows collections refetches after a change. */
export function invalidateCollections(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['collections'] })
  qc.invalidateQueries({ queryKey: ['collection'] })
  qc.invalidateQueries({ queryKey: ['home'] })
}

const sortLabels: Record<CollectionSort, string> = {
  '': 'Own order',
  title: 'Title',
  added: 'Date added',
  released: 'Release date',
  year: 'Year',
  rating: 'Rating',
  runtime: 'Runtime',
  lastPlayed: 'Last played',
  shuffle: 'Shuffle daily',
}
const manualSorts: CollectionSort[] = ['', 'title', 'added', 'year', 'rating', 'runtime', 'shuffle']
const smartSorts: CollectionSort[] = ['', 'title', 'added', 'released', 'year', 'rating', 'runtime', 'lastPlayed', 'shuffle']

/** A cover made from up to four posters, or the collection's initial when it's empty. */
export function Mosaic({ ids, title }: { ids: number[]; title: string }) {
  const shown = ids.slice(0, 4)
  return (
    <div className="grid aspect-[2/3] grid-cols-2 grid-rows-2 gap-0.5 overflow-hidden rounded-xl bg-raised ring-1 ring-text/5 transition group-hover:ring-accent/70">
      {shown.length === 0 ? (
        <div className="col-span-2 row-span-2 grid place-items-center text-4xl font-semibold text-faint">{title[0]}</div>
      ) : (
        shown.map((id, i) => (
          <img
            key={id}
            src={imageUrl(id, 'poster')}
            alt=""
            loading="lazy"
            className={`size-full object-cover ${shown.length === 1 ? 'col-span-2 row-span-2' : shown.length === 2 ? 'row-span-2' : shown.length === 3 && i === 0 ? 'row-span-2' : ''}`}
          />
        ))
      )}
    </div>
  )
}

export function CollectionCard({ c }: { c: Collection }) {
  return (
    <Link to={collectionLink(c)} className="group block animate-fade">
      <Mosaic ids={c.items.map((i) => i.id)} title={c.name} />
      <div className="mt-2 px-0.5">
        <div className="flex items-center gap-1.5 truncate text-sm font-medium text-text group-hover:text-accent-strong">
          {c.kind === 'smart' && <Sparkles className="size-3.5 shrink-0 text-accent" aria-label="Smart" />}
          <span className="truncate">{c.name}</span>
        </div>
        <div className="truncate text-xs text-muted">{c.count === 1 ? '1 title' : `${c.count} titles`}</div>
      </div>
    </Link>
  )
}

export function CardGrid({ children }: { children: ReactNode }) {
  return (
    <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))' }}>
      {children}
    </div>
  )
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', esc)
    return () => window.removeEventListener('keydown', esc)
  }, [onClose])
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(e) => e.stopPropagation()}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-t-2xl border border-line/60 bg-surface p-5 shadow-2xl sm:rounded-2xl md:p-6"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button onClick={onClose} aria-label="Close" className="text-muted hover:text-text">
            <X className="size-5" />
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}

/**
 * Makes a collection or changes one. A smart collection's rules are the
 * library page's filters; `start` fills them in, e.g. from the filters
 * someone has on now.
 */
export function CollectionEditor({
  collection,
  kind = 'smart',
  start,
  library,
  onClose,
  onSaved,
}: {
  collection?: Collection
  kind?: 'smart' | 'manual'
  start?: ItemFilterJSON
  library?: number
  onClose: () => void
  onSaved?: (c: Collection) => void
}) {
  const qc = useQueryClient()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const { data: libraries } = useLibraries()
  const smart = (collection?.kind ?? kind) === 'smart'
  const [name, setName] = useState(collection?.name ?? '')
  const [filter, setFilter] = useState<ItemFilter>((collection?.filter ?? start ?? (library ? { library: [library] } : {})) as ItemFilter)
  const [sort, setSort] = useState<CollectionSort>(collection?.sort ?? '')
  const [order, setOrder] = useState<'' | 'asc' | 'desc'>(collection?.order ?? '')
  const [pinHome, setPinHome] = useState(collection?.pinHome ?? false)
  const [pinLibrary, setPinLibrary] = useState<number | undefined>(collection?.pinLibrary ?? undefined)
  const [shared, setShared] = useState(collection?.shared ?? false)
  const save = useMutation({
    mutationFn: () => {
      const body: CollectionInput = {
        name,
        sort,
        order,
        pinHome,
        pinLibrary: pinLibrary ?? null,
        ...(smart && { filter: filter as ItemFilterJSON }),
      }
      return collection
        ? api.updateCollection(collection.id, body)
        : api.createCollection({ ...body, kind: smart ? 'smart' : 'manual', shared })
    },
    onSuccess: (c) => {
      invalidateCollections(qc)
      onSaved?.(c)
      onClose()
    },
  })
  const filterLibrary = (filter as { library?: number[] }).library
  return (
    <Dialog title={collection ? `Edit ${collection.name}` : smart ? 'New smart collection' : 'New collection'} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
        className="space-y-5"
      >
        <Field label="Name">
          <input
            className={inputClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={smart ? 'Unwatched 80s action' : 'Movie night'}
            autoFocus
            maxLength={100}
          />
        </Field>
        {smart && (
          <div className="space-y-2">
            <span className="text-xs font-medium tracking-wide text-muted uppercase">Rules</span>
            <p className="text-sm text-muted">Titles that match these filters join by themselves as the library changes.</p>
            <FilterBar value={filter} onChange={setFilter} libraryId={filterLibrary?.length === 1 ? filterLibrary[0] : undefined} />
          </div>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Order">
            <select className={inputClass} value={sort} onChange={(e) => setSort(e.target.value as CollectionSort)}>
              {(smart ? smartSorts : manualSorts).map((s) => (
                <option key={s} value={s}>
                  {s === '' ? (smart ? 'As the rules sort' : 'The order titles were added') : sortLabels[s]}
                </option>
              ))}
            </select>
          </Field>
          {sort !== '' && sort !== 'shuffle' && (
            <Field label="Direction">
              <select className={inputClass} value={order} onChange={(e) => setOrder(e.target.value as '' | 'asc' | 'desc')}>
                <option value="">Usual</option>
                <option value="asc">Ascending</option>
                <option value="desc">Descending</option>
              </select>
            </Field>
          )}
        </div>
        <fieldset className="space-y-2">
          <legend className="mb-1 text-xs font-medium tracking-wide text-muted uppercase">Show as a row</legend>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={pinHome} onChange={(e) => setPinHome(e.target.checked)} className="accent-accent" /> On Home
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={pinLibrary !== undefined}
              onChange={(e) => setPinLibrary(e.target.checked ? (library ?? libraries?.[0]?.id) : undefined)}
              className="accent-accent"
              disabled={!libraries?.length}
            />
            On a library page
            {pinLibrary !== undefined && (
              <select
                className="ml-1 rounded-md border border-line bg-surface px-2 py-1 text-sm"
                value={pinLibrary}
                onChange={(e) => setPinLibrary(Number(e.target.value))}
                aria-label="Library"
              >
                {libraries?.map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.name}
                  </option>
                ))}
              </select>
            )}
          </label>
        </fieldset>
        {!collection && me?.isAdmin && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} className="accent-accent" /> Share with
            everyone on this server
          </label>
        )}
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={!name.trim() || save.isPending}>
            {collection ? 'Save' : 'Create'}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

/** Puts a title in the collections kept by hand, or takes it out. */
export function AddToCollectionButton({ item }: { item: ItemDetail }) {
  const qc = useQueryClient()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const { data } = useQuery({
    queryKey: ['collections', { item: item.id }],
    queryFn: () => api.collections({ item: item.id }),
    enabled: open,
  })
  const toggle = useMutation({
    mutationFn: ({ c, add }: { c: Collection; add: boolean }) =>
      add ? api.addToCollection(c.id, item.id) : api.removeFromCollection(c.id, item.id),
    onSettled: () => invalidateCollections(qc),
  })
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', away)
    return () => document.removeEventListener('mousedown', away)
  }, [open])

  if (me?.guest || (item.kind !== 'movie' && item.kind !== 'show')) return null
  const mine = data?.collections.filter((c) => c.kind === 'manual' && c.canEdit) ?? []
  return (
    <div ref={ref} className="relative">
      <Button onClick={() => setOpen(!open)} aria-expanded={open} icon={<Layers className="size-4" />}>
        Collections
      </Button>
      {open && (
        <div className="absolute top-full left-0 z-30 mt-2 w-64 rounded-xl border border-line/60 bg-surface p-1.5 shadow-2xl">
          {!data ? (
            <div className="px-3 py-2 text-sm text-muted">Loading…</div>
          ) : (
            mine.map((c) => (
              <button
                key={c.id}
                onClick={() => toggle.mutate({ c, add: !c.contains })}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-text/5"
                aria-pressed={!!c.contains}
              >
                <span className="grid size-4 place-items-center">{c.contains && <Check className="size-4 text-accent" />}</span>
                <span className="truncate">{c.name}</span>
              </button>
            ))
          )}
          <button
            onClick={() => {
              setCreating(true)
              setOpen(false)
            }}
            className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm text-muted hover:bg-text/5 hover:text-text"
          >
            <Plus className="size-4" /> New collection
          </button>
        </div>
      )}
      {creating && (
        <CollectionEditor
          kind="manual"
          onClose={() => setCreating(false)}
          onSaved={(c) => api.addToCollection(c.id, item.id).then(() => invalidateCollections(qc))}
        />
      )}
    </div>
  )
}

/** Small labels for where a collection shows up. */
export function PinBadges({ c }: { c: Collection }) {
  const { data: libraries } = useLibraries()
  const lib = libraries?.find((l) => l.id === c.pinLibrary)
  return (
    <>
      {c.pinHome && (
        <span className="flex items-center gap-1 rounded-full bg-text/10 px-2 py-0.5 text-xs text-muted">
          <Pin className="size-3" /> Home
        </span>
      )}
      {lib && (
        <span className="flex items-center gap-1 rounded-full bg-text/10 px-2 py-0.5 text-xs text-muted">
          <LibraryBig className="size-3" /> {lib.name}
        </span>
      )}
    </>
  )
}

/** The collections pinned to a library's page, as rows above its titles. */
export function PinnedCollections({ library }: { library: number }) {
  const { data } = useQuery({ queryKey: ['collections', { library }], queryFn: () => api.collections({ library }) })
  const rows = data?.collections.filter((c) => c.items.length > 0) ?? []
  if (rows.length === 0) return null
  return (
    <div className="space-y-10">
      {rows.map((c) => (
        <Row key={c.id} title={c.name} more={collectionLink(c)}>
          {c.items.map((it) => (
            <RowItem key={it.id}>
              <PosterCard item={it} />
            </RowItem>
          ))}
        </Row>
      ))}
    </div>
  )
}

/** Saves the filters someone has on a library page as a smart collection. */
export function SaveAsCollectionButton({ filter, library }: { filter: ItemFilterJSON; library: number }) {
  const navigate = useNavigate()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const [open, setOpen] = useState(false)
  if (me?.guest) return null
  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)} icon={<Sparkles className="size-4" />}>
        Save as collection
      </Button>
      {open && (
        <CollectionEditor
          start={{ ...filter, library: [library] }}
          library={library}
          onClose={() => setOpen(false)}
          onSaved={(c) => navigate(collectionLink(c))}
        />
      )}
    </>
  )
}
