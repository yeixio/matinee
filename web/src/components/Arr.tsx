import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Flag, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api, type IssueKind, type Item, type Upcoming, type User } from '../api'
import { hue } from '../format'
import { Row, RowItem } from './Row'
import { Button, Field, inputClass } from './ui'

// What this account can do with Radarr and Sonarr: ask for titles, report
// problems, and for admins, what's waiting for them.
// enabled is false for kids profiles, which can't make requests.
export function useRequestOptions(enabled = true) {
  return useQuery({
    queryKey: ['requestOptions'],
    queryFn: api.requestOptions,
    staleTime: 60_000,
    enabled,
  })
}

// A poster from the internet (TMDB or TVDB, via Radarr and Sonarr), with the
// same tinted title card as library artwork when there's none. Small ones
// leave the title off the card (label), since it's beside them anyway.
export function RemotePoster({
  src,
  title,
  className = '',
  label = true,
}: {
  src?: string
  title: string
  className?: string
  label?: boolean
}) {
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)
  const h = hue(title)
  return (
    <div
      className={`relative aspect-[2/3] overflow-hidden bg-raised ${className}`}
      style={{
        backgroundImage: `linear-gradient(140deg, hsl(${h} 35% 22%), hsl(${(h + 40) % 360} 30% 10%))`,
      }}
    >
      {(!src || failed || !loaded) && label && (
        <div className="absolute inset-0 flex items-end p-3">
          <span className="line-clamp-3 text-sm leading-snug font-semibold text-white/85">{title}</span>
        </div>
      )}
      {src && !failed && (
        <img
          src={src}
          alt=""
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          onLoad={() => setLoaded(true)}
          onError={() => setFailed(true)}
          className={`absolute inset-0 size-full object-cover transition-opacity duration-300 ${loaded ? 'opacity-100' : 'opacity-0'}`}
        />
      )}
    </div>
  )
}

// "Today", "Tomorrow", "Fri 31 Oct".
export function dayLabel(date: Date) {
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const days = Math.round((start(date) - start(new Date())) / 86_400_000)
  if (days === 0) return 'Today'
  if (days === 1) return 'Tomorrow'
  if (days === -1) return 'Yesterday'
  return date.toLocaleDateString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  })
}

export const releaseLabel: Record<Upcoming['release'], string> = {
  cinemas: 'In cinemas',
  digital: 'Digital release',
  physical: 'On disc',
  airs: 'Airs',
}

export function upcomingTitle(u: Upcoming) {
  return u.kind === 'episode' ? (u.showTitle ?? u.title) : u.title
}

export function upcomingDetail(u: Upcoming) {
  if (u.kind === 'episode') return `S${u.season} · E${u.episode}${u.title ? ` · ${u.title}` : ''}`
  return releaseLabel[u.release]
}

// A row of what's coming soon, for Home.
export function ComingSoonRow() {
  const { data: opts } = useRequestOptions()
  const { data } = useQuery({
    queryKey: ['upcoming', 30],
    queryFn: () => api.upcoming(30),
    enabled: !!opts && (opts.movies || opts.shows),
    staleTime: 5 * 60_000,
  })
  if (!data?.items.length) return null
  return (
    <Row title="Coming soon" more="/upcoming">
      {data.items.slice(0, 20).map((u, i) => (
        <RowItem key={i}>
          <Link to="/upcoming" className="group block animate-fade">
            <RemotePoster
              src={u.poster}
              title={upcomingTitle(u)}
              className="rounded-xl ring-1 ring-text/5 transition group-hover:ring-accent/70"
            />
            <div className="mt-2 px-0.5">
              <div className="truncate text-sm font-medium">{upcomingTitle(u)}</div>
              <div className="truncate text-xs text-muted">
                <span className="text-accent">{dayLabel(new Date(u.date))}</span> · {upcomingDetail(u)}
              </div>
            </div>
          </Link>
        </RowItem>
      ))}
    </Row>
  )
}

export const issueKinds: { value: IssueKind; label: string }[] = [
  { value: 'video', label: 'Picture' },
  { value: 'audio', label: 'Sound' },
  { value: 'subtitles', label: 'Subtitles' },
  { value: 'wrong', label: 'Wrong title' },
  { value: 'other', label: 'Something else' },
]

