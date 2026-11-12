import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ExternalLink, Folder, Gamepad2, Gauge, Monitor } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { api, gameHostDesktopUrl } from '../api'
import { Button } from '../components/ui'
import { GamePlayer } from '../games/GamePlayer'

/**
 * Playing games streamed from the server, first version: an admin picks a
 * game file, or the lag test, and plays it here with a controller or the
 * keyboard, with the lag shown as it goes.
 */
export default function Games() {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const path = params.get('path') ?? undefined
  const streaming = useQuery({ queryKey: ['game-streaming'], queryFn: api.gameStreaming })
  const folder = useQuery({ queryKey: ['game-files', path], queryFn: () => api.gameFiles(path), enabled: !!streaming.data?.available })
  // A game host (Steam on a PC with a graphics card) answers 404 when there
  // isn't one, and its section stays hidden.
  const host = useQuery({ queryKey: ['game-host'], queryFn: api.gameHostGames, enabled: !!streaming.data?.available, retry: false })
  const [playing, setPlaying] = useState<{ name: string; path?: string; hostApp?: number; steam?: boolean; lagTest?: boolean }>()

  if (streaming.data && !streaming.data.available) {
    return (
      <div className="max-w-3xl">
        <h1 className="text-2xl font-bold tracking-tight">{t('games.title')}</h1>
        <p className="mt-4 text-muted">{t('settings.games.notSetUp')}</p>
      </div>
    )
  }

  return (
    <div className="max-w-3xl space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t('games.title')}</h1>
        <p className="mt-1 text-sm text-muted">{t('games.intro')}</p>
        {streaming.data && streaming.data.systems.length > 0 && (
          <p className="mt-2 text-sm text-muted">{t('settings.games.ready', { systems: streaming.data.systems.map((s) => s.name).join(', ') })}</p>
        )}
      </div>

      {streaming.data?.lagTest && (
        <section className="flex items-center gap-4 rounded-xl border border-line/60 bg-surface/50 p-4">
          <Gauge className="size-6 shrink-0 text-accent" />
          <div className="min-w-0 flex-1">
            <div className="font-medium">{t('games.lagTest')}</div>
            <div className="text-sm text-muted">{t('games.lagTestHint')}</div>
          </div>
          <Button variant="primary" onClick={() => setPlaying({ name: t('games.lagTest'), lagTest: true })}>
            {t('games.play')}
          </Button>
        </section>
      )}

      {host.data && (
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
          <ul className="rounded-xl border border-line/60 bg-surface/50 py-1">
            {host.data.games.map((g) => (
              <li key={g.appId}>
                <button type="button" className="flex w-full items-center gap-3 px-4 py-2 text-start hover:bg-text/5" onClick={() => setPlaying({ name: g.name, hostApp: g.appId })}>
                  <Monitor className="size-4 shrink-0 text-accent" />
                  <span className="truncate">{g.name}</span>
                </button>
              </li>
            ))}
            {host.data.games.length === 0 && <li className="px-4 py-3 text-sm text-muted">{t('games.hostGamesEmpty')}</li>}
          </ul>
        </section>
      )}

      <section className="space-y-2">
        <h2 className="text-lg font-semibold">{t('games.pickGame')}</h2>
        <p className="text-sm text-muted">{t('games.pickGameHintAny')}</p>
        <div className="rounded-xl border border-line/60 bg-surface/50">
          <div className="flex items-center gap-2 border-b border-line/60 px-3 py-2 text-sm">
            {folder.data && folder.data.parent !== folder.data.path && (
              <button
                type="button"
                className="rounded-lg p-1 text-muted hover:bg-text/10 hover:text-text"
                aria-label={t('games.up')}
                onClick={() => setParams({ path: folder.data.parent })}
              >
                <ChevronLeft className="size-4 rtl:-scale-x-100" />
              </button>
            )}
            <span className="truncate text-muted" dir="ltr">
              {folder.data?.path}
            </span>
          </div>
          {folder.error && <p className="p-4 text-sm text-danger">{(folder.error as Error).message}</p>}
          <ul className="max-h-[60vh] overflow-y-auto py-1">
            {folder.data?.dirs.map((d) => (
              <li key={d.path}>
                <button type="button" className="flex w-full items-center gap-3 px-4 py-2 text-start hover:bg-text/5" onClick={() => setParams({ path: d.path })}>
                  <Folder className="size-4 shrink-0 text-muted" />
                  <span className="truncate">{d.name}</span>
                </button>
              </li>
            ))}
            {folder.data?.files.map((f) => (
              <li key={f.path}>
                <button type="button" className="flex w-full items-center gap-3 px-4 py-2 text-start hover:bg-text/5" onClick={() => setPlaying({ name: f.name, path: f.path })}>
                  <Gamepad2 className="size-4 shrink-0 text-accent" />
                  <span className="truncate">{f.name}</span>
                </button>
              </li>
            ))}
            {folder.data && folder.data.dirs.length + folder.data.files.length === 0 && <li className="px-4 py-3 text-sm text-muted">{t('games.emptyFolder')}</li>}
          </ul>
        </div>
      </section>

      {playing && <GamePlayer {...playing} onClose={() => setPlaying(undefined)} />}
    </div>
  )
}
