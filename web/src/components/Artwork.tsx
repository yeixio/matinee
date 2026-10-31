import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { imageUrl, type Item } from '../api'
import { hue } from '../format'

// Artwork fades in when loaded and falls back to a tinted title card, so a
// library without posters still looks deliberate.
export function Artwork({
  item,
  type,
  className = '',
  label = true,
}: {
  item: Item
  type: 'poster' | 'backdrop'
  className?: string
  label?: boolean
}) {
  const { t } = useTranslation()
  // Seasons, tracks, episodes, book parts and podcast episodes borrow artwork from their show, album or book on the server.
  const borrows = item.kind === 'season' || item.kind === 'track' || item.kind === 'episode' || item.kind === 'part' || item.kind === 'podcastEpisode'
  // An episode without its own still shows the show's backdrop instead.
  const src = type === 'poster' && item.kind === 'episode' && !item.hasPoster ? 'backdrop' : type
  const has = (src === 'poster' ? item.hasPoster : item.hasBackdrop) || borrows
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)
  const h = hue(item.title)
  return (
    <div
      className={`relative overflow-hidden bg-raised ${className}`}
      style={{ backgroundImage: `linear-gradient(140deg, hsl(${h} 35% 22%), hsl(${(h + 40) % 360} 30% 10%))` }}
    >
      {item.pending && (!has || failed || !loaded) && (
        <div className="pending-sweep absolute inset-0" title={t('artwork.fetching')} aria-label={t('artwork.fetching')} />
      )}
      {(!has || failed || !loaded) && label && (
        <div className="absolute inset-0 flex items-end p-3">
          <span className="line-clamp-3 text-sm leading-snug font-semibold text-white/85">{item.title}</span>
        </div>
      )}
      {has && !failed && (
        <img
          src={imageUrl(item.id, src)}
          alt=""
          loading="lazy"
          decoding="async"
          onLoad={() => setLoaded(true)}
          onError={() => setFailed(true)}
          className={`absolute inset-0 size-full object-cover transition-opacity duration-300 ${loaded ? 'opacity-100' : 'opacity-0'}`}
        />
      )}
    </div>
  )
}
