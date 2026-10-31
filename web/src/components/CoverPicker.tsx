import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ImagePlus, Link2, RotateCcw } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type CoverOption, type Item } from '../api'
import { Dialog } from './Collections'
import { Button, inputClass, Spinner } from './ui'

// CoverPicker lets an admin pick an album's or book's cover: from covers
// found online, an image's address, or an upload. The pick stays until
// they go back to the automatic cover.
export function CoverPicker({ item, onClose }: { item: Item; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const options = useQuery({ queryKey: ['covers', item.id], queryFn: () => api.covers(item.id), staleTime: 0 })
  const [address, setAddress] = useState('')
  const file = useRef<HTMLInputElement>(null)
  const done = () => {
    // The cover shows on every page that lists the album or book.
    qc.invalidateQueries()
    onClose()
  }
  const pick = useMutation({
    mutationFn: (what: { url: string } | Blob) => (what instanceof Blob ? api.uploadCover(item.id, what) : api.setCover(item.id, what.url)),
    onSuccess: done,
  })
  const reset = useMutation({ mutationFn: () => api.resetCover(item.id), onSuccess: done })
  const busy = pick.isPending || reset.isPending
  const error = pick.error ?? reset.error

  return (
    <Dialog title={t('cover.title', { title: item.title })} onClose={onClose}>
      <div className="space-y-5">
        {options.isPending ? (
          <div className="flex items-center gap-3 text-sm text-muted">
            <Spinner className="size-4" /> {t('cover.looking')}
          </div>
        ) : !options.data?.options.length ? (
          <p className="text-sm text-muted">{t('cover.none')}</p>
        ) : (
          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4">
            {options.data.options.map((o) => (
              <CoverChoice key={o.url} option={o} disabled={busy} onPick={() => pick.mutate({ url: o.url })} />
            ))}
          </div>
        )}

        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (address.trim()) pick.mutate({ url: address.trim() })
          }}
        >
          <label className="relative flex-1">
            <span className="sr-only">{t('cover.address')}</span>
            <Link2 className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-faint" />
            <input
              type="url"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder={t('cover.addressPlaceholder')}
              className={`${inputClass} ps-9`}
            />
          </label>
          <Button type="submit" disabled={busy || !address.trim()}>
            {t('cover.use')}
          </Button>
        </form>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <input
            ref={file}
            type="file"
            accept="image/jpeg,image/png,image/webp,image/gif"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) pick.mutate(f)
              e.target.value = ''
            }}
          />
          <Button icon={<ImagePlus className="size-4" />} disabled={busy} onClick={() => file.current?.click()}>
            {t('cover.upload')}
          </Button>
          {options.data?.chosen && (
            <Button variant="ghost" icon={<RotateCcw className="size-4" />} disabled={busy} onClick={() => reset.mutate()}>
              {t('cover.reset')}
            </Button>
          )}
        </div>

        {options.data?.chosen && <p className="text-xs text-faint">{t('cover.chosenNote')}</p>}
        {busy && (
          <div className="flex items-center gap-3 text-sm text-muted">
            <Spinner className="size-4" /> {t('cover.saving')}
          </div>
        )}
        {error && <p className="text-sm text-danger">{error.message}</p>}
      </div>
    </Dialog>
  )
}

function CoverChoice({ option, disabled, onPick }: { option: CoverOption; disabled: boolean; onPick: () => void }) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  if (failed) return null
  const label = [option.title, option.detail, option.source].filter(Boolean).join(' · ')
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onPick}
      title={label}
      aria-label={t('cover.pick', { cover: label })}
      className="group flex flex-col gap-1.5 self-start text-start disabled:opacity-60"
    >
      <div className="relative aspect-square overflow-hidden rounded-lg bg-raised ring-1 ring-text/10 transition group-hover:ring-2 group-hover:ring-accent">
        <img src={option.thumb} alt="" loading="lazy" onError={() => setFailed(true)} className="size-full object-cover" />
        <Check className="absolute end-1.5 top-1.5 size-5 rounded-full bg-accent p-0.5 text-on-accent opacity-0 transition group-hover:opacity-100" />
      </div>
      <div className="truncate text-xs font-medium">{option.source}</div>
      {option.detail && <div className="truncate text-xs text-faint">{option.detail}</div>}
    </button>
  )
}
