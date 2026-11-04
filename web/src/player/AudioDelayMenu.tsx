import { AudioLines, Minus, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { clampDelay, DELAY_STEP_MS, delayLabel, type Delay } from './audioDelay'

// The audio delay button in the player's controls and its menu: earlier or
// later in 25 ms steps, back to none, and whether it's for this title only.
export function AudioDelayMenu({
  delay,
  onChange,
  onOpenChange,
}: {
  delay: Delay
  onChange: (d: Delay) => void
  onOpenChange: (open: boolean) => void
}) {
  const { t, i18n } = useTranslation()
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
  const step = (by: number) => onChange({ ...delay, ms: clampDelay(delay.ms + by) })
  const label = delayLabel(delay.ms, i18n.language)
  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className={`flex items-center gap-1.5 rounded-full p-2 hover:bg-white/10 ${delay.ms ? 'text-accent' : ''}`}
        aria-label={`${t('common:audioDelay.title')}: ${label}`}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <AudioLines className="size-6" />
      </button>
      {open && (
        <div role="dialog" aria-label={t('common:audioDelay.title')} className="absolute end-0 bottom-full mb-2 w-72 space-y-3 rounded-xl border border-white/10 bg-black/90 p-3 shadow-2xl backdrop-blur">
          <div className="text-xs font-medium tracking-wide text-white/50 uppercase">{t('common:audioDelay.title')}</div>
          <div className="flex items-center justify-between gap-2">
            <button onClick={() => step(-DELAY_STEP_MS)} className="rounded-full bg-white/10 p-2 hover:bg-white/20" aria-label={t('common:audioDelay.earlier')}>
              <Minus className="size-4" />
            </button>
            <span dir="ltr" className="text-lg tabular-nums" aria-live="polite">
              {label}
            </span>
            <button onClick={() => step(DELAY_STEP_MS)} className="rounded-full bg-white/10 p-2 hover:bg-white/20" aria-label={t('common:audioDelay.later')}>
              <Plus className="size-4" />
            </button>
          </div>
          <p className="text-xs text-white/60">{t('common:audioDelay.hint')}</p>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-accent" checked={delay.forTitle} onChange={(e) => onChange({ ...delay, forTitle: e.target.checked })} />
            {t('common:audioDelay.forTitle')}
          </label>
          {delay.ms !== 0 && (
            <button onClick={() => onChange({ ...delay, ms: 0 })} className="text-sm text-accent hover:underline">
              {t('common:audioDelay.reset')}
            </button>
          )}
        </div>
      )}
    </div>
  )
}
