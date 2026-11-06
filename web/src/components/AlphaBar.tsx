import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Letter } from '../api'

const alphabet = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ']

/** The letters the bar shows, in list order: # and A to Z always, then any
 * other alphabet's letters the list has, reversed for a Z to A list. */
export function barLetters(letters: Letter[], desc: boolean): string[] {
  const extra = letters.map((l) => l.letter).filter((l) => !alphabet.includes(l))
  const all = [...alphabet, ...extra]
  return desc ? all.reverse() : all
}

/** Where a letter on the bar jumps to: its own titles, or the next letter
 * down the bar that has some, or the last one when none further down does. */
export function jumpIndex(letters: Letter[], bar: string[], letter: string): number | undefined {
  const at = new Map(letters.map((l) => [l.letter, l.index]))
  const from = bar.indexOf(letter)
  for (let i = Math.max(0, from); i < bar.length; i++) {
    const index = at.get(bar[i])
    if (index !== undefined) return index
  }
  return letters.at(-1)?.index
}

/** A to Z down the edge of the screen. Click a letter, or drag along the bar,
 * to jump the list to the titles that start with it. */
export function AlphaBar({ letters, desc, onJump }: { letters: Letter[]; desc: boolean; onJump: (index: number) => void }) {
  const { t } = useTranslation()
  const bar = barLetters(letters, desc)
  const has = new Set(letters.map((l) => l.letter))
  const [dragging, setDragging] = useState<string>()
  const last = useRef<string>(undefined)

  const jump = (letter: string) => {
    if (letter === last.current) return
    last.current = letter
    const index = jumpIndex(letters, bar, letter)
    if (index !== undefined) onJump(index)
  }
  const letterAt = (x: number, y: number) => (document.elementFromPoint(x, y) as HTMLElement | null)?.closest<HTMLElement>('[data-letter]')?.dataset.letter

  return (
    <nav
      aria-label={t('library.letters')}
      className="fixed end-0.5 top-16 bottom-20 z-20 flex touch-none select-none flex-col items-center justify-center md:end-1.5 md:top-20 md:bottom-6"
      onPointerDown={(e) => {
        if (e.pointerType === 'mouse') return // a click does it
        e.currentTarget.setPointerCapture(e.pointerId)
        last.current = undefined
        const l = letterAt(e.clientX, e.clientY)
        if (l) {
          setDragging(l)
          jump(l)
        }
      }}
      onPointerMove={(e) => {
        if (!dragging) return
        const l = letterAt(e.clientX, e.clientY)
        if (l) {
          setDragging(l)
          jump(l)
        }
      }}
      onPointerUp={() => setDragging(undefined)}
      onPointerCancel={() => setDragging(undefined)}
    >
      <div className="flex max-h-full flex-col items-center overflow-hidden rounded-full bg-ink/60 px-0.5 py-1 backdrop-blur">
        {bar.map((l) => (
          <button
            key={l}
            type="button"
            data-letter={l}
            aria-label={t('library.jumpTo', { letter: l })}
            disabled={!has.has(l)}
            onClick={() => {
              last.current = undefined
              jump(l)
            }}
            className={`flex min-h-0 w-5 shrink items-center justify-center text-[10px] font-semibold leading-[1.15] md:w-6 md:text-[11px] ${
              dragging === l ? 'text-accent' : has.has(l) ? 'text-muted hover:text-accent' : 'text-muted/30'
            }`}
          >
            {l}
          </button>
        ))}
      </div>
      {dragging && (
        <div
          aria-hidden
          className="pointer-events-none fixed start-1/2 top-1/2 flex size-20 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-2xl bg-ink font-display text-4xl font-bold text-accent shadow-xl ring-1 ring-line rtl:translate-x-1/2"
        >
          {dragging}
        </div>
      )}
    </nav>
  )
}
