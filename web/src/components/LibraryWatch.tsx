import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Library, type WatchMode } from '../api'
import i18n from '../i18n'
import { inputClass } from './ui'

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
        {current !== 'off' && <CheckEvery seconds={data.watchPollSeconds} />}
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      </div>
      <FullRescans />
    </section>
  )
}

// How often polled folders can be checked, in seconds.
const checkChoices = [30, 60, 2 * 60, 5 * 60, 10 * 60]

// How often folders that don't announce changes (network shares, or every
// folder on a timer) are checked for new files.
function CheckEvery({ seconds }: { seconds: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const save = useMutation({
    mutationFn: (watchPollSeconds: number) => api.saveSettings({ watchPollSeconds }),
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s)
      setTimeout(() => qc.invalidateQueries({ queryKey: ['libraries'] }), 500)
    },
  })
  const choices = checkChoices.includes(seconds) ? checkChoices : [...checkChoices, seconds].sort((a, b) => a - b)
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium">{t('newFiles.checkLabel')}</span>
      <select className={inputClass} value={seconds} disabled={save.isPending} onChange={(e) => save.mutate(Number(e.target.value))}>
        {choices.map((s) => (
          <option key={s} value={s}>
            {t('newFiles.rescanEvery', { every: every(s) })}
          </option>
        ))}
      </select>
      <span className="block text-xs text-muted">{t('newFiles.checkHint')}</span>
      {save.error && <span className="block text-sm text-danger">{save.error.message}</span>}
    </label>
  )
}

// The full rescans an admin can pick, in seconds; 0 is never.
const rescanChoices = [0, 15 * 60, 30 * 60, 3600, 6 * 3600, 12 * 3600, 24 * 3600]

// How often every library is rescanned in full, which catches what watching
// for new files can't.
function FullRescans() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: (scanIntervalSeconds: number) => api.saveSettings({ scanIntervalSeconds }),
    onSuccess: (s) => qc.setQueryData(['settings'], s),
  })
  if (!data) return null
  const current = data.scanIntervalSeconds
  // One set on the server's command line, such as 2h, is offered as it is.
  const choices = rescanChoices.includes(current) ? rescanChoices : [...rescanChoices, current].sort((a, b) => a - b)
  const next = data.nextScan && new Date(data.nextScan).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })
  return (
    <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-5">
      <label className="block space-y-1.5">
        <span className="font-medium">{t('newFiles.rescanLabel')}</span>
        <select
          className={inputClass}
          value={current}
          disabled={save.isPending}
          onChange={(e) => save.mutate(Number(e.target.value))}
        >
          {choices.map((s) => (
            <option key={s} value={s}>
              {s === 0 ? t('newFiles.rescanNever') : t('newFiles.rescanEvery', { every: every(s) })}
            </option>
          ))}
        </select>
      </label>
      <p className="text-xs text-muted">{t('newFiles.rescanHint')}</p>
      {next && <p className="text-xs text-muted">{t('newFiles.rescanNext', { time: next })}</p>}
      {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
    </div>
  )
}
