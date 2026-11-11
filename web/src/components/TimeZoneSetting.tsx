import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import { inputClass } from './ui'

// Every time zone the browser knows, such as America/Juneau.
function zones(): string[] {
  try {
    return Intl.supportedValuesOf('timeZone')
  } catch {
    return []
  }
}

// The server's time zone: the clock its log, Home's holiday rows and
// anything else that goes by the time of day use.
export function TimeZoneSetting() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: (timeZone: string) => api.saveSettings({ timeZone }),
    onSuccess: (s) => qc.setQueryData(['settings'], s),
  })
  if (!data) return null
  const all = zones()
  const list = data.timeZone && !all.includes(data.timeZone) ? [data.timeZone, ...all] : all
  const system = data.systemTimeZone ? t('timeZone.system', { zone: data.systemTimeZone }) : t('timeZone.systemUnknown')
  // A pick takes effect when the server next starts.
  const pending = (data.timeZone || data.systemTimeZone || '') !== (data.timeZoneInUse ?? '')
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('timeZone.title')}</h2>
        <p className="mt-1 text-sm text-muted">{t('timeZone.intro')}</p>
      </div>
      <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-5">
        <select
          className={inputClass}
          aria-label={t('timeZone.title')}
          value={data.timeZone}
          disabled={save.isPending}
          onChange={(e) => save.mutate(e.target.value)}
        >
          <option value="">{system}</option>
          {list.map((z) => (
            <option key={z} value={z}>
              {z.replaceAll('_', ' ')}
            </option>
          ))}
        </select>
        {pending && (
          <p className="text-sm text-accent">
            {data.timeZone ? t('timeZone.restart', { zone: data.timeZone.replaceAll('_', ' ') }) : t('timeZone.restartSystem')}
          </p>
        )}
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      </div>
    </section>
  )
}
