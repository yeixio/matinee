import { Check, Moon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { clock } from '../format'
import { SLEEP_MINUTES, sleepTimer, type Sleep } from './sleep'

// The moon button in the player's controls and its menu: Off, a few
// lengths of time, or the end of what's playing (for a book, the chapter). While a timer runs the
// button shows the time left, or that it stops at the end.
export function SleepMenu({
  sleep,
  left,
  kind,
  onOpenChange,
}: {
  sleep: Sleep
  left?: number
  kind: string
  onOpenChange: (open: boolean) => void
}) {
  const { t: tr } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const toggle = (v: boolean) => {
    setOpen(v)
    onOpenChange(v)
  }

  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && toggle(false)
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  })

  // Picking the time that's already running starts it again from now.
  const choose = (minutes: number | 'end' | undefined) => {
    if (typeof minutes === 'number') sleepTimer.after(minutes)
    else sleepTimer.set(minutes)
    toggle(false)
  }
  const item = (key: string, label: string, active: boolean, minutes: number | 'end' | undefined) => (
    <li key={key}>
      <button
        role="menuitemradio"
        aria-checked={active}
        onClick={() => choose(minutes)}
        className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-white/10"
      >
        <Check className={`size-4 shrink-0 ${active ? 'text-accent' : 'invisible'}`} />
        <span className="min-w-0 flex-1 truncate">{label}</span>
      </button>
    </li>
  )
  const timed = !!sleep && sleep !== 'end'
  const status = timed && left !== undefined ? tr('sleep.left', { time: clock(left) }) : sleep === 'end' ? tr('sleep.atEnd') : undefined
  const endLabel =
    kind === 'episode' ? tr('sleep.endOfEpisode') : kind === 'movie' ? tr('sleep.endOfMovie') : kind === 'chapter' ? tr('sleep.endOfChapter') : tr('sleep.endOfItem')

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className={`flex items-center gap-1.5 rounded-full p-2 hover:bg-white/10 ${sleep ? 'text-accent' : ''}`}
        aria-label={status ? `${tr('sleep.title')}: ${status}` : tr('sleep.title')}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <Moon className="size-6" />
        {timed && left !== undefined && (
          <span dir="ltr" className="text-sm tabular-nums">
            {clock(left)}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute end-0 bottom-full mb-2 w-64 rounded-xl border border-white/10 bg-black/90 p-1.5 shadow-2xl backdrop-blur">
          <div className="flex items-baseline justify-between gap-2 px-3 pt-1.5 pb-1">
            <span className="text-xs font-medium tracking-wide text-white/50 uppercase">{tr('sleep.title')}</span>
            {status && <span className="text-xs text-accent tabular-nums">{status}</span>}
          </div>
          <ul role="menu">
            {item('off', tr('player.off'), !sleep, undefined)}
            {SLEEP_MINUTES.map((m) => item(String(m), tr('common:every.minutes', { count: m }), timed && sleep.minutes === m, m))}
            {item('end', endLabel, sleep === 'end', 'end')}
          </ul>
        </div>
      )}
    </div>
  )
}
