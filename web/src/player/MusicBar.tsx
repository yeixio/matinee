import { ChevronUp, ListMusic, Pause, Play, Repeat, Repeat1, Shuffle, SkipBack, SkipForward, Volume1, Volume2, VolumeX, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Artwork } from '../components/Artwork'
import { clock } from '../format'
import { useAudio, useAudioTime, type AudioState } from './AudioPlayer'

// The music player along the bottom of every page: what's playing, the
// buttons, and the way into the full player and the queue.
export function MusicBar() {
  const { t } = useTranslation()
  const s = useAudio()
  const { time, dur } = useAudioTime()
  const item = s.current!
  const open = () => s.setExpanded(true)
  return (
    <div className="fixed inset-x-0 bottom-[calc(57px+env(safe-area-inset-bottom))] z-30 border-t border-line/60 bg-surface/95 backdrop-blur md:bottom-0">
      {/* Phones: a thin line shows how far along it is. */}
      <div className="absolute inset-x-0 top-0 h-0.5 bg-text/10 md:hidden">
        <div className="h-full bg-accent" style={{ width: `${dur ? (time / dur) * 100 : 0}%` }} />
      </div>
      {s.notice && (
        <div role="status" className="absolute -top-12 start-1/2 -translate-x-1/2 w-max max-w-[calc(100vw-2rem)] animate-fade rounded-full bg-text px-4 py-2 text-center text-sm font-medium text-ink shadow-lg rtl:translate-x-1/2">
          {s.notice}
        </div>
      )}
      <div className="flex h-16 items-center gap-3 px-3 md:grid md:h-20 md:grid-cols-[1fr_minmax(0,36rem)_1fr] md:gap-6 md:px-6">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <button onClick={open} className="group relative shrink-0" aria-label={t('common:music.openPlayer')}>
            <Artwork item={item} type="poster" label={false} className="size-11 rounded-md shadow-md md:size-14" />
            <span className="absolute inset-0 hidden place-items-center rounded-md bg-black/50 opacity-0 transition group-hover:opacity-100 md:grid">
              <ChevronUp className="size-5 text-white" />
            </span>
          </button>
          <div className="min-w-0 flex-1">
            {/* On phones the whole title opens the player. */}
            <button onClick={open} className="block w-full truncate text-start text-sm font-medium md:hidden">
              {item.title}
            </button>
            <Link to={item.parentId ? `/item/${item.parentId}` : '#'} className="hidden truncate text-sm font-medium hover:underline md:block">
              {item.title}
            </Link>
            {item.artistId ? (
              <Link to={`/item/${item.artistId}`} className="block truncate text-xs text-muted hover:text-text hover:underline">
                {item.artist}
              </Link>
            ) : null}
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-center gap-1">
          <Controls s={s} compact />
          <div className="hidden w-full items-center gap-2 text-[11px] text-muted tabular-nums md:flex">
            <span className="w-10 text-end">{clock(time)}</span>
            <Seek s={s} time={time} dur={dur} />
            <span className="w-10">{clock(dur)}</span>
          </div>
        </div>

        <div className="hidden items-center justify-end gap-1 md:flex">
          <button
            onClick={open}
            className="rounded-full p-2 text-muted hover:bg-text/5 hover:text-text"
            aria-label={t('common:music.queue')}
            title={t('common:music.queue')}
          >
            <ListMusic className="size-5" />
          </button>
          <VolumeControl s={s} />
          <button onClick={s.stop} className="rounded-full p-2 text-faint hover:bg-text/5 hover:text-text" aria-label={t('player.closePlayer')} title={t('player.closePlayer')}>
            <X className="size-5" />
          </button>
        </div>
      </div>
    </div>
  )
}

