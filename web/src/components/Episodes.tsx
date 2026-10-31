import { Check, Pause, Play } from 'lucide-react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import type { Item } from '../api'
import { duration, progressFraction, shortDate } from '../format'
import { useAudio, useBookTime } from '../player/AudioPlayer'
import { Artwork } from './Artwork'
import { Equalizer } from './Tracks'

// What a podcast episode's line says under its title: the day it came out,
// and how long it is or how much is left.
export function episodeLine(ep: Item, t: TFunction, locale: string, withShow = false): string {
  const parts: string[] = []
  if (withShow && ep.podcast) parts.push(ep.podcast)
  if (ep.released) parts.push(shortDate(ep.released, locale))
  const left = ep.durationMs && ep.positionMs && !ep.played ? ep.durationMs - ep.positionMs : 0
  if (left > 0) parts.push(t('common:books.left', { time: duration(left) }))
  else if (ep.durationMs) parts.push(duration(ep.durationMs))
  return parts.join(' · ')
}

// A podcast's episodes, newest first: each with its date, length or time
// left, and a button that plays it, or picks up where the person left off.
export function EpisodeList({ episodes, withShow = false }: { episodes: Item[]; withShow?: boolean }) {
  const { t, i18n } = useTranslation()
  return (
    <ol className="divide-y divide-line/50">
      {episodes.map((ep) => (
        <EpisodeRow key={ep.id} ep={ep} line={episodeLine(ep, t, i18n.language, withShow)} withArt={withShow} />
      ))}
    </ol>
  )
}

function EpisodeRow({ ep, line, withArt }: { ep: Item; line: string; withArt: boolean }) {
  const { t } = useTranslation()
  const audio = useAudio()
  const playingThis = audio.book?.id === ep.id
  const { time } = useBookTime()
  const frac = playingThis && ep.durationMs ? (time * 1000) / ep.durationMs : progressFraction(ep.positionMs, ep.durationMs)
  return (
    <li className="group flex items-center gap-3 py-3">
      <button
        onClick={() => (playingThis ? audio.toggle() : audio.playItem(ep))}
        className={`grid size-10 shrink-0 place-items-center rounded-full transition ${playingThis ? 'bg-accent text-on-accent' : 'bg-text/10 text-text hover:bg-accent hover:text-on-accent'}`}
        aria-label={playingThis && audio.playing ? t('player.pause') : `${t('item.play')} ${ep.title}`}
      >
        {playingThis && audio.playing ? <Pause className="size-4 fill-current" /> : <Play className="size-4 translate-x-px fill-current" />}
      </button>
      {withArt && <Artwork item={ep} type="poster" label={false} className="size-12 shrink-0 rounded-md" />}
      <Link to={`/item/${ep.id}`} className="min-w-0 flex-1">
        <div className={`flex items-center gap-2 text-sm font-medium group-hover:text-accent-strong ${ep.played ? 'text-muted' : ''}`}>
          {playingThis && <Equalizer playing={audio.playing} />}
          <span className="line-clamp-2">{ep.title}</span>
        </div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted">
          {ep.played && <Check className="size-3.5 shrink-0 text-accent" strokeWidth={3} aria-label={t('common:podcasts.played')} />}
          <span className="truncate">{line}</span>
        </div>
        {frac > 0 && frac < 1 && !ep.played && (
          <div className="mt-1.5 h-1 max-w-48 overflow-hidden rounded-full bg-text/10">
            <div className="h-full bg-accent" style={{ width: `${frac * 100}%` }} />
          </div>
        )}
      </Link>
    </li>
  )
}
