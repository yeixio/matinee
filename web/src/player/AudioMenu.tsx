import { Check, Languages } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { AudioStream } from '../api'
import { audioLabel, audioTitle } from '../media'

// The audio button in the player's controls and its menu: each of the
// file's audio tracks, such as another language or a commentary.
export function AudioMenu({
  tracks,
  selected,
  onSelect,
  onOpenChange,
}: {
  tracks: AudioStream[]
  selected: number
  onSelect: (index: number) => void
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
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
  const current = tracks.find((a) => a.index === selected)
  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className={`flex items-center gap-1.5 rounded-full p-2 hover:bg-white/10 ${current?.commentary ? 'text-accent' : ''}`}
        aria-label={`${t('common:audioTrack.title')}${current ? `: ${audioLabel(current)}` : ''}`}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <Languages className="size-6" />
      </button>
      {open && (
        <div className="absolute end-0 bottom-full mb-2 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-white/10 bg-black/90 p-1.5 shadow-2xl backdrop-blur">
          <div className="px-3 pt-1.5 pb-1 text-xs font-medium tracking-wide text-white/50 uppercase">{t('common:audioTrack.title')}</div>
          <ul role="menu" className="max-h-80 overflow-y-auto">
            {tracks.map((a) => (
              <li key={a.index}>
                <button
                  role="menuitemradio"
                  aria-checked={a.index === selected}
                  onClick={() => {
                    toggle(false)
                    if (a.index !== selected) onSelect(a.index)
                  }}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-white/10"
                >
                  <Check className={`size-4 shrink-0 ${a.index === selected ? 'text-accent' : 'invisible'}`} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{audioLabel(a)}</span>
                    <span className="block truncate text-xs text-white/50">{audioTitle(a)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
