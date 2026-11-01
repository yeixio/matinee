import { Captions, Check, Search, X } from 'lucide-react'
import { Fragment, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SubtitleTrack } from '../api'
import { subtitleLabel } from '../media'
import { FindSubtitles } from './FindSubtitles'
import { activeCues, spans, type Cue } from './vtt'

const prefKey = 'matinee.subtitles'

// The viewer's last choice: a language code, "" for a track without one, or "off".
export function savedPreference(): string | null {
  try {
    return localStorage.getItem(prefKey)
  } catch {
    return null
  }
}

export function savePreference(track: SubtitleTrack | undefined) {
  try {
    localStorage.setItem(prefKey, track ? (track.language ?? '') : 'off')
  } catch {
    // private mode: just don't remember
  }
}

// defaultTrack picks what to show when playback starts: the language the
// viewer chose last time (so a series keeps its subtitles from episode to
// episode), otherwise a forced track, which exists to be shown.
export function defaultTrack(tracks: SubtitleTrack[]): SubtitleTrack | undefined {
  const pref = savedPreference()
  if (pref !== null && pref !== 'off') {
    const same = tracks.filter((t) => (t.language ?? '') === pref && !t.forced)
    // Text over pictures (no re-encode), plain over SDH.
    same.sort((a, b) => Number(!!a.burnIn) - Number(!!b.burnIn) || Number(!!a.sdh) - Number(!!b.sdh))
    if (same[0]) return same[0]
  }
  if (pref === 'off') return undefined
  return tracks.find((t) => t.forced && t.default) ?? tracks.find((t) => t.forced)
}

// SubtitleOverlay draws the cues showing at the current position. It reads
// the clock every frame, so lines appear on time rather than at the next
// timeupdate event.
export function SubtitleOverlay({ cues, position, lifted }: { cues: Cue[]; position: () => number; lifted: boolean }) {
  const [shown, setShown] = useState<Cue[]>([])
  useEffect(() => {
    let frame = 0
    const tick = () => {
      const now = activeCues(cues, position())
      setShown((prev) => (prev.length === now.length && prev.every((c, i) => c === now[i]) ? prev : now))
      frame = requestAnimationFrame(tick)
    }
    tick()
    return () => cancelAnimationFrame(frame)
  }, [cues, position])

  if (!shown.length) return null
  return (
    <div
      aria-live="off"
      className={`pointer-events-none absolute inset-x-0 flex flex-col items-center gap-1 px-[6%] text-center transition-[bottom] duration-300 ${
        lifted ? 'bottom-36 md:bottom-40' : 'bottom-[7%]'
      }`}
    >
      {shown.map((cue, i) => (
        <div
          key={`${cue.start}-${i}`}
          dir="auto"
          className="rounded bg-black/55 px-[0.4em] py-[0.1em] leading-snug font-medium whitespace-pre-line text-white [text-shadow:0_1px_3px_rgb(0_0_0/0.9)]"
          style={{ fontSize: 'clamp(1rem, 2.4vw + 0.35rem, 2.75rem)' }}
        >
          {spans(cue.text).map((s, j) => (
            <Fragment key={j}>
              {s.italic || s.bold || s.underline ? (
                <span className={`${s.italic ? 'italic' : ''} ${s.bold ? 'font-bold' : ''} ${s.underline ? 'underline' : ''}`}>{s.text}</span>
              ) : (
                s.text
              )}
            </Fragment>
          ))}
        </div>
      ))}
    </div>
  )
}

type MenuProps = {
  tracks: SubtitleTrack[]
  selected?: string
  onSelect: (track: SubtitleTrack | undefined) => void
  onOpenChange: (open: boolean) => void
  /** Set when the viewer can look for more subtitles online. */
  find?: {
    itemId: number
    /** A file found online was added: show it. */
    onAdded: (track: SubtitleTrack) => void
    /** A found file was removed. */
    onRemove: (track: SubtitleTrack) => void
  }
}

// SubtitleMenu is the captions button and its list of tracks, ending in
// Find subtitles when the server can look for more online.
export function SubtitleMenu({ tracks, selected, onSelect, onOpenChange, find }: MenuProps) {
  const { t: tr } = useTranslation()
  const [open, setOpen] = useState(false)
  const [finding, setFinding] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const toggle = (v: boolean) => {
    setOpen(v)
    if (!v) setFinding(false)
    onOpenChange(v)
  }

  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && toggle(false)
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  })

  const choose = (t: SubtitleTrack | undefined) => {
    onSelect(t)
    toggle(false)
  }
  const item = (key: string, label: string, active: boolean, t: SubtitleTrack | undefined, note?: string) => (
    <li key={key} className="flex items-center">
      <button
        role="menuitemradio"
        aria-checked={active}
        onClick={() => choose(t)}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-white/10"
      >
        <Check className={`size-4 shrink-0 ${active ? 'text-accent' : 'invisible'}`} />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {note && <span className="shrink-0 text-xs text-white/50">{note}</span>}
      </button>
      {t?.removable && find && (
        <button
          onClick={() => find.onRemove(t)}
          className="rounded-full p-1.5 text-white/50 hover:bg-white/10 hover:text-white"
          aria-label={tr('player.findSubtitles.remove', { name: label })}
          title={tr('player.findSubtitles.remove', { name: label })}
        >
          <X className="size-4" />
        </button>
      )}
    </li>
  )
  const note = (t: SubtitleTrack) =>
    t.burnIn ? tr('player.convertsVideo') : t.found ? tr('player.findSubtitles.online') : t.external ? tr('player.file') : undefined

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className={`rounded-full p-2 hover:bg-white/10 ${selected ? 'text-accent' : ''}`}
        aria-label={tr('file.subtitles')}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <Captions className="size-6" />
      </button>
      {open && (
        <div
          className={`absolute end-0 bottom-full mb-2 rounded-xl border border-white/10 bg-black/90 p-1.5 shadow-2xl backdrop-blur ${finding ? '' : 'w-64'}`}
        >
          {finding && find ? (
            <FindSubtitles
              itemId={find.itemId}
              onBack={() => setFinding(false)}
              onAdded={(t) => {
                find.onAdded(t)
                toggle(false)
              }}
            />
          ) : (
            <>
              <div className="px-3 pt-1.5 pb-1 text-xs font-medium tracking-wide text-white/50 uppercase">{tr('file.subtitles')}</div>
              <ul role="menu" className="max-h-72 overflow-y-auto">
                {item('off', tr('player.off'), !selected, undefined)}
                {tracks.map((t) => item(t.id, subtitleLabel(t), t.id === selected, t, note(t)))}
              </ul>
              {find && (
                <button
                  role="menuitem"
                  onClick={() => setFinding(true)}
                  className="mt-1 flex w-full items-center gap-3 rounded-md border-t border-white/10 px-3 py-2 text-start text-sm hover:bg-white/10"
                >
                  <Search className="size-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{tr('player.findSubtitles.open')}</span>
                </button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
