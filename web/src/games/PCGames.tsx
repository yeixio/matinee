import { useQuery } from '@tanstack/react-query'
import { ExternalLink, Gamepad2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, gameHostDesktopUrl } from '../api'
import { Button } from '../components/ui'
import { GamePlayer } from './GamePlayer'

/**
 * PC games: Steam on the game host (a PC with a graphics card), and its
 * desktop. One Steam button: players pick, add and remove games inside
 * Steam's Big Picture, like on a Steam Deck. Shown on the Games page and at
 * the top of a Games library. Hidden when there's no game host: it answers
 * 404 then (403 for someone who isn't an admin). A game host that's set up
 * but not answering is shown with why, so it doesn't just vanish.
 */
export function PCGames() {
  const { t } = useTranslation()
  const host = useQuery({ queryKey: ['game-host'], queryFn: api.gameHostGames, retry: false })
  const [playing, setPlaying] = useState<{ name: string; steam: true }>()
  const err = host.error
  if (err instanceof ApiError && err.status !== 404 && err.status !== 403) {
    return (
      <section className="space-y-2">
        <h2 className="text-lg font-semibold">{t('games.hostGames')}</h2>
        <p className="text-sm text-danger">{err.message}</p>
      </section>
    )
  }
  if (!host.data) return null
  return (
    <section className="space-y-2">
      <h2 className="text-lg font-semibold">{t('games.hostGames')}</h2>
      <p className="text-sm text-muted">{t('games.hostGamesHint')}</p>
      <div className="flex items-center gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <Gamepad2 className="size-6 shrink-0 text-accent" />
        <div className="min-w-0 flex-1">
          <div className="font-medium">{t('games.steamPlay')}</div>
          <div className="text-sm text-muted">{t('games.steamPlayHint')}</div>
        </div>
        <Button variant="primary" onClick={() => setPlaying({ name: t('games.steamPlay'), steam: true })}>
          {t('games.play')}
        </Button>
      </div>
      <a
        href={gameHostDesktopUrl}
        target="_blank"
        rel="noreferrer"
        className="flex items-center gap-4 rounded-xl border border-line/60 bg-surface/50 p-4 hover:bg-text/5"
      >
        <ExternalLink className="size-6 shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <div className="font-medium">{t('games.steamDesktop')}</div>
          <div className="text-sm text-muted">{t('games.steamDesktopHint')}</div>
        </div>
      </a>
      {playing && <GamePlayer {...playing} onClose={() => setPlaying(undefined)} />}
    </section>
  )
}
