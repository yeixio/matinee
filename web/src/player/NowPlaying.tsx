import { AudioWaveform, ChevronDown, GripVertical, ListMusic, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Artwork } from '../components/Artwork'
import { Equalizer } from '../components/Tracks'
import { clock } from '../format'
import { useAudio, useAudioTime } from './AudioPlayer'
import { Controls, Seek, VolumeControl } from './MusicBar'
import { upNext } from './queue'
import { Visualizer } from './Visualizer'

// The full player: the cover large, the controls, and what's coming up,
// which can be reordered by dragging, or played or removed.
export function NowPlaying() {
  const { t } = useTranslation()
  const s = useAudio()
  const { time, dur } = useAudioTime()
  const item = s.current!
  // Phones show the cover or the queue; wider screens show both.
  const [showQueue, setShowQueue] = useState(false)
  const [visual, setVisual] = useState(false)
  const close = () => s.setExpanded(false)

  useEffect(() => {
    // The visualizer closes itself first.
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && !visual && close()
    window.addEventListener('keydown', esc)
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', esc)
      document.body.style.overflow = ''
    }
  })

  const link = (to: string, text?: string) =>
    text ? (
      <Link to={to} onClick={close} className="hover:text-text hover:underline">
        {text}
      </Link>
    ) : null

  return (
    <div role="dialog" aria-modal="true" aria-label={t('common:music.nowPlaying')} className="fixed inset-0 z-40 flex animate-fade flex-col overflow-hidden bg-ink">
      {/* The cover, blurred, tints the whole player. */}
      <div className="pointer-events-none absolute inset-0 opacity-40">
        <Artwork item={item} type="poster" label={false} className="size-full scale-125 blur-3xl" />
      </div>
      <div className="absolute inset-0 bg-gradient-to-b from-ink/30 via-ink/70 to-ink" />

      <header className="relative flex items-center gap-2 px-3 pt-[max(env(safe-area-inset-top),0.75rem)] md:px-6 md:pt-5">
        <button onClick={close} className="rounded-full p-2 text-muted hover:bg-text/10 hover:text-text" aria-label={t('common:music.minimize')} title={t('common:music.minimize')}>
          <ChevronDown className="size-6" />
        </button>
        {/* As wide as the visualizer button, so the title stays in the middle. */}
        <div className="w-10" />
        <div className="flex-1 text-center text-xs font-semibold tracking-widest text-muted uppercase">{t('common:music.nowPlaying')}</div>
        <button
          onClick={() => setVisual(true)}
          className="rounded-full p-2 text-muted hover:bg-text/10 hover:text-text"
          aria-label={t('common:music.visualizer')}
          title={t('common:music.visualizer')}
        >
          <AudioWaveform className="size-6" />
        </button>
        <button
          onClick={() => setShowQueue(!showQueue)}
          aria-pressed={showQueue}
          className={`rounded-full p-2 hover:bg-text/10 lg:invisible ${showQueue ? 'text-accent' : 'text-muted'}`}
          aria-label={t('common:music.queue')}
          title={t('common:music.queue')}
        >
          <ListMusic className="size-6" />
        </button>
      </header>

      <div className="relative flex min-h-0 flex-1 justify-center gap-16 px-6 pb-[max(env(safe-area-inset-bottom),1.5rem)] md:px-12 lg:px-16">
        <section className={`mx-auto min-h-0 w-full max-w-md flex-col justify-center gap-6 lg:mx-0 lg:flex lg:max-w-lg lg:flex-1 ${showQueue ? 'hidden' : 'flex'}`}>
          <Artwork item={item} type="poster" className="aspect-square w-full shrink rounded-2xl shadow-2xl shadow-black/60 ring-1 ring-text/10" />
          <div className="min-w-0 space-y-1">
            <h2 className="truncate text-2xl font-semibold md:text-3xl">{item.title}</h2>
            <p className="truncate text-muted">
              {link(`/item/${item.artistId}`, item.artistId ? item.artist : undefined)}
              {item.artist && item.album && ' · '}
              {link(`/item/${item.parentId}`, item.parentId ? item.album : undefined)}
            </p>
          </div>
          <div className="space-y-1">
            <Seek s={s} time={time} dur={dur} />
            <div className="flex justify-between text-xs text-muted tabular-nums">
              <span>{clock(time)}</span>
              <span>-{clock(Math.max(0, dur - time))}</span>
            </div>
          </div>
          <div className="flex justify-center">
            <Controls s={s} large />
          </div>
          <div className="hidden justify-center md:flex">
            <VolumeControl s={s} />
          </div>
        </section>

        <section className={`min-h-0 w-full max-w-md flex-col lg:flex lg:max-w-xl lg:flex-1 lg:pt-4 ${showQueue ? 'mx-auto flex' : 'hidden'}`}>
          <Queue />
        </section>
      </div>
      {visual && <Visualizer onClose={() => setVisual(false)} />}
    </div>
  )
}

