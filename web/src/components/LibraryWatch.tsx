import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Library, type WatchMode } from '../api'

// "30 seconds", "15 minutes", "2 hours".
export function every(seconds: number) {
  const [n, unit] = seconds % 3600 === 0 ? [seconds / 3600, 'hour'] : seconds % 60 === 0 ? [seconds / 60, 'minute'] : [seconds, 'second']
  return `${n} ${unit}${n === 1 ? '' : 's'}`
}

// One line on how a library notices new files, for its row in Settings.
export function watchSummary(lib: Library, pollSeconds: number) {
  const methods = new Set(lib.watch.map((w) => w.method))
  if (methods.size === 0) return ''
  if (methods.size > 1) return `Some folders checked every ${every(pollSeconds)}`
  const network = lib.watch.some((w) => w.reason?.startsWith('network share'))
  switch ([...methods][0]) {
    case 'events':
      return 'Watching for new files'
    case 'polling':
      return `Checked for new files every ${every(pollSeconds)}${network ? ' (network share)' : ''}`
    default:
      return 'Not watched for new files'
  }
}

const modes: { mode: WatchMode; label: string; hint: (poll: string) => string }[] = [
  {
    mode: 'auto',
    label: 'Automatic',
    hint: (poll) =>
      `Right away on this server's own disks. Network shares (SMB, NFS) don't announce changes, so Matinee checks those every ${poll}.`,
  },
  {
    mode: 'poll',
    label: 'Check on a timer',
    hint: (poll) => `Every folder is checked every ${poll}. Pick this if new files on a disk don't show up by themselves.`,
  },
  { mode: 'off', label: 'Off', hint: () => 'New files show up at the next full rescan, or when you press Scan.' },
]

// How the server notices files added to, renamed in or removed from library folders.
export function NewFiles() {
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
  const current = modes.find((m) => m.mode === data.libraryWatch) ?? modes[0]
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">New files</h2>
        <p className="mt-1 text-sm text-muted">
          Matinee notices files you add, rename or delete and updates the library a few seconds after they finish copying.
        </p>
      </div>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-5">
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Watch for new files">
          {modes.map((m) => (
            <button
              key={m.mode}
              role="radio"
              aria-checked={current.mode === m.mode}
              disabled={save.isPending}
              onClick={() => save.mutate(m.mode)}
              className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
                current.mode === m.mode ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
              }`}
            >
              {m.label}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted">{current.hint(poll)}</p>
        <p className="text-xs text-muted">
          {data.scanIntervalSeconds > 0
            ? `Every library is also rescanned in full every ${every(data.scanIntervalSeconds)}, which catches anything else, like a file replaced in place on a share.`
            : 'Full rescans are turned off on this server (MATINEE_SCAN_INTERVAL=0).'}
        </p>
        {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      </div>
    </section>
  )
}
