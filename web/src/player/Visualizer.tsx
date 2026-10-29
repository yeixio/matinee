import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Pause, Play, Wand2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import { Artwork } from '../components/Artwork'
import { useAudio } from './AudioPlayer'
import * as L from './levels'

const styleKey = 'matinee.visualizer'

function savedStyle(): L.Style {
  try {
    return L.parseStyle(localStorage.getItem(styleKey))
  } catch {
    return 'bars'
  }
}

const levelsQuery = (id: number) => ({
  queryKey: ['levels', id],
  queryFn: async () => L.decodeLevels(await api.levels(id)),
  staleTime: Infinity,
  gcTime: 30 * 60_000,
  retry: 1,
})

// The theme's colours, for drawing on the canvas.
function colors() {
  const css = getComputedStyle(document.documentElement)
  return { accent: css.getPropertyValue('--color-accent').trim() || '#ff6a55', strong: css.getPropertyValue('--color-accent-strong').trim() || '#ff8873' }
}

/**
 * The music visualizer, over the whole window: the song's sound as bars, a
 * halo round the cover, or rings that pulse with the beat, drawn from the
 * levels the server worked out (see levels.ts) in time with the music.
 * Left and right (or the look button) change the look; Escape closes it.
 */
export function Visualizer({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const s = useAudio()
  const item = s.current!
  const [look, setLookState] = useState(savedStyle)
  const [chrome, setChrome] = useState(true)
  const canvas = useRef<HTMLCanvasElement>(null)
  const cover = useRef<HTMLDivElement>(null)
  const hideTimer = useRef<number>(undefined)
  const levels = useQuery({ ...levelsQuery(item.id), enabled: item.kind === 'track' })
  const qc = useQueryClient()
  const next = s.queue.entries[s.queue.pos + 1]?.item

  // The next song's levels come early, so its visuals start with it.
  useEffect(() => {
    if (next?.kind === 'track') qc.prefetchQuery(levelsQuery(next.id))
  }, [next?.id, next?.kind, qc])

  const setLook = (l: L.Style) => {
    setLookState(l)
    try {
      localStorage.setItem(styleKey, l)
    } catch {
      // Private windows may not keep it; the look still changes.
    }
  }
  const wake = () => {
    setChrome(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => setChrome(false), 4000)
  }
  useEffect(() => {
    wake()
    return () => window.clearTimeout(hideTimer.current)
  }, [item.id])

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') setLook(L.nextStyle(look, e.key === 'ArrowLeft' ? -1 : 1))
      else if (e.key === ' ') {
        e.preventDefault()
        s.toggle()
      } else if (e.key === 'Escape') onClose()
      else return
      e.stopPropagation()
      wake()
    }
    window.addEventListener('keydown', key, true)
    return () => window.removeEventListener('keydown', key, true)
  })

  // Drawn every frame from the audio element's own clock; the browser stops
  // drawing while the tab is hidden.
  const live = useRef({ levels: levels.data, playing: s.playing, look })
  live.current = { levels: levels.data, playing: s.playing, look }
  useEffect(() => {
    const el = canvas.current
    const ctx = el?.getContext('2d')
    if (!el || !ctx) return
    const n = 48
    const target = new Float32Array(n)
    const shown = new Float32Array(n)
    let last = 0
    let frame = 0
    let { accent, strong } = colors()
    const draw = (now: number) => {
      frame = requestAnimationFrame(draw)
      const dt = last ? Math.min(0.1, (now - last) / 1000) : 1
      last = now
      const { levels, playing, look } = live.current
      if (levels) L.levelsAt(levels, s.audio.current?.currentTime ?? 0, target)
      else if (playing) L.idle(now / 1000, target)
      else target.fill(0.04)
      L.settle(shown, target, dt)

      const dpr = window.devicePixelRatio || 1
      const w = el.clientWidth
      const h = el.clientHeight
      if (el.width !== Math.round(w * dpr) || el.height !== Math.round(h * dpr)) {
        el.width = Math.round(w * dpr)
        el.height = Math.round(h * dpr)
        ;({ accent, strong } = colors())
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, w, h)
      const size = Math.min(w, h) * 0.8
      const cx = w / 2
      const cy = h / 2
      const beat = L.bass(shown)
      if (cover.current) cover.current.style.transform = `translate(-50%, -50%) scale(${look === 'pulse' ? 0.96 + beat * 0.1 : 1 + beat / 14})`

      if (look === 'bars') {
        const count = 32
        const gap = size / count / 3
        const bw = (size - gap * (count - 1)) / count
        for (let i = 0; i < count; i++) {
          const v = shown[Math.floor((i * n) / count)]
          const bh = Math.max(bw, size * 0.62 * Math.max(0.03, v))
          ctx.globalAlpha = 0.45 + 0.55 * v
          ctx.fillStyle = i % 2 ? accent : strong
          ctx.beginPath()
          ctx.roundRect(cx - size / 2 + i * (bw + gap), cy - bh / 2, bw, bh, bw / 2)
          ctx.fill()
        }
      } else if (look === 'halo') {
        const radius = size * 0.23 + size * 0.03
        const length = size / 2 - radius - size * 0.02
        ctx.lineCap = 'round'
        ctx.lineWidth = Math.max(2, size / 110)
        ctx.strokeStyle = accent
        const bands = 24
        for (let side = 0; side < 2; side++) {
          for (let b = 0; b < bands; b++) {
            const v = shown[Math.floor((b * n) / bands)]
            const a = (side ? -1 : 1) * Math.PI * ((b + 0.5) / bands) - Math.PI / 2
            const len = length * Math.max(0.04, v)
            ctx.globalAlpha = 0.35 + 0.65 * v
            ctx.beginPath()
            ctx.moveTo(cx + Math.cos(a) * radius, cy + Math.sin(a) * radius)
            ctx.lineTo(cx + Math.cos(a) * (radius + len), cy + Math.sin(a) * (radius + len))
            ctx.stroke()
          }
        }
      } else {
        const inner = size * 0.4
        const groups = [
          [0, 8],
          [8, 20],
          [20, 32],
          [32, 48],
        ]
        groups.forEach(([from, to], i) => {
          let v = 0
          for (let b = from; b < to; b++) v += shown[b]
          v /= to - from
          const d = (inner + ((size - inner) * (i + 1)) / groups.length) * (0.82 + 0.18 * v)
          ctx.globalAlpha = 0.15 + (0.8 - i * 0.12) * v
          ctx.lineWidth = Math.max(2, size / (60 + i * 30))
          ctx.strokeStyle = i % 2 ? strong : accent
          ctx.beginPath()
          ctx.arc(cx, cy, d / 2, 0, Math.PI * 2)
          ctx.stroke()
        })
      }
      ctx.globalAlpha = 1
    }
    frame = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(frame)
  }, [s.audio])

  const lookName = t(`common:music.look.${look}`)
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('common:music.visualizer')}
      className={`fixed inset-0 z-50 overflow-hidden bg-black ${chrome ? '' : 'cursor-none'}`}
      onMouseMove={wake}
      onClick={wake}
      data-testid="visualizer"
    >
      <div className="pointer-events-none absolute inset-0 opacity-30">
        <Artwork item={item} type="poster" label={false} className="size-full scale-125 blur-3xl" />
      </div>
      <canvas ref={canvas} className="absolute inset-0 size-full" />
      {look !== 'bars' && (
        <div ref={cover} className="pointer-events-none absolute top-1/2 left-1/2 aspect-square overflow-hidden rounded-full" style={{ width: `min(${look === 'pulse' ? 32 : 36.8}vw, ${look === 'pulse' ? 32 : 36.8}vh)` }}>
          <Artwork item={item} type="poster" label={false} className="size-full" />
        </div>
      )}
      <div className={`absolute inset-0 transition-opacity duration-500 ${chrome ? 'opacity-100' : 'pointer-events-none opacity-0'}`}>
        <div className="absolute inset-x-0 top-0 flex justify-between p-4 md:p-6">
          <button onClick={onClose} className="rounded-full p-2 text-white/80 hover:bg-white/10 hover:text-white" aria-label={t('close')} title={t('close')}>
            <X className="size-6" />
          </button>
          <button
            onClick={() => setLook(L.nextStyle(look))}
            className="flex items-center gap-2 rounded-full bg-white/15 px-4 py-2 text-sm font-medium text-white hover:bg-white/25"
            title={t('common:music.lookHint')}
            aria-label={`${t('common:music.changeLook')}: ${lookName}`}
          >
            <Wand2 className="size-4" />
            {lookName}
          </button>
        </div>
        <div className="absolute inset-x-0 bottom-0 flex items-center gap-4 p-5 md:p-8">
          <Artwork item={item} type="poster" label={false} className="size-14 shrink-0 rounded-md md:size-16" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-xl font-semibold text-white md:text-2xl">{item.title}</div>
            <div className="truncate text-white/70">{[item.artist, item.album].filter(Boolean).join(' · ')}</div>
          </div>
          <span className="hidden text-sm text-white/60 md:block">{t('common:music.lookHint')}</span>
          <button onClick={s.toggle} className="rounded-full p-3 text-white hover:bg-white/10" aria-label={s.playing ? t('player.pause') : t('item.play')}>
            {s.playing ? <Pause className="size-7" /> : <Play className="size-7" />}
          </button>
        </div>
      </div>
    </div>
  )
}
