import { AudioLines, Captions, Check, LoaderCircle, Minus, Plus, Search, Undo2, X } from 'lucide-react'
import { Fragment, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SubtitleSync, SubtitleTrack } from '../api'
import { subtitleLabel } from '../media'
import { FindSubtitles } from './FindSubtitles'
import { defaultSubtitles, subtitleLook, TIMING_STEP, type SubtitlePrefs } from './subtitlePrefs'
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

type OverlayProps = {
  cues: Cue[]
  position: () => number
  lifted: boolean
  /** How the person wants lines drawn. */
  prefs?: SubtitlePrefs
  /** Seconds later than written that lines show. */
  timing?: number
}

// SubtitleOverlay draws the cues showing at the current position. It reads
// the clock every frame, so lines appear on time rather than at the next
// timeupdate event.
export function SubtitleOverlay({ cues, position, lifted, prefs = defaultSubtitles, timing = 0 }: OverlayProps) {
  const [shown, setShown] = useState<Cue[]>([])
  const look = subtitleLook(prefs)
  useEffect(() => {
    let frame = 0
    const tick = () => {
      const now = activeCues(cues, position() - timing)
      setShown((prev) => (prev.length === now.length && prev.every((c, i) => c === now[i]) ? prev : now))
      frame = requestAnimationFrame(tick)
    }
    tick()
    return () => cancelAnimationFrame(frame)
  }, [cues, position, timing])

  if (!shown.length) return null
  return (
    <div
      aria-live="off"
      className={`pointer-events-none absolute inset-x-0 flex flex-col items-center gap-1 px-[6%] text-center transition-[bottom] duration-300 ${
        lifted ? 'bottom-36 md:bottom-40' : look.raised ? 'bottom-[18%]' : 'bottom-[7%]'
      }`}
    >
      {shown.map((cue, i) => (
        <div
          key={`${cue.start}-${i}`}
          dir="auto"
          className="rounded px-[0.4em] py-[0.1em] leading-snug font-medium whitespace-pre-line"
          style={{ fontSize: look.fontSize, color: look.color, textShadow: look.textShadow, backgroundColor: look.backgroundColor }}
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
  /** Set while text subtitles show: move them earlier or later. */
  timing?: { seconds: number; onChange: (seconds: number) => void }
  /** Set while text subtitles show: line them up with the audio, or undo that. */
  sync?: { state?: SubtitleSync; onSync: () => void; onUndo: () => void }
}

// SubtitleMenu is the captions button and its list of tracks, ending in
// Find subtitles when the server can look for more online.
export function SubtitleMenu({ tracks, selected, onSelect, onOpenChange, find, timing, sync }: MenuProps) {
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
              {timing && (
                <div className="mt-1 border-t border-white/10 px-3 pt-2 pb-1.5">
                  <div className="flex items-center gap-2 text-sm">
                    <span className="min-w-0 flex-1 truncate">{tr('common:subtitleTiming.title')}</span>
                    <button
                      onClick={() => timing.onChange(timing.seconds - TIMING_STEP)}
                      className="rounded-full p-1.5 hover:bg-white/10"
                      aria-label={tr('common:subtitleTiming.earlier')}
                      title={tr('common:subtitleTiming.earlier')}
                    >
                      <Minus className="size-4" />
                    </button>
                    <span className="w-14 text-center text-xs tabular-nums">{timingLabel(timing.seconds)}</span>
                    <button
                      onClick={() => timing.onChange(timing.seconds + TIMING_STEP)}
                      className="rounded-full p-1.5 hover:bg-white/10"
                      aria-label={tr('common:subtitleTiming.later')}
                      title={tr('common:subtitleTiming.later')}
                    >
                      <Plus className="size-4" />
                    </button>
                  </div>
                  {timing.seconds !== 0 && (
                    <button onClick={() => timing.onChange(0)} className="mt-1 text-xs text-white/60 hover:text-white">
                      {tr('common:subtitleTiming.reset')}
                    </button>
                  )}
                  {sync && <SyncRow {...sync} />}
                </div>
              )}
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

// SyncRow lines the subtitles up with the audio, and says what that did.
function SyncRow({ state, onSync, onUndo }: NonNullable<MenuProps['sync']>) {
  const { t: tr } = useTranslation()
  const syncing = state?.state === 'syncing'
  const synced = state?.state === 'synced'
  const status = syncStatus(tr, state)
  return (
    <div className="mt-2 border-t border-white/10 pt-2">
      <div className="flex items-center gap-2">
        <button
          onClick={onSync}
          disabled={syncing}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md py-1 text-start text-sm hover:text-accent disabled:opacity-60 disabled:hover:text-current"
        >
          {syncing ? <LoaderCircle className="size-4 shrink-0 animate-spin" /> : <AudioLines className="size-4 shrink-0" />}
          <span className="min-w-0 flex-1 truncate">{tr(synced ? 'common:subtitleSync.again' : 'common:subtitleSync.title')}</span>
        </button>
        {synced && (
          <button
            onClick={onUndo}
            className="flex items-center gap-1 rounded-full px-2 py-1 text-xs text-white/60 hover:bg-white/10 hover:text-white"
          >
            <Undo2 className="size-3.5" />
            {tr('common:subtitleSync.undo')}
          </button>
        )}
      </div>
      {status && (
        <p className="mt-0.5 text-xs text-white/60" role="status">
          {status}
        </p>
      )}
    </div>
  )
}

/** syncStatus says what syncing a track to the audio did, or undefined when nobody did. */
export function syncStatus(tr: (key: string, opts?: Record<string, unknown>) => string, s: SubtitleSync | undefined): string | undefined {
  switch (s?.state) {
    case 'syncing':
      return tr('common:subtitleSync.syncing')
    case 'synced': {
      const ms = s.offsetMs ?? 0
      const speed = !!s.rate && s.rate !== 1
      if (Math.abs(ms) < 100) return speed ? tr('common:subtitleSync.speed') : tr('common:subtitleSync.inSync')
      const seconds = (Math.abs(ms) / 1000).toLocaleString(undefined, { maximumFractionDigits: 1, minimumFractionDigits: 1 })
      const key = (ms < 0 ? 'earlier' : 'later') + (speed ? 'Speed' : '')
      return tr(`common:subtitleSync.${key}`, { seconds })
    }
    case 'inSync':
    case 'unsure':
    case 'failed':
    case 'off':
      return tr(`common:subtitleSync.${s.state}`)
  }
  return undefined
}

/** timingLabel shows a subtitle offset like "+0.3 s". */
export function timingLabel(seconds: number): string {
  if (!seconds) return '0 s'
  return `${seconds > 0 ? '+' : '−'}${Math.abs(seconds).toFixed(1)} s`
}
