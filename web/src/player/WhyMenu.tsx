import { Gauge } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { PlaybackCause } from '../api'
import { causeText, testConnection } from './why'

// The gauge in the player's controls, there once playback has buffered: why
// it did in plain words, what an admin could change (admins only), and a
// test of the connection to the server.
export function WhyMenu({ cause, onOpenChange }: { cause: PlaybackCause; onOpenChange: (open: boolean) => void }) {
  const { t: tr } = useTranslation()
  const [open, setOpen] = useState(false)
  const [test, setTest] = useState<'testing' | 'failed' | { mbps: number; pingMs: number }>()
  const ref = useRef<HTMLDivElement>(null)
  const abort = useRef<AbortController>(undefined)
  const toggle = (v: boolean) => {
    setOpen(v)
    onOpenChange(v)
    if (!v) abort.current?.abort()
  }

  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && toggle(false)
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  })
  useEffect(() => () => abort.current?.abort(), [])

  const run = async () => {
    abort.current?.abort()
    const a = new AbortController()
    abort.current = a
    setTest('testing')
    try {
      const r = await testConnection(a.signal)
      if (!a.signal.aborted) setTest(r)
    } catch {
      if (!a.signal.aborted) setTest('failed')
    }
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className="rounded-full p-2 hover:bg-white/10"
        aria-label={tr('player.why.title')}
        title={tr('player.why.title')}
        aria-expanded={open}
      >
        <Gauge className="size-6" />
      </button>
      {open && (
        <div
          role="dialog"
          aria-label={tr('player.why.title')}
          className="absolute end-0 bottom-full mb-2 w-80 max-w-[calc(100vw-2rem)] space-y-3 rounded-xl border border-white/10 bg-black/90 p-4 text-sm shadow-2xl backdrop-blur"
        >
          <div className="text-xs font-medium tracking-wide text-white/50 uppercase">{tr('player.why.title')}</div>
          <p>{causeText(tr, cause)}</p>
          {cause.converted && <p className="text-white/70">{tr(`player.why.converted.${cause.converted}`)}</p>}
          {cause.tip && <p className="rounded-md bg-white/10 p-2 text-white/80">{tr(`player.why.tip.${cause.tip}`)}</p>}
          <div className="flex items-center gap-3">
            <button
              onClick={run}
              disabled={test === 'testing'}
              className="rounded-full bg-white/15 px-3 py-1.5 font-medium hover:bg-white/25 disabled:opacity-60"
            >
              {test === 'testing' ? tr('player.why.testing') : tr('player.why.test')}
            </button>
            {test === 'failed' && <span className="text-white/70">{tr('player.why.testFailed')}</span>}
            {typeof test === 'object' && (
              <span dir="auto" className="text-white/70 tabular-nums">
                {tr('player.why.result', { mbps: test.mbps, ms: test.pingMs })}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
