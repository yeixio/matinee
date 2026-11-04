import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Film, RefreshCw, Search, Unlink, Wand2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Item, type MatchCandidate } from '../api'
import { Dialog } from './Collections'
import { Button, inputClass, Spinner } from './ui'

// MatchPicker lets an admin fix a movie's or show's online match: search
// for the right title (or paste its TMDB or IMDb link) and pick it, refresh
// its details, or unmatch it so it goes by its file name. A match picked
// or cleared here stays: scans leave it alone.
export function MatchPicker({ item, onClose }: { item: Item; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [text, setText] = useState(item.title)
  const [yearText, setYearText] = useState(item.year ? String(item.year) : '')
  const [search, setSearch] = useState({ q: '', year: undefined as number | undefined })
  const found = useQuery({
    queryKey: ['match', item.id, search.q, search.year],
    queryFn: () => api.match(item.id, search.q, search.year),
    staleTime: 0,
  })
  const done = () => {
    // The title's details show on every page that lists it.
    qc.invalidateQueries()
    onClose()
  }
  const pick = useMutation({ mutationFn: (ref: string) => api.setMatch(item.id, ref), onSuccess: done })
  const refresh = useMutation({ mutationFn: () => api.refreshItem(item.id), onSuccess: done })
  const unmatch = useMutation({ mutationFn: () => api.clearMatch(item.id), onSuccess: done })
  const busy = pick.isPending || refresh.isPending || unmatch.isPending
  const error = pick.error ?? refresh.error ?? unmatch.error ?? found.error
  const data = found.data

  return (
    <Dialog title={t('match.title', { title: item.title })} onClose={onClose}>
      <div className="space-y-5">
        <form
          className="flex flex-wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const year = Number(yearText)
            setSearch({ q: text.trim(), year: year > 1800 ? year : undefined })
          }}
        >
          <label className="relative min-w-48 flex-1">
            <span className="sr-only">{t('match.search')}</span>
            <Search className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-faint" />
            <input
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={t('match.searchPlaceholder')}
              className={`${inputClass} ps-9`}
            />
          </label>
          <label className="w-24">
            <span className="sr-only">{t('match.year')}</span>
            <input
              inputMode="numeric"
              value={yearText}
              onChange={(e) => setYearText(e.target.value.replace(/\D/g, '').slice(0, 4))}
              placeholder={t('match.year')}
              className={inputClass}
            />
          </label>
          <Button type="submit" disabled={busy || !text.trim()}>
            {t('match.search')}
          </Button>
        </form>

        {found.isPending ? (
          <div className="flex items-center gap-3 text-sm text-muted">
            <Spinner className="size-4" /> {t('match.looking')}
          </div>
        ) : data && !data.ready ? (
          <p className="text-sm text-muted">{t('match.noKey')}</p>
        ) : !data?.results.length ? (
          !found.error && <p className="text-sm text-muted">{t('match.none')}</p>
        ) : (
          <div className="grid max-h-[50vh] grid-cols-3 gap-3 overflow-y-auto sm:grid-cols-4">
            {data.results.map((c) => (
              <Choice
                key={c.id}
                candidate={c}
                current={c.id === data.tmdbId}
                disabled={busy}
                onPick={() => pick.mutate(String(c.id))}
              />
            ))}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3">
          {(data?.tmdbId || !data?.locked) && (
            <Button icon={<RefreshCw className="size-4" />} disabled={busy} onClick={() => refresh.mutate()}>
              {t('match.refresh')}
            </Button>
          )}
          {!!data?.tmdbId && (
            <Button
              variant="ghost"
              icon={<Unlink className="size-4" />}
              disabled={busy}
              onClick={() => confirm(t('match.confirmUnmatch', { title: item.title })) && unmatch.mutate()}
            >
              {t('match.unmatch')}
            </Button>
          )}
        </div>

        {data?.locked && <p className="text-xs text-faint">{t('match.locked')}</p>}
        {busy && (
          <div className="flex items-center gap-3 text-sm text-muted">
            <Spinner className="size-4" /> {t('match.saving')}
          </div>
        )}
        {error && <p className="text-sm text-danger">{error.message}</p>}
      </div>
    </Dialog>
  )
}

function Choice({
  candidate: c,
  current,
  disabled,
  onPick,
}: {
  candidate: MatchCandidate
  current: boolean
  disabled: boolean
  onPick: () => void
}) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  const label = c.year ? `${c.title} (${c.year})` : c.title
  return (
    <button
      type="button"
      disabled={disabled || current}
      onClick={onPick}
      title={c.overview || label}
      aria-label={current ? `${label} · ${t('match.current')}` : t('match.pick', { title: label })}
      className="group flex flex-col gap-1.5 self-start text-start disabled:cursor-default"
    >
      <div
        className={`relative aspect-[2/3] w-full overflow-hidden rounded-lg bg-raised ring-1 transition ${
          current ? 'ring-2 ring-accent' : 'ring-text/10 group-hover:ring-2 group-hover:ring-accent'
        }`}
      >
        {c.poster && !failed ? (
          <img src={c.poster} alt="" loading="lazy" onError={() => setFailed(true)} className="size-full object-cover" />
        ) : (
          <div className="grid size-full place-items-center text-faint">
            <Film className="size-8" />
          </div>
        )}
        <Check
          className={`absolute end-1.5 top-1.5 size-5 rounded-full bg-accent p-0.5 text-on-accent transition ${
            current ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
          }`}
        />
      </div>
      <div className="line-clamp-2 text-xs font-medium">{c.title}</div>
      <div className="text-xs text-faint">{current ? t('match.current') : (c.year ?? '')}</div>
    </button>
  )
}

// FixMatchButton opens the MatchPicker, for admins on a movie's or show's page.
export function FixMatchButton({ item, open, onOpenChange }: { item: Item; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation()
  return (
    <>
      <Button variant="ghost" icon={<Wand2 className="size-4" />} onClick={() => onOpenChange(true)}>
        {t('match.button')}
      </Button>
      {open && <MatchPicker item={item} onClose={() => onOpenChange(false)} />}
    </>
  )
}
