import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, DatabaseBackup, Download, History, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, type BackupSettings as Settings, type Backups } from '../api'
import i18n from '../i18n'
import { Button, inputClass } from './ui'

// Settings → Server health → Backups: copies of the server's SQLite
// database (internal/backup), made before upgrades, on a schedule and by
// hand, to download or restore at the next start.

const card = 'rounded-xl border border-line/60 bg-surface/50'
const schedules = [0, 6, 12, 24, 168]
const scheduleLabels: Record<number, 'settings.backups.never' | 'settings.backups.every6' | 'settings.backups.every12' | 'settings.backups.daily' | 'settings.backups.weekly'> = {
  0: 'settings.backups.never',
  6: 'settings.backups.every6',
  12: 'settings.backups.every12',
  24: 'settings.backups.daily',
  168: 'settings.backups.weekly',
}

const when = (ms: number) => new Date(ms).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })
const size = (bytes: number) =>
  bytes >= 1 << 30 ? `${(bytes / (1 << 30)).toFixed(1)} GB` : bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.ceil(bytes / 1024)} KB`

export function BackupSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['backups'], queryFn: api.backups })
  const done = (b: Backups) => qc.setQueryData(['backups'], b)
  const make = useMutation({ mutationFn: api.makeBackup, onSuccess: done })
  const save = useMutation({ mutationFn: (s: Settings) => api.saveBackupSettings(s), onSuccess: done })
  const restore = useMutation({ mutationFn: api.restoreBackup, onSuccess: done })
  const cancel = useMutation({ mutationFn: api.cancelRestore, onSuccess: done })
  const remove = useMutation({ mutationFn: api.deleteBackup, onSuccess: done })
  if (!data) return null

  if (!data.supported) {
    return (
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">{t('settings.backups.title')}</h2>
        <p className={`${card} p-4 text-sm text-muted`}>{t('settings.backups.postgres')}</p>
      </section>
    )
  }
  const pending = data.restore && data.backups.find((b) => b.name === data.restore)
  const error = make.error?.message ?? data.lastError
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.backups.title')}</h2>
      <p className="text-sm text-muted">{t('settings.backups.intro', { dir: data.dir })}</p>

      {data.restore && (
        <div role="status" className={`${card} flex flex-wrap items-center gap-3 border-accent/50 p-4 text-sm`}>
          <History className="size-5 shrink-0 text-accent" />
          <span className="min-w-0 flex-1">{t('settings.backups.pending', { time: pending ? when(pending.time) : data.restore })}</span>
          <Button disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            {t('settings.backups.cancelRestore')}
          </Button>
        </div>
      )}
      {error && (
        <p className="flex items-start gap-2 text-sm text-danger">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.backups.failed', { error })}
        </p>
      )}

      <div className={`${card} flex flex-wrap items-end gap-4 p-4`}>
        <label className="space-y-1 text-sm">
          <span className="block font-medium">{t('settings.backups.every')}</span>
          <select
            className={inputClass}
            value={data.settings.everyHours}
            disabled={save.isPending}
            onChange={(e) => save.mutate({ ...data.settings, everyHours: Number(e.target.value) })}
          >
            {!schedules.includes(data.settings.everyHours) && <option value={data.settings.everyHours}>{data.settings.everyHours} h</option>}
            {schedules.map((h) => (
              <option key={h} value={h}>
                {t(scheduleLabels[h])}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-sm">
          <span className="block font-medium">{t('settings.backups.keep')}</span>
          <input
            type="number"
            min={1}
            max={100}
            className={`${inputClass} w-24`}
            defaultValue={data.settings.keep}
            disabled={save.isPending}
            onBlur={(e) => {
              const keep = Number(e.target.value)
              if (keep >= 1 && keep !== data.settings.keep) save.mutate({ ...data.settings, keep })
            }}
          />
        </label>
        <Button variant="primary" className="ms-auto" icon={<DatabaseBackup className="size-4" />} disabled={make.isPending} onClick={() => make.mutate()}>
          {make.isPending ? t('settings.backups.making') : t('settings.backups.makeNow')}
        </Button>
      </div>

      <div className={`${card} divide-y divide-line/60`}>
        {data.backups.length === 0 && <p className="p-4 text-sm text-muted">{t('settings.backups.none')}</p>}
        {data.backups.map((b) => (
          <div key={b.name} className="flex flex-wrap items-center gap-3 p-4">
            <div className="min-w-0 flex-1">
              <div className="font-medium">{when(b.time)}</div>
              <div className="text-sm text-muted">
                {t(`settings.backups.reason.${b.reason}`)} · {size(b.size)}
              </div>
            </div>
            <a href={api.backupUrl(b.name)} download className="rounded-full p-2 text-muted hover:bg-text/10 hover:text-text" aria-label={t('settings.backups.download')} title={t('settings.backups.download')}>
              <Download className="size-4" />
            </a>
            <Button
              disabled={restore.isPending || data.restore === b.name}
              onClick={() => confirm(t('settings.backups.restoreConfirm', { time: when(b.time) })) && restore.mutate(b.name)}
            >
              {t('settings.backups.restore')}
            </Button>
            <button
              className="rounded-full p-2 text-muted hover:bg-text/10 hover:text-text"
              aria-label={t('settings.backups.delete')}
              title={t('settings.backups.delete')}
              disabled={remove.isPending}
              onClick={() => confirm(t('settings.backups.deleteConfirm', { time: when(b.time) })) && remove.mutate(b.name)}
            >
              <Trash2 className="size-4" />
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}
