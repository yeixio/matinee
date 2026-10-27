import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Clock, RefreshCw, Search as SearchIcon, Trash2, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Issue, type Title, type TitleKind, type TitleRequest, type User } from '../api'
import { issueKindLabel, RemotePoster, useRequestOptions } from '../components/Arr'
import { Button, EmptyState, Spinner } from '../components/ui'
import { episodeCode } from '../format'
import i18n, { formatLocale } from '../i18n'

type Tab = 'ask' | 'requests' | 'problems'

// Asking for movies and shows, the requests made, and problems reported.
// Admins see everyone's and act on them here.
export default function Requests() {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({
    queryKey: ['me'],
    queryFn: api.me,
    staleTime: Infinity,
  })
  const { data: opts } = useRequestOptions()
  const [params, setParams] = useSearchParams()
  if (!opts || !me) {
    return (
      <div className="grid h-[50vh] place-items-center">
        <Spinner />
      </div>
    )
  }
  const tabs: { id: Tab; label: string; count?: number }[] = [
    ...(opts.canRequest ? [{ id: 'ask' as Tab, label: t('requests.tab.ask') }] : []),
    {
      id: 'requests',
      label: me.isAdmin ? t('requests.tab.requests') : t('requests.tab.yourRequests'),
      count: opts.pendingRequests,
    },
    {
      id: 'problems',
      label: me.isAdmin ? t('requests.tab.problems') : t('requests.tab.yourReports'),
      count: opts.openIssues,
    },
  ]
  const tab = tabs.find((x) => x.id === params.get('tab'))?.id ?? tabs[0].id

  return (
    <div className="max-w-5xl space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">{t('nav.requests')}</h1>
      <nav className="flex gap-1 overflow-x-auto border-b border-line/60">
        {tabs.map((x) => (
          <button
            key={x.id}
            onClick={() =>
              setParams(x.id === tabs[0].id ? {} : { tab: x.id }, {
                replace: true,
              })
            }
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors ${
              tab === x.id ? 'border-accent text-text' : 'border-transparent text-muted hover:text-text'
            }`}
          >
            {x.label}
            {!!x.count && <span className="rounded-full bg-accent px-1.5 text-xs font-semibold text-on-accent">{x.count}</span>}
          </button>
        ))}
      </nav>
      {tab === 'ask' && <Ask />}
      {tab === 'requests' && <RequestList admin={me.isAdmin} />}
      {tab === 'problems' && <Problems admin={me.isAdmin} />}
    </div>
  )
}

function Ask() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: opts } = useRequestOptions()
  const [params, setParams] = useSearchParams()
  const kinds = (['movie', 'show'] as TitleKind[]).filter((k) => (k === 'movie' ? opts?.movies : opts?.shows))
  const [kind, setKind] = useState<TitleKind>(
    kinds.includes(params.get('kind') as TitleKind) ? (params.get('kind') as TitleKind) : kinds[0],
  )
  const [text, setText] = useState(params.get('q') ?? '')
  const [q, setQ] = useState(text.trim())
  const [profile, setProfile] = useState<number>()

  // Searching goes out to the internet through Radarr or Sonarr, so wait for a pause in typing.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQ(text.trim())
      setParams(text.trim() ? { q: text.trim(), kind } : {}, { replace: true })
    }, 400)
    return () => window.clearTimeout(timer)
  }, [text, kind, setParams])

  const { data, isFetching, error } = useQuery({
    queryKey: ['titleSearch', kind, q],
    queryFn: () => api.searchTitles(kind, q),
    enabled: q.length > 1,
    placeholderData: (prev) => prev,
    staleTime: 60_000,
  })
  const ask = useMutation({
    mutationFn: (title: Title) => api.createRequest(title, profile),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['titleSearch'] })
      qc.invalidateQueries({ queryKey: ['requests'] })
      qc.invalidateQueries({ queryKey: ['requestOptions'] })
    },
  })
  const profiles = opts?.qualityProfiles?.[kind] ?? []
  const defaultProfile = opts?.defaultQualityProfile?.[kind]

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        {kinds.length > 1 && (
          <div className="flex shrink-0 rounded-lg border border-line p-0.5">
            {kinds.map((k) => (
              <button
                key={k}
                onClick={() => {
                  setKind(k)
                  setProfile(undefined)
                }}
                className={`rounded-md px-3 py-1.5 text-sm ${kind === k ? 'bg-text font-medium text-ink' : 'text-muted hover:text-text'}`}
              >
                {k === 'movie' ? t('requests.movies') : t('requests.shows')}
              </button>
            ))}
          </div>
        )}
        <div className="relative flex-1">
          <SearchIcon className="absolute top-1/2 start-3 size-4 -translate-y-1/2 text-faint" />
          <input
            autoFocus
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={kind === 'movie' ? t('requests.findMovie') : t('requests.findShow')}
            className="h-11 w-full rounded-lg border border-line bg-surface ps-10 pe-4 text-sm placeholder:text-faint focus:border-accent focus:outline-none"
          />
          {isFetching && <div className="absolute top-1/2 end-3 size-2 -translate-y-1/2 animate-pulse rounded-full bg-accent" />}
        </div>
        {profiles.length > 1 && (
          <label className="flex shrink-0 items-center gap-2 text-sm text-muted">
            {t('quality.label')}
            <select
              className="h-11 rounded-lg border border-line bg-surface px-2 text-sm text-text focus:border-accent focus:outline-none"
              value={profile ?? defaultProfile ?? ''}
              onChange={(e) => setProfile(Number(e.target.value))}
            >
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      {error && <p className="text-sm text-danger">{error.message}</p>}
      {ask.error && <p className="text-sm text-danger">{ask.error.message}</p>}
      {q.length > 1 && data && data.length === 0 && <EmptyState title={t('requests.nothingFound', { q })} />}
      <div className="grid gap-x-4 gap-y-6" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))' }}>
        {data?.map((title) => (
          <div key={`${title.tmdbId}-${title.tvdbId}`} className="animate-fade">
            <RemotePoster src={title.poster} title={title.title} className="rounded-xl ring-1 ring-text/5" />
            <div className="mt-2 px-0.5">
              <div className="truncate text-sm font-medium" title={title.title}>
                {title.title}
              </div>
              <div className="text-xs text-muted">{title.year || ''}</div>
              <div className="mt-2">
                {title.status === 'available' ? (
                  <Link to={`/item/${title.itemId}`} className="flex h-9 items-center gap-1.5 text-sm text-accent">
                    <Check className="size-4" /> {t('requests.ready')}
                  </Link>
                ) : title.status ? (
                  <span className="flex h-9 items-center gap-1.5 text-sm text-muted">
                    <Clock className="size-4" /> {title.status === 'requested' ? t('requests.askedFor') : t('requests.onItsWay')}
                  </span>
                ) : (
                  <Button
                    variant="secondary"
                    className="h-9 w-full"
                    disabled={ask.isPending && ask.variables === title}
                    onClick={() => ask.mutate(title)}
                  >
                    {opts?.autoApprove ? t('requests.add') : t('requests.askForIt')}
                  </Button>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

const stateLabel = (state: TitleRequest['state']) => i18n.t(`requests.state.${state}`)

function ago(unix: number) {
  return new Date(unix * 1000).toLocaleDateString(formatLocale(), {
    day: 'numeric',
    month: 'short',
  })
}

function RequestList({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending } = useQuery({
    queryKey: ['requests', admin],
    queryFn: () => api.requests(admin),
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['requests'] })
    qc.invalidateQueries({ queryKey: ['requestOptions'] })
    qc.invalidateQueries({ queryKey: ['upcoming'] })
  }
  const approve = useMutation({
    mutationFn: (id: string) => api.approveRequest(id),
    onSuccess: refresh,
  })
  const decline = useMutation({
    mutationFn: api.declineRequest,
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: api.deleteRequest,
    onSuccess: refresh,
  })
  const busy = approve.isPending || decline.isPending || remove.isPending

  if (isPending) return <Spinner />
  if (!data?.length) return <p className="text-muted">{t('requests.none')}</p>
  return (
    <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
      {(approve.error || decline.error || remove.error) && (
        <p className="p-4 text-sm text-danger">{(approve.error ?? decline.error ?? remove.error)!.message}</p>
      )}
      {data.map((r) => (
        <div key={r.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 p-3">
          <RemotePoster src={r.poster} title={r.title} className="w-12 shrink-0 rounded-md" label={false} />
          <div className="min-w-0 flex-1 basis-40">
            <div className="truncate font-medium">
              {r.itemId ? (
                <Link to={`/item/${r.itemId}`} className="hover:text-accent-strong">
                  {r.title}
                </Link>
              ) : (
                r.title
              )}{' '}
              {r.year ? <span className="text-sm font-normal text-muted">{r.year}</span> : null}
            </div>
            <div className="truncate text-xs text-muted">
              <span className={r.state === 'failed' ? 'text-danger' : r.itemId ? 'text-accent' : ''}>
                {r.itemId ? t('requests.ready') : stateLabel(r.state)}
              </span>
              {admin && ` · ${r.username}`} · {ago(r.createdAt)}
            </div>
            {r.error && (
              <div className="mt-0.5 truncate text-xs text-danger" title={r.error}>
                {r.error}
              </div>
            )}
          </div>
          <div className="ms-auto flex gap-1">
            {admin && (r.state === 'pending' || r.state === 'failed') && (
              <Button
                variant="primary"
                className="h-9"
                disabled={busy}
                icon={r.state === 'failed' ? <RefreshCw className="size-4" /> : <Check className="size-4" />}
                onClick={() => approve.mutate(r.id)}
              >
                {r.state === 'failed' ? t('requests.retry') : t('requests.approve')}
              </Button>
            )}
            {admin && r.state === 'pending' && (
              <Button variant="ghost" className="h-9" disabled={busy} icon={<X className="size-4" />} onClick={() => decline.mutate(r.id)}>
                {t('link.decline')}
              </Button>
            )}
            {r.state !== 'pending' || !admin ? (
              <Button
                variant="ghost"
                className="h-9"
                disabled={busy}
                icon={<Trash2 className="size-4" />}
                aria-label={t('requests.remove', { title: r.title })}
                onClick={() => remove.mutate(r.id)}
              />
            ) : null}
          </div>
        </div>
      ))}
    </div>
  )
}

function issueTitle(x: Issue) {
  const it = x.item
  if (!it) return i18n.t('requests.removedFromLibrary')
  if (it.kind === 'episode' && it.showTitle) return `${it.showTitle} · ${episodeCode(it.seasonIndex, it.index)}`
  return it.title
}

function Problems({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending } = useQuery({
    queryKey: ['issues', admin],
    queryFn: () => api.issues(admin),
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['issues'] })
    qc.invalidateQueries({ queryKey: ['requestOptions'] })
  }
  const resolve = useMutation({
    mutationFn: api.resolveIssue,
    onSuccess: refresh,
  })
  const replace = useMutation({
    mutationFn: api.replaceIssueFile,
    onSuccess: refresh,
  })
  const busy = resolve.isPending || replace.isPending

  if (isPending) return <Spinner />
  if (!data?.length) {
    return (
      <p className="text-muted">
        {admin ? t('requests.noProblems') : t('requests.noReports')}
      </p>
    )
  }
  return (
    <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
      {(resolve.error || replace.error) && <p className="p-4 text-sm text-danger">{(resolve.error ?? replace.error)!.message}</p>}
      {data.map((x) => (
        <div key={x.id} className={`flex flex-wrap items-center gap-x-4 gap-y-2 p-4 ${x.resolved ? 'opacity-60' : ''}`}>
          <div className="min-w-0 flex-1 basis-60">
            <div className="truncate font-medium">
              {x.item ? (
                <Link to={`/item/${x.item.id}`} className="hover:text-accent-strong">
                  {issueTitle(x)}
                </Link>
              ) : (
                issueTitle(x)
              )}
            </div>
            <div className="text-xs text-muted">
              {issueKindLabel(x.kind)}
              {admin && ` · ${x.username}`} · {ago(x.createdAt)}
              {x.resolved && ` · ${x.resolution === 'replaced' ? t('requests.fileReplaced') : t('requests.closed')}`}
            </div>
            {x.note && <p className="mt-1 text-sm text-text/85">{x.note}</p>}
          </div>
          {admin && !x.resolved && (
            <div className="flex gap-2">
              {x.replaceable && (
                <Button
                  disabled={busy}
                  icon={<RefreshCw className="size-4" />}
                  onClick={() =>
                    confirm(t('requests.confirmReplace', { title: issueTitle(x) })) && replace.mutate(x.id)
                  }
                >
                  {t('requests.replaceFile')}
                </Button>
              )}
              <Button variant="ghost" disabled={busy} onClick={() => resolve.mutate(x.id)}>
                {t('requests.close')}
              </Button>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}
