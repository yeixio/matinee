import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ArrowRightLeft, CircleCheck, Database, Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type DatabaseInfo } from '../api'
import i18n from '../i18n'
import { waitForNewRun } from './RestartServer'
import { Button, inputClass } from './ui'

// Settings → Server health → Database: which database Matinee uses, and
// moving it to another one (internal/store/move.go): SQLite to PostgreSQL,
// to another PostgreSQL server, or back to a SQLite file. The server moves
// it as it starts, before anything else uses it, then opens on the new one.

const card = 'rounded-xl border border-line/60 bg-surface/50'
const when = (s: number) => new Date(s * 1000).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })

type Step = { step: 'idle' } | { step: 'moving' } | { step: 'moved' } | { step: 'failed'; error: string }

export function DatabaseMove() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['database'], queryFn: api.database })
  const [kind, setKind] = useState<'postgres' | 'sqlite'>('postgres')
  const [to, setTo] = useState('')
  const [state, setState] = useState<Step>({ step: 'idle' })
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const done = (d: DatabaseInfo) => qc.setQueryData(['database'], d)
  const check = useMutation({ mutationFn: () => api.checkDatabaseMove(to.trim()) })
  const cancel = useMutation({ mutationFn: api.cancelDatabaseMove, onSuccess: done })
  if (!data) return null

  const target = to.trim() || (kind === 'sqlite' ? (data.suggestedFile ?? '') : '')
  const move = async () => {
    if (!target || !confirm(t('settings.database.moveConfirm', { where: target.replace(/:[^:@/]*@/, ':•••@') }))) return
    abort.current?.abort()
    const ctl = new AbortController()
    abort.current = ctl
    try {
      const answer = await api.moveDatabase(target)
      done(answer)
      if (!answer.restarting || !answer.bootId) return
      setState({ step: 'moving' })
      await waitForNewRun(answer.bootId, ctl.signal, () => {})
      const after = await api.database()
      done(after)
      setState(after.lastError ? { step: 'failed', error: after.lastError } : { step: 'moved' })
      setTo('')
      // Everything else on the page is from the old run too.
      qc.invalidateQueries()
    } catch (e) {
      if (!ctl.signal.aborted) setState({ step: 'failed', error: (e as Error).message })
    }
  }
  const busy = state.step === 'moving'
  const error = state.step === 'failed' ? state.error : data.lastError

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.database.title')}</h2>
      <p className="flex items-start gap-2 text-sm text-muted">
        <Database className="mt-0.5 size-4 shrink-0" />
        <span className="min-w-0 break-all">
          {t(data.kind === 'postgres' ? 'settings.database.usesPostgres' : 'settings.database.usesSqlite', { where: data.where })}
        </span>
      </p>
      {data.movedFrom && <p className="text-sm text-muted">{t('settings.database.movedFrom', { from: data.movedFrom.from, time: when(data.movedFrom.at), rows: data.movedFrom.rows.toLocaleString(i18n.language) })}</p>}
      {data.configured && (
        <p role="status" className={`${card} flex items-start gap-2 border-accent/50 p-4 text-sm`}>
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-accent" />
          <span className="min-w-0 break-all">{t('settings.database.configured', { old: data.configured })}</span>
        </p>
      )}
      {data.pending && !busy && (
        <div role="status" className={`${card} flex flex-wrap items-center gap-3 border-accent/50 p-4 text-sm`}>
          <ArrowRightLeft className="size-5 shrink-0 text-accent" />
          <span className="min-w-0 flex-1 break-all">{t('settings.database.pending', { where: data.pending })}</span>
          <Button disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            {t('settings.database.cancel')}
          </Button>
        </div>
      )}
      {busy && (
        <p role="status" className="flex items-start gap-2 text-sm text-muted">
          <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin" /> {t('settings.database.moving')}
        </p>
      )}
      {state.step === 'moved' && (
        <p role="status" className="flex items-start gap-2 text-sm">
          <CircleCheck className="mt-0.5 size-4 shrink-0 text-accent" /> {t('settings.database.moved')}
        </p>
      )}
      {error && (
        <p className="flex items-start gap-2 text-sm text-danger">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {t('settings.database.failed', { error })}
        </p>
      )}

      <div className={`${card} space-y-4 p-4`}>
        <p className="text-sm text-muted">{t('settings.database.intro')}</p>
        <fieldset className="flex flex-wrap gap-x-6 gap-y-2">
          <legend className="sr-only">{t('settings.database.moveTo')}</legend>
          {(['postgres', 'sqlite'] as const).map((k) => (
            <label key={k} className={`flex items-center gap-2 text-sm ${k === 'sqlite' && !data.sqliteOk ? 'opacity-50' : 'cursor-pointer'}`}>
              <input
                type="radio"
                name="database-kind"
                className="accent-[var(--color-accent)]"
                checked={kind === k}
                disabled={busy || (k === 'sqlite' && !data.sqliteOk)}
                onChange={() => {
                  setKind(k)
                  setTo('')
                  check.reset()
                }}
              />
              {t(k === 'postgres' ? 'settings.database.postgres' : 'settings.database.sqlite')}
            </label>
          ))}
        </fieldset>
        {!data.sqliteOk && <p className="text-xs text-faint">{t('settings.database.noSqliteHere')}</p>}
        <label className="block space-y-1 text-sm">
          <span className="block font-medium">{t(kind === 'postgres' ? 'settings.database.url' : 'settings.database.file')}</span>
          <input
            className={inputClass}
            value={to}
            spellCheck={false}
            autoComplete="off"
            disabled={busy}
            placeholder={kind === 'postgres' ? 'postgres://matinee:password@tower.local:5432/matinee?sslmode=disable' : (data.suggestedFile ?? '/data/matinee.db')}
            onChange={(e) => {
              setTo(e.target.value)
              check.reset()
            }}
          />
          <span className="block text-xs text-faint">{t(kind === 'postgres' ? 'settings.database.urlHint' : 'settings.database.fileHint')}</span>
        </label>
        {check.isSuccess && (
          <p role="status" className="flex items-start gap-2 text-sm">
            <CircleCheck className="mt-0.5 size-4 shrink-0 text-accent" /> <span className="min-w-0 break-all">{t('settings.database.checkOk', { where: check.data.where })}</span>
          </p>
        )}
        {check.isError && (
          <p className="flex items-start gap-2 text-sm text-danger">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" /> {check.error.message}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button disabled={busy || check.isPending || !to.trim()} onClick={() => check.mutate()}>
            {check.isPending ? t('settings.database.checking') : t('settings.database.check')}
          </Button>
          <Button variant="primary" icon={<ArrowRightLeft className="size-4" />} disabled={busy || !target || !!data.pending} onClick={move}>
            {t('settings.database.move')}
          </Button>
        </div>
        {!data.canRestart && <p className="text-xs text-faint">{t('settings.database.restartByHand')}</p>}
      </div>
    </section>
  )
}
