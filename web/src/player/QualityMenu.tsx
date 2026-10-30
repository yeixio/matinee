import { Check, Gauge } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { QualityStep } from '../api'
import type { Quality } from './quality'

// The quality button in the player's controls and its menu: Auto, each
// quality step the server offers, and Original. The button says what's
// playing, such as "Auto · 720p".
export function QualityMenu({
  steps,
  selected,
  playing,
  onSelect,
  onOpenChange,
}: {
  steps: QualityStep[]
  selected: Quality
  // What Auto is playing right now, when known: a step's name, or Original.
  playing?: string
  onSelect: (q: Quality) => void
  onOpenChange: (open: boolean) => void
}) {
  const { t: tr, i18n } = useTranslation()
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

  const choose = (q: Quality) => {
    toggle(false)
    if (q !== selected) onSelect(q)
  }
  const mbps = (s: QualityStep) => tr('common:quality.mbps', { n: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(s.videoBps / 1e6) })
  const entry = (q: Quality, label: string, detail?: string) => (
    <li key={q}>
      <button
        role="menuitemradio"
        aria-checked={q === selected}
        onClick={() => choose(q)}
        className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-white/10"
      >
        <Check className={`size-4 shrink-0 ${q === selected ? 'text-accent' : 'invisible'}`} />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {detail && <span className="text-xs text-white/50 tabular-nums">{detail}</span>}
      </button>
    </li>
  )
  const current =
    selected === 'auto'
      ? playing
        ? tr('common:quality.autoNow', { quality: playing })
        : tr('common:quality.auto')
      : selected === 'original'
        ? tr('common:quality.original')
        : (steps.find((s) => s.id === selected)?.label ?? selected)

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className="flex items-center gap-1.5 rounded-full p-2 hover:bg-white/10"
        aria-label={`${tr('common:quality.title')}: ${current}`}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <Gauge className="size-6" />
        <span className="hidden text-sm sm:inline">{current}</span>
      </button>
      {open && (
        <div className="absolute end-0 bottom-full mb-2 w-64 rounded-xl border border-white/10 bg-black/90 p-1.5 shadow-2xl backdrop-blur">
          <div className="px-3 pt-1.5 pb-1 text-xs font-medium tracking-wide text-white/50 uppercase">{tr('common:quality.title')}</div>
          <ul role="menu">
            {entry('auto', tr('common:quality.auto'), selected === 'auto' ? playing : undefined)}
            {steps.map((s) => entry(s.id, s.label, mbps(s)))}
            {entry('original', tr('common:quality.original'))}
          </ul>
          <p className="px-3 pt-1 pb-2 text-xs text-white/50">{tr('common:quality.hint')}</p>
        </div>
      )}
    </div>
  )
}
