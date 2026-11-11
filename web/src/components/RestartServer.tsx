import { useQueryClient } from '@tanstack/react-query'
import { CircleCheck, Loader2, RotateCw, AlertTriangle } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import { backoff, health } from '../player/reconnect'
import { Button } from './ui'

// Settings → Server health → Restart server: an admin restarting Matinee by
// hand (POST /admin/restart). The server stops the way it does for a
// deploy, so players say it's restarting and pick up where they were, and
// this page waits for /health to answer from a new run (another bootId).

// SLOW is when the page says the server is taking long to come back. It
// keeps waiting after that.
const SLOW_MS = 2 * 60_000

type State = { step: 'idle' } | { step: 'restarting'; since: number; slow: boolean } | { step: 'back' } | { step: 'failed'; error: string }

/** Waits until /health answers from a run other than bootId. */
export async function waitForNewRun(bootId: string, signal: AbortSignal, onSlow: () => void, sleep = delay): Promise<void> {
  const start = Date.now()
  for (let n = 0; !signal.aborted; n++) {
    try {
      const h = await health(signal)
      if (h.bootId && h.bootId !== bootId && h.state !== 'restarting') return
    } catch {
      // Not answering yet: it's on its way down or back up.
    }
    if (Date.now() - start > SLOW_MS) onSlow()
    await sleep(backoff(n), signal)
  }
  throw new Error('aborted')
}

const delay = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, ms)
    signal.addEventListener('abort', () => {
      window.clearTimeout(timer)
      resolve()
    })
  })

/** The button, for the top of Server health, and what's happening once it's pressed. */
export function RestartServer({ playing }: { playing: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [state, setState] = useState<State>({ step: 'idle' })
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])

  const restart = async () => {
    const ask = playing > 0 ? t('settings.restart.confirmPlaying', { count: playing }) : t('settings.restart.confirm')
    if (!confirm(ask)) return
    abort.current?.abort()
    const ctl = new AbortController()
    abort.current = ctl
    try {
      const { bootId } = await api.restartServer()
      setState({ step: 'restarting', since: Date.now(), slow: false })
      await waitForNewRun(bootId, ctl.signal, () => setState((s) => (s.step === 'restarting' && !s.slow ? { ...s, slow: true } : s)))
      setState({ step: 'back' })
      // Everything on the page is from the old run.
      qc.invalidateQueries()
    } catch (e) {
      if (!ctl.signal.aborted) setState({ step: 'failed', error: (e as Error).message })
    }
  }

  const busy = state.step === 'restarting'
  return (
    <div className="flex flex-col items-start gap-2 sm:items-end">
      <Button variant="danger" icon={<RotateCw className={`size-4 ${busy ? 'animate-spin' : ''}`} />} disabled={busy} onClick={restart}>
        {busy ? t('settings.restart.restarting') : t('settings.restart.button')}
      </Button>
      {state.step !== 'idle' && (
        <p role="status" className={`flex items-start gap-2 text-sm ${state.step === 'failed' || (busy && state.slow) ? 'text-danger' : 'text-muted'}`}>
          {state.step === 'restarting' &&
            (state.slow ? (
              <>
                <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.restart.slow')}
              </>
            ) : (
              <>
                <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin" /> {t('settings.restart.waiting')}
              </>
            ))}
          {state.step === 'back' && (
            <>
              <CircleCheck className="mt-0.5 size-4 shrink-0 text-accent" /> {t('settings.restart.back')}
            </>
          )}
          {state.step === 'failed' && (
            <>
              <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.restart.failed', { error: state.error })}
            </>
          )}
        </p>
      )}
    </div>
  )
}
