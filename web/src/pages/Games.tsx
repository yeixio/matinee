import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, Folder, Gamepad2, Gauge, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../api'
import { Button } from '../components/ui'
import { startGame, type GameStream, type Lag } from '../games/stream'

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
  const [playing, setPlaying] = useState<{ name: string; path?: string; lagTest?: boolean }>()

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

      {playing && <Player {...playing} onClose={() => setPlaying(undefined)} />}
    </div>
  )
}

function Player({ name, path, lagTest, onClose }: { name: string; path?: string; lagTest?: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const video = useRef<HTMLVideoElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const [lag, setLag] = useState<Lag>({})
  const [error, setError] = useState<string>()
  const [starting, setStarting] = useState(true)
  const [showLag, setShowLag] = useState(true)

  useEffect(() => {
    let stream: GameStream | undefined
    let cancelled = false
    box.current?.requestFullscreen?.().catch(() => {})
    startGame({
      path,
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
  }, [path, lagTest, t])

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
        <video ref={video} autoPlay playsInline className="absolute inset-0 size-full object-contain [image-rendering:pixelated]" />
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
