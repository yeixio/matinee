import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type AnalysisMode } from '../api'

const modes: { mode: AnalysisMode; label: string; hint: string }[] = [
  { mode: 'gentle', label: 'Gentle', hint: 'One file every few seconds, and none while the server is converting something for playback.' },
  { mode: 'normal', label: 'Normal', hint: 'Files back to back, still at the lowest priority.' },
  { mode: 'off', label: 'Off', hint: 'Paused until you turn it back on.' },
]

// The background media analysis, for admins: how far it is and how hard it works.
export function MediaAnalysis() {
  const qc = useQueryClient()
  const { data: st } = useQuery({
    queryKey: ['analysis'],
    queryFn: api.analysis,
    refetchInterval: (q) => (q.state.data?.state === 'analyzing' || q.state.data?.state === 'waiting' ? 3000 : 30_000),
  })
  const setMode = useMutation({
    mutationFn: api.setAnalysisMode,
    onSuccess: (s) => qc.setQueryData(['analysis'], s),
  })
  if (!st) return null
  const pct = st.total ? Math.floor((st.done / st.total) * 100) : 100
  const state = {
    analyzing: st.current ? `Looking at ${st.current}` : 'Working',
    waiting: 'Waiting for playback to finish',
    slowDisk: 'Files are slow to read; resting for a few minutes',
    idle: st.done >= st.total ? 'Every file is analyzed' : 'Up to date',
    off: 'Paused',
  }[st.state]

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">Media analysis</h2>
        <p className="mt-1 text-sm text-muted">
          Matinee slowly reads each file for its quality, sound and languages, and again whenever a file changes.
        </p>
      </div>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-5">
        <div className="space-y-2">
          <div className="flex items-baseline justify-between gap-4 text-sm">
            <span className="min-w-0 truncate">{state}</span>
            <span className="shrink-0 text-muted tabular-nums">
              {st.done.toLocaleString()} of {st.total.toLocaleString()}
            </span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-text/10" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-accent transition-[width]" style={{ width: `${pct}%` }} />
          </div>
          {st.unreadable > 0 && (
            <p className="text-xs text-muted">
              {st.unreadable === 1 ? '1 file' : `${st.unreadable.toLocaleString()} files`} couldn't be read.
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Speed">
          {modes.map((m) => (
            <button
              key={m.mode}
              role="radio"
              aria-checked={st.mode === m.mode}
              title={m.hint}
              disabled={setMode.isPending}
              onClick={() => setMode.mutate(m.mode)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                st.mode === m.mode ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              {m.label}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted">{modes.find((m) => m.mode === st.mode)?.hint}</p>
        {setMode.error && <p className="text-sm text-danger">{setMode.error.message}</p>}
      </div>
    </section>
  )
}
