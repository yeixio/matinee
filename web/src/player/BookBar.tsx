import { ChevronUp, Gauge, ListOrdered, Pause, Play, RotateCcw, RotateCw, SkipBack, SkipForward, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Artwork } from '../components/Artwork'
import { clock } from '../format'
import { useAudio, useBookTime, type AudioState } from './AudioPlayer'
import * as B from './book'
import { VolumeControl } from './MusicBar'

// The audiobook player along the bottom of every page: the book, the
// chapter, and the buttons people use most while listening.
export function BookBar() {
  const { t } = useTranslation()
  const s = useAudio()
  const book = s.book!
  const { time, chapter } = useBookTime()
  const ch = book.chapters[chapter]
  const start = ch ? ch.startMs / 1000 : 0
  const end = ch ? ch.endMs / 1000 : B.bookLength(book)
  const open = () => s.setExpanded(true)
  return (
    <div className="fixed inset-x-0 bottom-[calc(57px+env(safe-area-inset-bottom))] z-30 border-t border-line/60 bg-surface/95 backdrop-blur md:bottom-0">
      <div className="absolute inset-x-0 top-0 h-0.5 bg-text/10 md:hidden">
        <div className="h-full bg-accent" style={{ width: `${end > start ? ((time - start) / (end - start)) * 100 : 0}%` }} />
      </div>
      {s.notice && (
        <div role="status" className="absolute -top-12 start-1/2 -translate-x-1/2 w-max max-w-[calc(100vw-2rem)] animate-fade rounded-full bg-text px-4 py-2 text-center text-sm font-medium text-ink shadow-lg rtl:translate-x-1/2">
          {s.notice}
        </div>
      )}
      <div className="flex h-16 items-center gap-3 px-3 md:grid md:h-20 md:grid-cols-[1fr_minmax(0,36rem)_1fr] md:gap-6 md:px-6">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <button onClick={open} className="group relative shrink-0" aria-label={t('common:books.openPlayer')}>
            <Artwork item={book} type="poster" label={false} className="size-11 rounded-md shadow-md md:size-14" />
            <span className="absolute inset-0 hidden place-items-center rounded-md bg-black/50 opacity-0 transition group-hover:opacity-100 md:grid">
              <ChevronUp className="size-5 text-white" />
            </span>
          </button>
          <div className="min-w-0 flex-1">
            <button onClick={open} className="block w-full truncate text-start text-sm font-medium md:hidden">
              {book.title}
            </button>
            <Link to={`/item/${book.id}`} className="hidden truncate text-sm font-medium hover:underline md:block">
              {book.title}
            </Link>
            <div className="truncate text-xs text-muted">{ch?.title || book.author}</div>
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-center gap-1">
          <BookControls s={s} compact />
          <div className="hidden w-full items-center gap-2 text-[11px] text-muted tabular-nums md:flex">
            <span className="w-12 text-end">{clock(time - start)}</span>
            <Scrubber value={time - start} max={end - start} onSeek={(v) => s.seekBook(start + v)} />
            <span className="w-12">-{clock(B.timeLeft(time, end, s.speed))}</span>
          </div>
        </div>

        <div className="hidden items-center justify-end gap-1 md:flex">
          <SpeedMenu s={s} />
          <button
            onClick={open}
            className="rounded-full p-2 text-muted hover:bg-text/5 hover:text-text"
            aria-label={t('common:books.chapters')}
            title={t('common:books.chapters')}
          >
            <ListOrdered className="size-5" />
          </button>
          <VolumeControl s={s} />
          <button onClick={s.stop} className="rounded-full p-2 text-faint hover:bg-text/5 hover:text-text" aria-label={t('player.closePlayer')} title={t('player.closePlayer')}>
            <X className="size-5" />
          </button>
        </div>
      </div>
    </div>
  )
}

