import { X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api'
import { Button } from '../components/ui'
import { startGame, type GameStream, type Lag } from './stream'

/**
 * A game streamed from the server, full screen: a file picked on the Games
 * page, the lag test, or a game in a Games library (itemId).
 */
export function GamePlayer({
  name,
  path,
  itemId,
  hostApp,
  steam,
  lagTest,
  onClose,
}: {
  name: string
  path?: string
  itemId?: number
  /** A PC game on the game host, by its Steam app id. */
  hostApp?: number
  /** Steam itself on the game host, in Big Picture. */
  steam?: boolean
  lagTest?: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const video = useRef<HTMLVideoElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const [lag, setLag] = useState<Lag>({})
  const [error, setError] = useState<string>()
  const [starting, setStarting] = useState(true)
  // The lag numbers are for trying things out; a game from the library plays without them.
  // A browser that won't start sound without a tap or a key press leaves the
  // picture paused until there is one.
  const [blocked, setBlocked] = useState(false)
  const [showLag, setShowLag] = useState(itemId === undefined)

  useEffect(() => {
    let stream: GameStream | undefined
    let cancelled = false
    box.current?.requestFullscreen?.().catch(() => {})
    startGame({
      path,
      itemId,
      hostApp,
      steam,
      lagTest,
      video: video.current!,
      onLag: (l) => {
        setLag(l)
        setStarting(false)
      },
      onEnd: (err) => {
        if (err) setError(t('games.lostConnection'))
      },
    })
      .then((s) => {
        if (cancelled) s.stop()
        else stream = s
      })
      .catch((e) => {
        setError(e instanceof ApiError ? e.message : t('games.couldntStart'))
        setStarting(false)
      })
    return () => {
      cancelled = true
      stream?.stop()
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
    }
  }, [path, itemId, hostApp, steam, lagTest, t])

  const ms = (v?: number) => (v === undefined ? '…' : t('games.ms', { value: Math.round(v) }))
  return (
    <div ref={box} className="fixed inset-0 z-50 flex flex-col bg-black text-white">
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{name}</div>
          <div className="truncate text-xs text-white/60">{lag.controller ? t('games.controller', { name: lag.controller }) : t('games.pressButton')}</div>
        </div>
        <button type="button" className="rounded-lg px-3 py-1.5 text-sm text-white/70 hover:bg-white/10" onClick={() => setShowLag(!showLag)}>
          {showLag ? t('games.hideLag') : t('games.showLag')}
        </button>
        <button type="button" className="rounded-full p-2 hover:bg-white/10" aria-label={t('games.stop')} onClick={onClose}>
          <X className="size-5" />
        </button>
      </div>
      <div className="relative min-h-0 flex-1">
        <video
          ref={video}
          autoPlay
          playsInline
          className="absolute inset-0 size-full object-contain [image-rendering:pixelated]"
          onLoadedMetadata={(e) => e.currentTarget.play().then(() => setBlocked(false), () => setBlocked(true))}
        />
        {blocked && !error && (
          <div className="absolute inset-0 grid place-items-center">
            <Button variant="primary" autoFocus onClick={() => video.current?.play().then(() => setBlocked(false), () => {})}>
              {t('games.tapToPlay')}
            </Button>
          </div>
        )}
        {starting && !error && <div className="absolute inset-0 grid place-items-center text-white/70">{t('games.starting')}</div>}
        {error && (
          <div className="absolute inset-0 grid place-items-center p-6 text-center">
            <div>
              <p>{error}</p>
              <Button variant="primary" className="mt-4" onClick={onClose}>
                {t('games.back')}
              </Button>
            </div>
          </div>
        )}
        {showLag && !error && !starting && (
          <dl className="absolute end-4 bottom-4 grid grid-cols-[auto_auto] gap-x-4 gap-y-0.5 rounded-xl bg-black/70 px-4 py-3 text-xs tabular-nums">
            {lagTest && (
              <>
                <dt className="text-white/60">{t('games.lag.measured')}</dt>
                <dd className="text-end font-semibold">{lag.measured === undefined ? t('games.lag.pressA') : ms(lag.measured)}</dd>
              </>
            )}
            <dt className="text-white/60">{t('games.lag.network')}</dt>
            <dd className="text-end">{ms(lag.roundTrip)}</dd>
            <dt className="text-white/60">{t('games.lag.server')}</dt>
            <dd className="text-end">{ms(lag.server)}</dd>
            <dt className="text-white/60">{t('games.lag.decode')}</dt>
            <dd className="text-end">{ms(lag.decode)}</dd>
            <dt className="text-white/60">{t('games.lag.fps')}</dt>
            <dd className="text-end">{lag.fps === undefined ? '…' : Math.round(lag.fps)}</dd>
          </dl>
        )}
      </div>
    </div>
  )
}
