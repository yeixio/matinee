import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Library, type WatchMode } from '../api'
import i18n from '../i18n'

// "30 seconds", "15 minutes", "2 hours".
export function every(seconds: number) {
  const [count, unit] = seconds % 3600 === 0 ? [seconds / 3600, 'hours'] : seconds % 60 === 0 ? [seconds / 60, 'minutes'] : [seconds, 'seconds']
  return i18n.t(`common:every.${unit}`, { count })
}

// One line on how a library notices new files, for its row in Settings.
export function watchSummary(lib: Library, pollSeconds: number) {
  const methods = new Set(lib.watch.map((w) => w.method))
  if (methods.size === 0) return ''
  const t = i18n.t
  if (methods.size > 1) return t('newFiles.summary.some', { every: every(pollSeconds) })
  const network = lib.watch.some((w) => w.reason?.startsWith('network share'))
  switch ([...methods][0]) {
    case 'events':
      return t('newFiles.summary.watching')
    case 'polling':
      return t(network ? 'newFiles.summary.pollingShare' : 'newFiles.summary.polling', { every: every(pollSeconds) })
    default:
      return t('newFiles.summary.off')
  }
}

const modes: WatchMode[] = ['auto', 'poll', 'off']

// How the server notices files added to, renamed in or removed from library folders.
export function NewFiles() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: (libraryWatch: WatchMode) => api.saveSettings({ libraryWatch }),
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s)
      // The server sets the new mode up in a moment.
      setTimeout(() => qc.invalidateQueries({ queryKey: ['libraries'] }), 500)
    },
  })
  if (!data) return null
  const poll = every(data.watchPollSeconds || 30)
  const current = modes.find((m) => m === data.libraryWatch) ?? modes[0]
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('newFiles.title')}</h2>
        <p className="mt-1 text-sm text-muted">
          {t('newFiles.intro')}
        </p>
      </div>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-5">
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label={t('newFiles.watch')}>
          {modes.map((m) => (
            <button
              key={m}
              role="radio"
              aria-checked={current === m}
              disabled={save.isPending}
              onClick={() => save.mutate(m)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                current === m ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              {t(`newFiles.mode.${m}.label`)}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted">{t(`newFiles.mode.${current}.hint`, { every: poll })}</p>
        <p className="text-xs text-muted">
          {data.scanIntervalSeconds > 0
            ? t('newFiles.rescan', { every: every(data.scanIntervalSeconds) })
            : t('newFiles.rescanOff')}
        </p>
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      </div>
    </section>
  )
}