// Report a problem with an item, for an admin to fix.
export function ReportProblem({ item }: { item: Item }) {
  const qc = useQueryClient()
  const { data: opts } = useRequestOptions()
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<IssueKind>('video')
  const [note, setNote] = useState('')
  const report = useMutation({
    mutationFn: () => api.reportIssue(item.id, kind, note),
    onSuccess: () => {
      setNote('')
      qc.invalidateQueries({ queryKey: ['issues'] })
      qc.invalidateQueries({ queryKey: ['requestOptions'] })
    },
  })
  if (!opts?.canReport) return null
  if (report.isSuccess && !open) {
    return <span className="flex h-10 items-center text-sm text-muted">Thanks, an admin will take a look.</span>
  }
  if (!open) {
    return (
      <Button variant="ghost" icon={<Flag className="size-4" />} onClick={() => setOpen(true)}>
        Report a problem
      </Button>
    )
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    report.mutate(undefined, { onSuccess: () => setOpen(false) })
  }
  return (
    <form onSubmit={submit} className="w-full max-w-lg space-y-4 rounded-xl border border-line/60 bg-surface/90 p-4 backdrop-blur">
      <div className="flex items-center justify-between">
        <span className="font-medium">What's wrong with {item.title}?</span>
        <button type="button" onClick={() => setOpen(false)} aria-label="Close" className="text-muted hover:text-text">
          <X className="size-4" />
        </button>
      </div>
      <div className="flex flex-wrap gap-2">
        {issueKinds.map((k) => (
          <button
            type="button"
            key={k.value}
            onClick={() => setKind(k.value)}
            className={`rounded-full border px-3 py-1.5 text-sm transition-colors ${
              kind === k.value ? 'border-accent bg-accent/10 text-text' : 'border-line text-muted hover:border-faint'
            }`}
          >
            {k.label}
          </button>
        ))}
      </div>
      <Field label="Details (optional)">
        <textarea
          className={`${inputClass} h-20 py-2`}
          maxLength={1000}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Like: no sound after 20 minutes"
        />
      </Field>
      {report.error && <p className="text-sm text-danger">{report.error.message}</p>}
      <div className="flex justify-end">
        <Button variant="primary" disabled={report.isPending}>
          Send
        </Button>
      </div>
    </form>
  )
}

// An admin's choice of quality for a movie, or the show an item belongs to.
export function ItemQuality({ item, user }: { item: Item; user?: User }) {
  const qc = useQueryClient()
  const { data: opts } = useRequestOptions()
  const app = item.kind === 'movie' ? opts?.movies : ['show', 'season', 'episode'].includes(item.kind) ? opts?.shows : false
  const enabled = !!user?.isAdmin && !!app
  const { data } = useQuery({
    queryKey: ['quality', item.id],
    queryFn: () => api.itemQuality(item.id),
    enabled,
    retry: false, // 404 just means Radarr or Sonarr doesn't have it
  })
  const set = useMutation({
    mutationFn: (profile: number) => api.setItemQuality(item.id, profile),
    onSuccess: (q) => qc.setQueryData(['quality', item.id], q),
  })
  if (!enabled || !data) return null
  return (
    <label className="flex h-10 items-center gap-2 text-sm text-muted">
      Quality{item.kind !== 'movie' && ' for the show'}
      <select
        className="h-10 rounded-lg border border-line bg-surface px-2 text-sm text-text focus:border-accent focus:outline-none"
        value={data.qualityProfileId}
        disabled={set.isPending}
        onChange={(e) => {
          const p = data.qualityProfiles.find((x) => x.id === Number(e.target.value))
          if (p && confirm(`Switch to ${p.name}? ${data.app === 'radarr' ? 'Radarr' : 'Sonarr'} will look for copies that match.`))
            set.mutate(p.id)
        }}
      >
        {data.qualityProfiles.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
      {set.isSuccess && <span className="text-accent">Searching…</span>}
      {set.error && <span className="text-danger">{set.error.message}</span>}
    </label>
  )
}