function Queue() {
  const { t } = useTranslation()
  const s = useAudio()
  const next = upNext(s.queue)
  const [dragging, setDragging] = useState<number>()
  const [over, setOver] = useState<number>()
  const item = s.current!
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div>
        <h3 className="mb-2 text-sm font-semibold text-muted">{t('common:music.nowPlaying')}</h3>
        <div className="flex items-center gap-3 rounded-lg bg-text/5 p-2">
          <Artwork item={item} type="poster" label={false} className="size-11 rounded-md" />
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium text-accent">{item.title}</div>
            <div className="truncate text-xs text-muted">{item.artist}</div>
          </div>
          <Equalizer playing={s.playing} className="me-2" />
        </div>
      </div>
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-muted">{t('common:music.upNext')}</h3>
        {next.length > 0 && (
          <button onClick={s.clearUpNext} className="rounded-full px-3 py-1 text-xs text-muted hover:bg-text/10 hover:text-text">
            {t('common:music.clearQueue')}
          </button>
        )}
      </div>
      {next.length === 0 ? (
        <p className="text-sm text-faint">{t('common:music.queueEnd')}</p>
      ) : (
        <ol className="-mx-2 min-h-0 flex-1 space-y-0.5 overflow-y-auto px-2">
          {next.map((e, i) => {
            const pos = s.queue.pos + 1 + i
            return (
              <li
                key={e.key}
                draggable
                onDragStart={() => setDragging(pos)}
                onDragEnd={() => {
                  setDragging(undefined)
                  setOver(undefined)
                }}
                onDragOver={(ev) => {
                  ev.preventDefault()
                  setOver(pos)
                }}
                onDrop={() => {
                  if (dragging !== undefined) s.move(dragging, pos)
                  setDragging(undefined)
                  setOver(undefined)
                }}
                className={`group flex items-center gap-2 rounded-lg p-1.5 hover:bg-text/5 ${dragging === pos ? 'opacity-40' : ''} ${
                  over === pos && dragging !== pos ? 'ring-1 ring-accent/60' : ''
                }`}
              >
                <GripVertical className="hidden size-4 shrink-0 cursor-grab text-faint md:block" aria-hidden />
                <button onClick={() => s.jump(pos)} className="flex min-w-0 flex-1 items-center gap-3 text-start">
                  <Artwork item={e.item} type="poster" label={false} className="size-10 shrink-0 rounded-md" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm">{e.item.title}</span>
                    <span className="block truncate text-xs text-muted">{e.item.artist}</span>
                  </span>
                </button>
                <span className="text-xs text-faint tabular-nums">{e.item.durationMs ? clock(e.item.durationMs / 1000) : ''}</span>
                <button
                  onClick={() => s.remove(e.key)}
                  className="rounded-full p-1.5 text-faint opacity-100 hover:bg-text/10 hover:text-text md:opacity-0 md:group-hover:opacity-100 md:focus:opacity-100"
                  aria-label={t('common:music.remove')}
                  title={t('common:music.remove')}
                >
                  <X className="size-4" />
                </button>
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}
