import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import { useAudio } from './AudioPlayer'
import { lineAt, lineProgress } from './lyrics'

// How long after someone scrolls the words by hand before they follow the
// song again.
const handsOffMs = 4000

// The playing song's words. Timed words follow the song, the line being
// sung lit up in the middle; picking a line plays from there. Untimed
// words just scroll.
export function LyricsView() {
  const { t } = useTranslation()
  const s = useAudio()
  const item = s.current!
  const { data, isPending } = useQuery({ queryKey: ['lyrics', item.id], queryFn: () => api.lyrics(item.id), staleTime: 5 * 60_000 })
  const lines = data?.lines ?? []
  const synced = !!data?.synced

  // The time, read every frame while timed words are showing, so the
  // lit line moves exactly with the singing; it only re-renders on a change.
  const [now, setNow] = useState({ line: -1, fill: 0 })
  useEffect(() => {
    if (!synced) return
    let frame = 0
    const tick = () => {
      const ms = (s.audio.current?.currentTime ?? 0) * 1000
      const line = lineAt(lines, ms)
      // A pause's dots fill in tenths, which is smooth enough.
      const fill = lines[line]?.text === '' ? Math.round(lineProgress(lines, line, ms) * 10) / 10 : 0
      setNow((n) => (n.line === line && n.fill === fill ? n : { line, fill }))
      frame = requestAnimationFrame(tick)
    }
    tick()
    return () => cancelAnimationFrame(frame)
  }, [synced, lines, s.audio])

  const box = useRef<HTMLDivElement>(null)
  const touched = useRef(0)
  useEffect(() => {
    if (!synced || Date.now() - touched.current < handsOffMs) return
    const el = box.current?.querySelector<HTMLElement>(`[data-line="${Math.max(0, now.line)}"]`)
    const b = box.current
    if (!el || !b) return
    // Inside the box only, so the page itself never moves.
    b.scrollTo({ top: el.offsetTop - b.clientHeight / 2 + el.clientHeight / 2, behavior: 'smooth' })
  }, [synced, now.line])

  if (isPending) return <div className="flex-1" />
  if (lines.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-center text-muted">
        {data?.instrumental ? t('common:music.instrumental') : t('common:music.noLyrics')}
      </div>
    )
  }
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={box}
        onWheel={() => (touched.current = Date.now())}
        onTouchMove={() => (touched.current = Date.now())}
        className="relative min-h-0 flex-1 overflow-y-auto overscroll-contain px-1 py-[30vh] [mask-image:linear-gradient(transparent,black_15%,black_85%,transparent)]"
        aria-label={t('common:music.lyrics')}
      >
        {synced ? (
          <ol className="space-y-4">
            {lines.map((l, i) => {
              const state = i === now.line ? 'now' : i < now.line ? 'past' : 'next'
              return (
                <li key={i} data-line={i}>
                  <button
                    onClick={() => {
                      touched.current = 0
                      s.seek((l.ms ?? 0) / 1000)
                    }}
                    aria-current={state === 'now' ? 'true' : undefined}
                    className={`block w-full origin-left text-start text-2xl leading-snug font-semibold transition duration-300 rtl:origin-right md:text-3xl ${
                      state === 'now' ? 'scale-100 text-text' : state === 'past' ? 'scale-95 text-text/35 hover:text-text/70' : 'scale-95 text-text/50 hover:text-text/80'
                    }`}
                  >
                    {l.text || <Pause on={state === 'now'} fill={state === 'now' ? now.fill : state === 'past' ? 1 : 0} />}
                  </button>
                </li>
              )
            })}
          </ol>
        ) : (
          <div className="space-y-1 text-xl leading-relaxed font-medium text-text/85">
            {lines.map((l, i) => (l.text ? <p key={i}>{l.text}</p> : <div key={i} className="h-5" />))}
          </div>
        )}
      </div>
      {data?.source === 'online' && <p className="pt-2 text-center text-xs text-faint">{t('common:music.lyricsFromLrclib')}</p>}
    </div>
  )
}

// A pause in the words: three dots that fill in as it goes.
function Pause({ on, fill }: { on: boolean; fill: number }) {
  return (
    <span className={`inline-flex gap-2 py-2 ${on ? '' : 'opacity-60'}`} aria-label="♪">
      {[0, 1, 2].map((d) => (
        <span key={d} className="size-2.5 rounded-full bg-current transition-opacity" style={{ opacity: fill > d / 3 ? 1 : 0.3 }} />
      ))}
    </span>
  )
}
