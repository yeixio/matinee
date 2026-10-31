import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Check, FolderOpen, Plus, Upload, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ImportPreview, type ImportRequest, type PathChange } from '../api'
import { FolderPicker } from '../pages/Settings'
import { Button, inputClass } from './ui'

// Settings → Import: bring watch history, collections and posters over from
// another media server's data folder (or its uploaded database), matched to
// this server's files by path.
export function ImportSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  // undefined: nothing picked yet; '': the uploaded database.
  const [path, setPath] = useState<string>()
  const [picking, setPicking] = useState(false)
  const [changes, setChanges] = useState<PathChange[]>([])
  const [preview, setPreview] = useState<ImportPreview>()
  const [accounts, setAccounts] = useState<Record<string, number>>({})
  const [what, setWhat] = useState({ watched: true, collections: true, posters: true })
  const fileInput = useRef<HTMLInputElement>(null)

  const { data: found } = useQuery({ queryKey: ['importFind'], queryFn: api.findImports, enabled: open })
  const { data: status } = useQuery({
    queryKey: ['importStatus'],
    queryFn: api.importStatus,
    refetchInterval: (q) => (q.state.data?.running ? 1000 : false),
  })

  const check = useMutation({
    mutationFn: (req: ImportRequest) => api.previewImport(req),
    onSuccess: (p, req) => {
      setPreview(p)
      setChanges(req.changes)
      // Keep picks already made; suggest the rest.
      setAccounts((prev) => {
        const next: Record<string, number> = {}
        for (const a of p.accounts) next[a.id] = prev[a.id] ?? a.suggested
        return next
      })
    },
  })
  const pick = (p: string) => {
    setPath(p)
    setPicking(false)
    setPreview(undefined)
    check.mutate({ path: p, changes })
  }
  const upload = useMutation({
    mutationFn: api.uploadImport,
    onSuccess: () => pick(''),
  })
  const run = useMutation({
    mutationFn: () =>
      api.runImport({
        path: path ?? '',
        changes,
        accounts: Object.fromEntries(Object.entries(accounts).filter(([, v]) => v > 0)),
        watched: what.watched,
        collections: what.collections,
        posters: what.posters && !!preview?.canPosters,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['importStatus'] }),
  })
  const recheck = (next: PathChange[]) => path !== undefined && check.mutate({ path, changes: next })

  const running = status?.running || run.isPending
  const anyAccount = Object.values(accounts).some((v) => v > 0)
  const canRun =
    !!preview && !running && preview.matched > 0 && ((what.watched && anyAccount) || what.collections || (what.posters && preview.canPosters))

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">{t('import.title')}</h2>
          <p className="text-sm text-muted">{t('import.intro')}</p>
        </div>
        {!open && (
          <Button icon={<ArrowRight className="size-4 rtl:-scale-x-100" />} onClick={() => setOpen(true)}>
            {t('import.start')}
          </Button>
        )}
      </div>

      {open && (
        <div className="space-y-5 rounded-xl border border-line/60 bg-surface/50 p-5">
          {/* Where from */}
          <div className="space-y-3">
            <h3 className="text-sm font-medium">{t('import.where')}</h3>
            <p className="text-sm text-muted">{t('import.whereHelp')}</p>
            {found?.found.map((f) => (
              <button
                type="button"
                key={f}
                onClick={() => pick(f)}
                className={`flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-start text-sm ${
                  path === f ? 'border-accent bg-accent/10' : 'border-line hover:border-faint'
                }`}
              >
                <FolderOpen className="size-4 shrink-0 text-accent" />
                <span className="min-w-0 flex-1">
                  <span className="block text-xs text-muted">{t('import.foundHere')}</span>
                  <span className="block truncate font-mono text-xs" dir="ltr">
                    {f}
                  </span>
                </span>
              </button>
            ))}
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" icon={<FolderOpen className="size-4" />} onClick={() => setPicking(!picking)}>
                {t('import.pickFolder')}
              </Button>
              <Button variant="secondary" icon={<Upload className="size-4" />} disabled={upload.isPending} onClick={() => fileInput.current?.click()}>
                {upload.isPending ? t('import.uploading') : t('import.uploadDatabase')}
              </Button>
              <input
                ref={fileInput}
                type="file"
                className="hidden"
                accept=".db"
                onChange={(e) => {
                  const f = e.target.files?.[0]
                  if (f) upload.mutate(f)
                  e.target.value = ''
                }}
              />
            </div>
            {picking && <FolderPicker picked={path ? [path] : []} onPick={pick} />}
            {upload.error && <p className="text-sm text-danger">{upload.error.message}</p>}
            {check.error && <p className="text-sm text-danger">{check.error.message}</p>}
            {check.isPending && <p className="text-sm text-muted">{t('import.reading')}</p>}
            {preview && (
              <p className="truncate text-xs text-faint" dir="ltr">
                {path === '' ? t('import.uploaded') : preview.database}
              </p>
            )}
          </div>

          {preview && (
            <>
              <Files preview={preview} changes={changes} onChange={recheck} busy={check.isPending} />

              {/* Whose history goes to whom */}
              {preview.accounts.length > 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-medium">{t('import.people')}</h3>
                  <p className="text-sm text-muted">{t('import.peopleHelp')}</p>
                  {preview.accounts.map((a) => (
                    <div key={a.id} className="flex flex-col gap-2 sm:flex-row sm:items-center">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">{a.name}</div>
                        <div className="text-xs text-muted">{t('import.history', { watched: a.watched, inProgress: a.inProgress, found: a.found })}</div>
                      </div>
                      <select
                        className={`${inputClass} sm:w-64`}
                        value={accounts[a.id] ?? 0}
                        aria-label={t('import.whoIs', { name: a.name })}
                        onChange={(e) => setAccounts({ ...accounts, [a.id]: Number(e.target.value) })}
                      >
                        <option value={0}>{t('import.skip')}</option>
                        {preview.viewers.map((v) => (
                          <option key={v.id} value={v.id}>
                            {v.profileOf ? t('import.profileOf', { name: v.name, account: v.profileOf }) : v.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  ))}
                </div>
              )}

              {/* What to bring */}
              <div className="space-y-2">
                <h3 className="text-sm font-medium">{t('import.what')}</h3>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" className="accent-accent" checked={what.watched} onChange={(e) => setWhat({ ...what, watched: e.target.checked })} />
                  {t('import.watched')}
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="accent-accent"
                    checked={what.collections}
                    onChange={(e) => setWhat({ ...what, collections: e.target.checked })}
                  />
                  {t('import.collections', { number: preview.collections })}
                </label>
                <label className={`flex items-center gap-2 text-sm ${preview.canPosters ? '' : 'text-faint'}`}>
                  <input
                    type="checkbox"
                    className="accent-accent"
                    disabled={!preview.canPosters}
                    checked={what.posters && preview.canPosters}
                    onChange={(e) => setWhat({ ...what, posters: e.target.checked })}
                  />
                  {preview.canPosters ? t('import.posters', { number: preview.posters }) : t('import.noPosters')}
                </label>
              </div>

              <div className="flex flex-wrap items-center justify-end gap-3">
                {run.error && <p className="text-sm text-danger">{run.error.message}</p>}
                <Button variant="primary" disabled={!canRun} onClick={() => run.mutate()}>
                  {t('import.run')}
                </Button>
              </div>
            </>
          )}

          {status?.running && (
            <div className="space-y-2">
              <p className="text-sm text-muted">{t('import.running', { done: status.done, total: status.total })}</p>
              <div className="h-1.5 overflow-hidden rounded-full bg-text/10">
                <div className="h-full bg-accent transition-all" style={{ width: `${status.total ? (status.done / status.total) * 100 : 5}%` }} />
              </div>
            </div>
          )}
          {!status?.running && status?.error && <p className="text-sm text-danger">{t('import.stopped')}</p>}
          {!status?.running && status?.result && (
            <div className="flex gap-3 rounded-lg bg-accent/10 p-3 text-sm">
              <Check className="mt-0.5 size-4 shrink-0 text-accent" />
              <div>
                <div className="font-medium">{t('import.done')}</div>
                <div className="text-muted">
                  {t('import.summary', {
                    watched: status.result.watched,
                    inProgress: status.result.inProgress,
                    collections: status.result.collections,
                    posters: status.result.posters,
                  })}
                </div>
                {status.result.kept > 0 && <div className="text-muted">{t('import.kept', { number: status.result.kept })}</div>}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

// Paths, not words, so they aren't translated.
const examplePaths = { from: '/data', to: '/mnt/media' }

// How many files match, examples of those that don't, and the path
// changes that match them.
function Files({ preview, changes, onChange, busy }: { preview: ImportPreview; changes: PathChange[]; onChange: (c: PathChange[]) => void; busy: boolean }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<PathChange[]>(changes)
  const [lastChanges, setLastChanges] = useState(changes)
  // Follow changes made elsewhere (a suggestion taken).
  if (lastChanges !== changes) {
    setLastChanges(changes)
    setDraft(changes)
  }
  const all = preview.matched === preview.files
  return (
    <div className="space-y-3">
      <h3 className="text-sm font-medium">{t('import.files')}</h3>
      <p className={`text-sm ${all ? 'text-text' : 'text-muted'}`}>{t('import.matched', { matched: preview.matched, files: preview.files })}</p>
      {preview.suggested.map((s) => (
        <div key={s.from + s.to} className="flex flex-col gap-2 rounded-lg border border-accent/40 p-3 text-sm sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <div>{t('import.suggestion', { number: s.files ?? 0 })}</div>
            <div className="truncate font-mono text-xs text-muted" dir="ltr">
              {s.from} → {s.to}
            </div>
          </div>
          <Button variant="primary" className="h-8 px-3 text-xs" disabled={busy} onClick={() => onChange([...changes, { from: s.from, to: s.to }])}>
            {t('import.useChange')}
          </Button>
        </div>
      ))}
      {!all && preview.unmatched.length > 0 && (
        <details className="text-sm text-muted">
          <summary className="cursor-pointer">{t('import.notFound')}</summary>
          <ul className="mt-2 space-y-1">
            {preview.unmatched.map((u) => (
              <li key={u} className="truncate font-mono text-xs" dir="ltr">
                {u}
              </li>
            ))}
          </ul>
        </details>
      )}
      {draft.map((c, i) => (
        <div key={i} className="flex items-center gap-2">
          <input
            className={`${inputClass} font-mono text-xs`}
            dir="ltr"
            value={c.from}
            placeholder={examplePaths.from}
            aria-label={t('import.from')}
            onChange={(e) => setDraft(draft.map((d, j) => (j === i ? { ...d, from: e.target.value } : d)))}
          />
          <ArrowRight className="size-4 shrink-0 text-faint rtl:-scale-x-100" />
          <input
            className={`${inputClass} font-mono text-xs`}
            dir="ltr"
            value={c.to}
            placeholder={examplePaths.to}
            aria-label={t('import.to')}
            onChange={(e) => setDraft(draft.map((d, j) => (j === i ? { ...d, to: e.target.value } : d)))}
          />
          <button type="button" aria-label={t('filters.remove', { label: c.from })} onClick={() => setDraft(draft.filter((_, j) => j !== i))}>
            <X className="size-4 text-muted hover:text-text" />
          </button>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        <Button variant="ghost" icon={<Plus className="size-4" />} onClick={() => setDraft([...draft, { from: '', to: '' }])}>
          {t('import.addChange')}
        </Button>
        {JSON.stringify(draft) !== JSON.stringify(changes) && (
          <Button variant="secondary" disabled={busy} onClick={() => onChange(draft.filter((d) => d.from.trim()))}>
            {t('import.checkAgain')}
          </Button>
        )}
      </div>
    </div>
  )
}
