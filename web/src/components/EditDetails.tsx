import { useMutation, useQueryClient } from '@tanstack/react-query'
import { PencilLine, Undo2 } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type DetailsEdit, type EpisodeOrder, type ItemDetail } from '../api'
import { Dialog } from './Collections'
import { Button, Field, inputClass } from './ui'

// EditDetails lets an admin write a title's details by hand. What they
// change is kept: scans and online lookups leave it alone until they undo
// their edits. A show's or season's name and year come from its folder.
export function EditDetails({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const named = item.kind === 'movie' || item.kind === 'episode'
  const [title, setTitle] = useState(item.title)
  const [year, setYear] = useState(item.year ? String(item.year) : '')
  const [overview, setOverview] = useState(item.overview ?? '')
  const [genres, setGenres] = useState((item.genres ?? []).join(', '))
  const [rating, setRating] = useState(item.rating ? String(Math.round(item.rating * 10) / 10) : '')
  // Shows whose files are numbered from the first episode pick the
  // seasons those go in.
  const [order, setOrder] = useState<EpisodeOrder | undefined>(item.episodeOrder)
  const done = () => {
    qc.invalidateQueries()
    onClose()
  }
  const save = useMutation({
    mutationFn: async (e: DetailsEdit) => {
      if (order && order !== item.episodeOrder) await api.setEpisodeOrder(item.id, order)
      if (Object.keys(e).length > 0) await api.editDetails(item.id, e)
    },
    onSuccess: done,
  })
  const undo = useMutation({ mutationFn: () => api.undoDetailEdits(item.id), onSuccess: done })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    // Only what changed is sent, so only that is locked.
    const edit: DetailsEdit = {}
    if (named && title.trim() && title.trim() !== item.title) edit.title = title.trim()
    const y = Number(year) || 0
    if (named && y !== (item.year ?? 0)) edit.year = y
    if (overview.trim() !== (item.overview ?? '')) edit.overview = overview.trim()
    const g = genres.split(',').map((x) => x.trim()).filter(Boolean)
    if (g.join('|') !== (item.genres ?? []).join('|')) edit.genres = g
    const r = Math.min(10, Math.max(0, Number(rating.replace(',', '.')) || 0))
    if (Math.abs(r - (item.rating ?? 0)) > 0.05) edit.rating = r
    if (Object.keys(edit).length === 0 && order === item.episodeOrder) return onClose()
    save.mutate(edit)
  }
  const busy = save.isPending || undo.isPending
  const error = save.error ?? undo.error

  return (
    <Dialog title={t('editDetails.title', { title: item.title })} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        {named && (
          <div className="flex gap-3">
            <div className="flex-1">
              <Field label={t('editDetails.name')}>
                <input className={inputClass} value={title} onChange={(e) => setTitle(e.target.value)} required />
              </Field>
            </div>
            <div className="w-24">
              <Field label={t('match.year')}>
                <input
                  className={inputClass}
                  inputMode="numeric"
                  value={year}
                  onChange={(e) => setYear(e.target.value.replace(/\D/g, '').slice(0, 4))}
                />
              </Field>
            </div>
          </div>
        )}
        <Field label={t('editDetails.overview')}>
          <textarea
            className={`${inputClass} h-32 py-2 leading-relaxed`}
            value={overview}
            onChange={(e) => setOverview(e.target.value)}
          />
        </Field>
        <Field label={t('editDetails.genres')}>
          <input className={inputClass} value={genres} onChange={(e) => setGenres(e.target.value)} />
        </Field>
        <div className="w-40">
          <Field label={t('editDetails.rating')}>
            <input className={inputClass} inputMode="decimal" value={rating} onChange={(e) => setRating(e.target.value.slice(0, 4))} />
          </Field>
        </div>
        {order && (
          <Field label={t('editDetails.order')}>
            <select className={inputClass} value={order} onChange={(e) => setOrder(e.target.value as EpisodeOrder)}>
              <option value="aired">{t('editDetails.orderAired')}</option>
              <option value="dvd">{t('editDetails.orderDvd')}</option>
              <option value="absolute">{t('editDetails.orderAbsolute')}</option>
            </select>
            <p className="mt-1 text-sm text-muted">{t('editDetails.orderHelp')}</p>
          </Field>
        )}
        <p className="text-sm text-muted">{t('editDetails.locked')}</p>
        {item.localFields?.length ? <p className="text-sm text-muted">{t('editDetails.fromFile')}</p> : null}
        {error && <p className="text-sm text-danger">{error.message}</p>}
        <div className="flex flex-wrap items-center justify-end gap-2">
          {item.lockedFields?.length ? (
            <Button type="button" variant="ghost" icon={<Undo2 className="size-4" />} disabled={busy} onClick={() => undo.mutate()}>
              {t('editDetails.undo')}
            </Button>
          ) : null}
          <span className="flex-1" />
          <Button type="button" variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button variant="primary" disabled={busy}>
            {t('save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

// EditDetailsButton opens EditDetails, for admins on a title's page.
export function EditDetailsButton({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button variant="ghost" icon={<PencilLine className="size-4" />} onClick={() => setOpen(true)}>
        {t('editDetails.button')}
      </Button>
      {open && <EditDetails item={item} onClose={() => setOpen(false)} />}
    </>
  )
}
