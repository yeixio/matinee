import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type AnalysisMode, type ThumbnailMode } from '../api'

const modes: AnalysisMode[] = ['gentle', 'normal', 'off']
const thumbModes: ThumbnailMode[] = ['all', 'onPlay', 'off']

// The background media analysis, for admins: how far it is and how hard it works.
export function MediaAnalysis() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: st } = useQuery({
    queryKey: ['analysis'],
    queryFn: api.analysis,
    refetchInterval: (q) => (['analyzing', 'intros', 'thumbnails', 'waiting'].includes(q.state.data?.state ?? '') ? 3000 : 30_000),
  })
  const setMode = useMutation({
    mutationFn: api.setAnalysisMode,
    onSuccess: (s) => qc.setQueryData(['analysis'], s),
  })
  const setIntros = useMutation({
    mutationFn: api.setIntroDetection,
    onSuccess: (s) => qc.setQueryData(['analysis'], s),
  })
  const setThumbs = useMutation({
    mutationFn: api.setThumbnailMode,
    onSuccess: (s) => qc.setQueryData(['analysis'], s),
  })
  if (!st) return null
  const pct = st.total ? Math.floor((st.done / st.total) * 100) : 100
  const state = {
    analyzing: st.current ? t('analysis.state.lookingAt', { file: st.current }) : t('analysis.state.working'),
    intros: t('analysis.state.intros', { season: st.current ?? '' }),
    thumbnails: t('analysis.state.thumbnails', { title: st.current ?? '' }),
    waiting: t('analysis.state.waiting'),
    slowDisk: t('analysis.state.slowDisk'),
    idle: st.done >= st.total ? t('analysis.state.allDone') : t('analysis.state.upToDate'),
    off: t('analysis.state.paused'),
  }[st.state]

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('analysis.title')}</h2>
        <p className="mt-1 text-sm text-muted">
          {t('analysis.intro')}
        </p>
      </div>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-5">
        <div className="space-y-2">
          <div className="flex items-baseline justify-between gap-4 text-sm">
            <span className="min-w-0 truncate">{state}</span>
            <span className="shrink-0 text-muted tabular-nums">
              {t('analysis.progress', { done: st.done, total: st.total })}
            </span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-text/10" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-accent transition-[width]" style={{ width: `${pct}%` }} />
          </div>
          {st.unreadable > 0 && (
            <p className="text-xs text-muted">
              {t('analysis.unreadable', { count: st.unreadable })}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label={t('analysis.speed')}>
          {modes.map((m) => (
            <button
              key={m}
              role="radio"
              aria-checked={st.mode === m}
              title={t(`analysis.mode.${m}.hint`)}
              disabled={setMode.isPending}
              onClick={() => setMode.mutate(m)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                st.mode === m ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              {t(`analysis.mode.${m}.label`)}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted">{modes.includes(st.mode) && t(`analysis.mode.${st.mode}.hint`)}</p>
        {setMode.error && <p className="text-sm text-danger">{setMode.error.message}</p>}
      </div>
      {st.intros && (
        <label className="flex cursor-pointer items-start gap-4 rounded-xl border border-line/60 bg-surface/50 p-5">
          <span className="min-w-0 flex-1">
            <span className="block font-medium">{t('analysis.intros.label')}</span>
            <span className="mt-1 block text-sm text-muted">{t('analysis.intros.hint')}</span>
            {st.intros.on && st.intros.total > 0 && (
              <span className="mt-2 block text-xs text-muted tabular-nums">
                {t('analysis.intros.progress', { done: st.intros.done, total: st.intros.total, found: st.intros.found })}
              </span>
            )}
          </span>
          <input
            type="checkbox"
            className="mt-1 size-5 accent-[var(--color-accent)]"
            checked={st.intros.on}
            disabled={setIntros.isPending}
            onChange={() => setIntros.mutate(!st.intros!.on)}
          />
        </label>
      )}
      {st.thumbnails && (
        <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-5">
          <div>
            <span className="block font-medium">{t('analysis.thumbnails.label')}</span>
            <span className="mt-1 block text-sm text-muted">{t('analysis.thumbnails.hint')}</span>
            {st.thumbnails.mode === 'all' && st.thumbnails.total > 0 && (
              <span className="mt-2 block text-xs text-muted tabular-nums">
                {t('analysis.thumbnails.progress', { done: st.thumbnails.done, total: st.thumbnails.total })}
              </span>
            )}
          </div>
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label={t('analysis.thumbnails.label')}>
            {thumbModes.map((m) => (
              <button
                key={m}
                role="radio"
                aria-checked={st.thumbnails!.mode === m}
                disabled={setThumbs.isPending}
                onClick={() => setThumbs.mutate(m)}
                className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                  st.thumbnails!.mode === m ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
                }`}
              >
                {t(`analysis.thumbnails.mode.${m}.label`)}
              </button>
            ))}
          </div>
          <p className="text-xs text-muted">{thumbModes.includes(st.thumbnails.mode) && t(`analysis.thumbnails.mode.${st.thumbnails.mode}.hint`)}</p>
          {setThumbs.error && <p className="text-sm text-danger">{setThumbs.error.message}</p>}
        </div>
      )}
    </section>
  )
}
