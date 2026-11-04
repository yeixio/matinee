import { useQuery } from '@tanstack/react-query'
import { Film, MessageSquareQuote, Play, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, extraFileUrl, extraImageUrl, type Extra, type ItemDetail } from '../api'
import { clock } from '../format'
import { commentaries, languageName } from '../media'
import { Row } from './Row'
import { LocalClip } from './Trailer'

// The title page's special features: commentary tracks inside the title's
// own file, which play the title with that track, then extras from files
// next to it (behind the scenes, deleted scenes, featurettes and so on),
// which play over the page like a trailer.
export function ExtrasRow({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Extra>()
  const has = item.kind === 'movie' || item.kind === 'show' || item.kind === 'season'
  const { data: extras } = useQuery({
    queryKey: ['extras', item.id],
    queryFn: () => api.extras(item.id),
    enabled: has,
    // Servers from before extras answer 404; there's simply no row.
    retry: false,
    staleTime: 10 * 60_000,
  })
  const tracks = item.playable ? commentaries(item.mediaInfo) : []
  if (!tracks.length && !extras?.length) return null
  return (
    <>
      <Row title={t('common:extras.title')} wide>
        {tracks.map((a) => (
          <Link
            key={`audio-${a.index}`}
            // This file's track, so this file plays, whichever version suits the screen.
            to={`/watch/${item.id}?audio=${a.index}${item.versions && item.versions.length > 1 ? '&v=1' : ''}`}
            aria-label={t('common:extras.playWith', { title: item.title, track: a.title || t('common:extras.commentary') })}
            className="group block w-64 shrink-0 snap-start self-start md:w-80"
          >
            <div className="relative grid aspect-video place-items-center overflow-hidden rounded-xl bg-linear-to-br from-accent/20 via-surface to-surface ring-1 ring-text/10 transition group-hover:ring-accent/70">
              <MessageSquareQuote className="size-10 text-accent" />
              <PlayBadge />
            </div>
            <Caption name={a.title || t('common:extras.commentary')} detail={[a.title ? t('common:extras.commentary') : '', languageName(a.language)]} />
          </Link>
        ))}
        {extras?.map((e) => (
          <button key={e.id} onClick={() => setOpen(e)} className="group block w-64 shrink-0 snap-start self-start text-start md:w-80">
            <div className="relative aspect-video overflow-hidden rounded-xl bg-surface ring-1 ring-text/10 transition group-hover:ring-accent/70">
              <Still item={item} extra={e} />
              <PlayBadge />
              {!!e.durationMs && (
                <span dir="ltr" className="absolute end-2 bottom-2 rounded bg-black/70 px-1.5 py-0.5 text-xs text-white tabular-nums">
                  {clock(e.durationMs / 1000)}
                </span>
              )}
            </div>
            <Caption name={extraName(e, t)} detail={[e.name ? t(`common:extras.kinds.${e.kind}`) : '']} />
          </button>
        ))}
      </Row>
      {open && <ExtraDialog item={item} extra={open} onClose={() => setOpen(undefined)} />}
    </>
  )
}

export const extraName = (e: Extra, t: (key: string) => string) => e.name || t(`common:extras.kinds.${e.kind}`)

function Still({ item, extra }: { item: ItemDetail; extra: Extra }) {
  const [failed, setFailed] = useState(false)
  if (failed) {
    return (
      <div className="grid size-full place-items-center">
        <Film className="size-10 text-faint" />
      </div>
    )
  }
  return (
    <img
      src={extraImageUrl(item.id, extra.id)}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
      className="size-full object-cover transition duration-300 group-hover:scale-[1.03]"
    />
  )
}

function PlayBadge() {
  return (
    <span className="absolute inset-0 grid place-items-center opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
      <span className="grid size-11 place-items-center rounded-full bg-black/60 backdrop-blur">
        <Play className="size-4 translate-x-0.5 fill-white text-white" />
      </span>
    </span>
  )
}

function Caption({ name, detail }: { name: string; detail: string[] }) {
  const line = detail.filter(Boolean).join(' · ')
  return (
    <>
      <div className="mt-2 line-clamp-1 text-sm font-medium group-hover:text-accent-strong">{name}</div>
      {line && <div className="mt-0.5 line-clamp-1 text-xs text-muted">{line}</div>}
    </>
  )
}

function ExtraDialog({ item, extra, onClose }: { item: ItemDetail; extra: Extra; onClose: () => void }) {
  const { t } = useTranslation()
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [onClose])
  const name = extraName(extra, t)
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`${item.title} · ${name}`}
      className="animate-fade fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 bg-ink/95 p-4 backdrop-blur md:p-10"
      onClick={onClose}
    >
      <div className="flex w-full max-w-6xl items-center justify-between gap-4" onClick={(e) => e.stopPropagation()}>
        <div className="min-w-0">
          <div className="truncate font-display text-lg font-semibold">{item.title}</div>
          <div className="truncate text-sm text-muted">{name}</div>
        </div>
        <button onClick={onClose} aria-label={t('close')} className="grid size-10 shrink-0 place-items-center rounded-full bg-text/10 hover:bg-text/20">
          <X className="size-5" />
        </button>
      </div>
      <div
        className="aspect-video w-full max-w-6xl overflow-hidden rounded-xl bg-black shadow-2xl ring-1 ring-text/10"
        style={{ maxHeight: 'calc(100vh - 12rem)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <LocalClip
          key={extra.id}
          clip={extra}
          fileUrl={extraFileUrl(item.id, extra.id)}
          startHls={() => api.startExtraHls(item.id, extra.id)}
          wontPlay={t('common:extras.wontPlay')}
          onEnded={onClose}
        />
      </div>
    </div>
  )
}
