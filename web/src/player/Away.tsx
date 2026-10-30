import { useTranslation } from 'react-i18next'
import { Spinner } from '../components/ui'
import type { Waiting } from './reconnect'

// While the server is away, music and books say why they stopped, over the
// bar or the full player, and that they'll pick up where they were.
export function Away({ away, onRetry, onStop }: { away: Waiting | 'gaveUp'; onRetry: () => void; onStop: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="pointer-events-none fixed inset-x-0 top-[max(env(safe-area-inset-top),1rem)] z-50 flex justify-center px-4">
      <div role="status" className="pointer-events-auto flex w-full max-w-md animate-fade items-start gap-3 rounded-2xl border border-line/60 bg-surface/95 p-4 shadow-2xl backdrop-blur">
        {away !== 'gaveUp' && <Spinner className="mt-0.5 size-6 shrink-0" />}
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-sm font-semibold">{t(`common:reconnect.${away}`)}</p>
          <p className="text-xs text-muted">{t(`common:reconnect.${away}Detail`)}</p>
          {away === 'gaveUp' && (
            <div className="flex gap-3 pt-2 text-sm">
              <button onClick={onRetry} className="rounded-full bg-accent px-4 py-1.5 font-semibold text-on-accent hover:bg-accent-strong">
                {t('common:reconnect.tryAgain')}
              </button>
              <button onClick={onStop} className="rounded-full bg-text/10 px-4 py-1.5 hover:bg-text/15">
                {t('player.closePlayer')}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// A moment's note over the full player, which covers the bar's.
export function Toast({ text }: { text: string }) {
  return (
    <div className="pointer-events-none fixed inset-x-0 top-[max(env(safe-area-inset-top),1rem)] z-50 flex justify-center px-4">
      <div role="status" className="max-w-md animate-fade rounded-full bg-text px-4 py-2 text-center text-sm font-medium text-ink shadow-lg">
        {text}
      </div>
    </div>
  )
}