// Shuffle, back, play, next and repeat. compact leaves only play and next
// on phones.
export function Controls({ s, compact = false, large = false }: { s: AudioState; compact?: boolean; large?: boolean }) {
  const { t } = useTranslation()
  const { queue } = s
  const lastOne = queue.pos >= queue.entries.length - 1 && queue.repeat === 'off'
  const side = `rounded-full p-2 transition-colors hover:text-text ${compact ? 'hidden md:block' : ''}`
  const repeatLabel = t(`common:music.repeat${queue.repeat === 'off' ? 'Off' : queue.repeat === 'all' ? 'All' : 'One'}`)
  const icon = large ? 'size-7' : 'size-5'
  return (
    <div className={`flex items-center ${large ? 'gap-5' : 'gap-1 md:gap-2'}`}>
      <button
        onClick={() => s.setShuffle(!queue.shuffle)}
        aria-pressed={queue.shuffle}
        aria-label={t('common:music.shuffle')}
        title={t('common:music.shuffle')}
        className={`${side} relative ${queue.shuffle ? 'text-accent' : 'text-muted'}`}
      >
        <Shuffle className={large ? 'size-6' : 'size-4'} />
        {queue.shuffle && <span className="absolute bottom-0.5 start-1/2 size-1 -translate-x-1/2 rounded-full bg-accent" />}
      </button>
      <button onClick={s.prev} className={`${side} text-muted`} aria-label={t('player.previous')} title={t('player.previous')}>
        <SkipBack className={`${icon} fill-current`} />
      </button>
      <button
        onClick={s.toggle}
        className={`grid place-items-center rounded-full bg-text text-ink transition-transform hover:scale-105 ${large ? 'size-16' : 'size-10'}`}
        aria-label={s.playing ? t('player.pause') : t('item.play')}
      >
        {s.playing ? <Pause className={`${icon} fill-current`} /> : <Play className={`${icon} translate-x-0.5 fill-current`} />}
      </button>
      <button onClick={s.next} disabled={lastOne} className="rounded-full p-2 text-muted transition-colors hover:text-text disabled:opacity-30" aria-label={t('player.next')} title={t('player.next')}>
        <SkipForward className={`${icon} fill-current`} />
      </button>
      <button
        onClick={s.cycleRepeat}
        aria-label={repeatLabel}
        title={repeatLabel}
        className={`${side} relative ${queue.repeat !== 'off' ? 'text-accent' : 'text-muted'}`}
      >
        {queue.repeat === 'one' ? <Repeat1 className={large ? 'size-6' : 'size-4'} /> : <Repeat className={large ? 'size-6' : 'size-4'} />}
        {queue.repeat !== 'off' && <span className="absolute bottom-0.5 start-1/2 size-1 -translate-x-1/2 rounded-full bg-accent" />}
      </button>
    </div>
  )
}

// The bar to see and change where in the song it is.
export function Seek({ s, time, dur }: { s: AudioState; time: number; dur: number }) {
  const { t } = useTranslation()
  // While dragging, show where it'll go rather than where it is.
  const [drag, setDrag] = useState<number>()
  const value = drag ?? time
  return (
    <input
      type="range"
      min={0}
      max={dur || 0}
      step={0.1}
      value={value}
      onChange={(e) => setDrag(Number(e.target.value))}
      onPointerUp={() => {
        if (drag !== undefined) s.seek(drag)
        setDrag(undefined)
      }}
      onKeyUp={() => {
        if (drag !== undefined) s.seek(drag)
        setDrag(undefined)
      }}
      aria-label={t('player.seek')}
      className="h-3 w-full flex-1 cursor-pointer appearance-none bg-transparent accent-accent"
      style={{ background: `linear-gradient(to right, var(--color-accent) ${dur ? (value / dur) * 100 : 0}%, color-mix(in srgb, var(--color-text) 15%, transparent) 0) center / 100% 4px no-repeat` }}
    />
  )
}

export function VolumeControl({ s }: { s: AudioState }) {
  const { t } = useTranslation()
  const el = s.audio.current
  const [volume, setVolume] = useState(el?.volume ?? 1)
  const [muted, setMuted] = useState(el?.muted ?? false)
  useEffect(() => {
    if (!el) return
    const sync = () => {
      setVolume(el.volume)
      setMuted(el.muted)
    }
    sync()
    el.addEventListener('volumechange', sync)
    return () => el.removeEventListener('volumechange', sync)
  }, [el])
  const level = muted ? 0 : volume
  const Icon = level === 0 ? VolumeX : level < 0.5 ? Volume1 : Volume2
  return (
    <div className="flex items-center gap-1">
      <button
        onClick={() => el && (el.muted = !el.muted)}
        className="rounded-full p-2 text-muted hover:bg-text/5 hover:text-text"
        aria-label={muted ? t('player.unmute') : t('player.mute')}
        title={muted ? t('player.unmute') : t('player.mute')}
      >
        <Icon className="size-5" />
      </button>
      <input
        type="range"
        min={0}
        max={1}
        step={0.01}
        value={level}
        onChange={(e) => {
          if (!el) return
          el.volume = Number(e.target.value)
          el.muted = false
        }}
        aria-label={t('common:music.volume')}
        className="h-3 w-24 cursor-pointer appearance-none bg-transparent accent-text"
        style={{ background: `linear-gradient(to right, var(--color-text) ${level * 100}%, color-mix(in srgb, var(--color-text) 15%, transparent) 0) center / 100% 4px no-repeat` }}
      />
    </div>
  )
}