// Previous chapter, back, play, ahead and next chapter. compact leaves back,
// play and ahead on phones.
export function BookControls({ s, compact = false, large = false }: { s: AudioState; compact?: boolean; large?: boolean }) {
  const { t } = useTranslation()
  const { time } = useBookTime()
  const lastChapter = !!s.book && B.nextChapterStart(s.book.chapters, time) === undefined
  const icon = large ? 'size-7' : 'size-5'
  const chapterBtn = `rounded-full p-2 text-muted transition-colors hover:text-text disabled:opacity-30 ${compact ? 'hidden md:block' : ''}`
  const jump = (sec: number, Icon: typeof RotateCcw, label: string) => (
    <button onClick={() => s.skip(sec)} className="relative rounded-full p-2 text-muted transition-colors hover:text-text" aria-label={label} title={label}>
      <Icon className={large ? 'size-9' : 'size-7'} strokeWidth={1.5} />
      <span className={`absolute inset-0 grid place-items-center pt-0.5 font-semibold tabular-nums ${large ? 'text-[11px]' : 'text-[9px]'}`}>{Math.abs(sec)}</span>
    </button>
  )
  return (
    <div className={`flex items-center ${large ? 'gap-5' : 'gap-1 md:gap-2'}`}>
      <button onClick={s.prev} className={chapterBtn} aria-label={t('common:books.previousChapter')} title={t('common:books.previousChapter')}>
        <SkipBack className={`${icon} fill-current`} />
      </button>
      {jump(-B.SKIP_BACK, RotateCcw, t('common:books.backSeconds', { seconds: B.SKIP_BACK }))}
      <button
        onClick={s.toggle}
        className={`grid place-items-center rounded-full bg-text text-ink transition-transform hover:scale-105 ${large ? 'size-16' : 'size-10'}`}
        aria-label={s.playing ? t('player.pause') : t('item.play')}
      >
        {s.playing ? <Pause className={`${icon} fill-current`} /> : <Play className={`${icon} translate-x-0.5 fill-current`} />}
      </button>
      {jump(B.SKIP_AHEAD, RotateCw, t('common:books.aheadSeconds', { seconds: B.SKIP_AHEAD }))}
      <button onClick={s.next} disabled={lastChapter} className={chapterBtn} aria-label={t('common:books.nextChapter')} title={t('common:books.nextChapter')}>
        <SkipForward className={`${icon} fill-current`} />
      </button>
    </div>
  )
}

// A bar to see and change where in the chapter it is.
export function Scrubber({ value, max, onSeek }: { value: number; max: number; onSeek: (v: number) => void }) {
  const { t } = useTranslation()
  const [drag, setDrag] = useState<number>()
  const v = Math.max(0, Math.min(drag ?? value, max))
  const done = () => {
    if (drag !== undefined) onSeek(drag)
    setDrag(undefined)
  }
  return (
    <input
      type="range"
      min={0}
      max={max || 0}
      step={1}
      value={v}
      onChange={(e) => setDrag(Number(e.target.value))}
      onPointerUp={done}
      onKeyUp={done}
      aria-label={t('player.seek')}
      className="h-3 w-full flex-1 cursor-pointer appearance-none bg-transparent accent-accent"
      style={{ background: `linear-gradient(to right, var(--color-accent) ${max ? (v / max) * 100 : 0}%, color-mix(in srgb, var(--color-text) 15%, transparent) 0) center / 100% 4px no-repeat` }}
    />
  )
}

// How fast the book plays, remembered for next time.
export function SpeedMenu({ s, large = false }: { s: AudioState; large?: boolean }) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])
  const label = B.speedLabel(s.speed, i18n.language)
  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${t('common:books.speed')}: ${label}`}
        title={t('common:books.speed')}
        className={`flex items-center gap-1.5 rounded-full px-2.5 py-2 hover:bg-text/10 ${s.speed !== 1 ? 'text-accent' : 'text-muted hover:text-text'}`}
      >
        <Gauge className={large ? 'size-6' : 'size-5'} />
        <span className="text-sm font-medium tabular-nums">{label}</span>
      </button>
      {open && (
        <ul role="menu" className="absolute end-0 bottom-full z-10 mb-2 w-32 rounded-xl border border-line bg-surface p-1.5 shadow-2xl">
          {B.SPEEDS.map((v) => (
            <li key={v}>
              <button
                role="menuitemradio"
                aria-checked={v === s.speed}
                onClick={() => {
                  s.setSpeed(v)
                  setOpen(false)
                }}
                className={`w-full rounded-md px-3 py-1.5 text-start text-sm tabular-nums hover:bg-text/10 ${v === s.speed ? 'font-semibold text-accent' : ''}`}
              >
                {B.speedLabel(v, i18n.language)}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
