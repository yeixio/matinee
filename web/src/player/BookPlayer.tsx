import { ChevronDown, FileText, ListOrdered } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Artwork } from '../components/Artwork'
import { Equalizer } from '../components/Tracks'
import { clock, duration } from '../format'
import { useAudio, useBookTime } from './AudioPlayer'
import * as B from './book'
import { BookControls, Scrubber, SpeedMenu } from './BookBar'
import { VolumeControl } from './MusicBar'
import { SleepMenu } from './SleepMenu'
import { useSleep } from './sleep'

// The full audiobook player: the cover large, the chapter and how much is
// left, the controls, speed and sleep timer, and every chapter to jump to.
export function BookPlayer() {
  const { t } = useTranslation()
  const s = useAudio()
  const book = s.book!
  const { time, dur, chapter } = useBookTime()
  const { sleep, left } = useSleep()
  const ch = book.chapters[chapter]
  const start = ch ? ch.startMs / 1000 : 0
  const end = ch ? ch.endMs / 1000 : dur
  // Phones show the cover or the chapters; wider screens show both.
  const [showChapters, setShowChapters] = useState(false)
  const close = () => s.setExpanded(false)
  // A podcast episode has show notes where a book has chapters.
  const episode = B.isEpisode(book)
  const sideLabel = episode ? t('common:podcasts.showNotes') : t('common:books.chapters')

  useEffect(() => {
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && close()
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
    <div role="dialog" aria-modal="true" aria-label={t('common:books.nowListening')} className="fixed inset-0 z-40 flex animate-fade flex-col overflow-hidden bg-ink">
      <div className="pointer-events-none absolute inset-0 opacity-40">
        <Artwork item={book} type="poster" label={false} className="size-full scale-125 blur-3xl" />
      </div>
      <div className="absolute inset-0 bg-gradient-to-b from-ink/30 via-ink/70 to-ink" />

      <header className="relative flex items-center gap-2 px-3 pt-[max(env(safe-area-inset-top),0.75rem)] md:px-6 md:pt-5">
        <button onClick={close} className="rounded-full p-2 text-muted hover:bg-text/10 hover:text-text" aria-label={t('common:music.minimize')} title={t('common:music.minimize')}>
          <ChevronDown className="size-6" />
        </button>
        <div className="flex-1 text-center text-xs font-semibold tracking-widest text-muted uppercase">{t('common:books.nowListening')}</div>
        <button
          onClick={() => setShowChapters(!showChapters)}
          aria-pressed={showChapters}
          className={`rounded-full p-2 hover:bg-text/10 lg:invisible ${showChapters ? 'text-accent' : 'text-muted'}`}
          aria-label={sideLabel}
          title={sideLabel}
        >
          {episode ? <FileText className="size-6" /> : <ListOrdered className="size-6" />}
        </button>
      </header>

      <div className="relative flex min-h-0 flex-1 justify-center gap-16 px-6 pb-[max(env(safe-area-inset-bottom),1.5rem)] md:px-12 lg:px-16">
        <section className={`mx-auto min-h-0 w-full max-w-md flex-col justify-center gap-5 lg:mx-0 lg:flex lg:max-w-lg lg:flex-1 ${showChapters ? 'hidden' : 'flex'}`}>
          <Artwork item={book} type="poster" className="mx-auto aspect-square w-full max-w-sm shrink rounded-2xl shadow-2xl shadow-black/60 ring-1 ring-text/10" />
          <div className="min-w-0 space-y-1 text-center">
            <h2 className="truncate text-2xl font-semibold md:text-3xl">{book.title}</h2>
            <p className="truncate text-muted">
              {episode ? link(`/item/${book.parentId}`, book.podcast) : link(`/item/${book.authorId}`, book.authorId ? book.author : undefined)}
            </p>
          </div>
          <div className="space-y-1">
            <div className="truncate text-center text-sm font-medium">{ch?.title}</div>
            <Scrubber value={time - start} max={end - start} onSeek={(v) => s.seekBook(start + v)} />
            <div className="flex justify-between text-xs text-muted tabular-nums">
              <span>{clock(time - start)}</span>
              <span>-{clock(B.timeLeft(time, end, s.speed))}</span>
            </div>
            {!episode && <div className="text-center text-xs text-muted">{t('common:books.leftInBook', { time: duration(B.timeLeft(time, dur, s.speed) * 1000) })}</div>}
          </div>
          <div className="flex justify-center">
            <BookControls s={s} large />
          </div>
          <div className="flex items-center justify-center gap-2">
            <SpeedMenu s={s} large />
            <SleepMenu sleep={sleep} left={left} kind={episode ? 'episode' : 'chapter'} onOpenChange={() => {}} />
            <div className="hidden md:block">
              <VolumeControl s={s} />
            </div>
          </div>
        </section>

        <section className={`min-h-0 w-full max-w-md flex-col lg:flex lg:max-w-xl lg:flex-1 lg:pt-4 ${showChapters ? 'mx-auto flex' : 'hidden'}`}>
          <h3 className="mb-2 px-2 text-sm font-semibold tracking-wide text-muted uppercase">{sideLabel}</h3>
          {episode ? (
            <div className="scrollbar-none min-h-0 flex-1 overflow-y-auto px-2 pb-6 text-sm leading-relaxed whitespace-pre-line text-muted">
              {book.overview || t('common:podcasts.noShowNotes')}
            </div>
          ) : (
            <Chapters onPick={() => setShowChapters(false)} />
          )}
        </section>
      </div>
    </div>
  )
}

// Every chapter, with the one playing marked; picking one plays from there.
function Chapters({ onPick }: { onPick: () => void }) {
  const s = useAudio()
  const { chapter } = useBookTime()
  const playingRef = useRef<HTMLLIElement>(null)
  useEffect(() => playingRef.current?.scrollIntoView({ block: 'center' }), [])
  const chapters = s.book?.chapters ?? []
  return (
    <ol className="scrollbar-none min-h-0 flex-1 overflow-y-auto pb-6">
      {chapters.map((c, i) => (
        <li key={`${c.partId}-${c.startMs}`} ref={i === chapter ? playingRef : undefined}>
          <button
            onClick={() => {
              s.seekBook(c.startMs / 1000)
              if (!s.playing) s.toggle()
              onPick()
            }}
            aria-current={i === chapter}
            className={`flex w-full items-center gap-3 rounded-lg px-2 py-2.5 text-start hover:bg-text/5 ${i === chapter ? 'text-accent' : ''}`}
          >
            <span className="w-7 shrink-0 text-end text-sm text-muted tabular-nums">{i === chapter ? <Equalizer playing={s.playing} /> : i + 1}</span>
            <span className="min-w-0 flex-1 truncate text-sm font-medium">{c.title}</span>
            <span className="shrink-0 text-xs text-muted tabular-nums">{clock((c.endMs - c.startMs) / 1000)}</span>
          </button>
        </li>
      ))}
    </ol>
  )
}
