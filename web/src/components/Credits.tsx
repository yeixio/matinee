import { Info, Trophy, X } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { personImageUrl, type Award, type Credit, type Details, type ItemDetail } from '../api'
import { hue } from '../format'
import { castLine, crewLine, detailFacts, roleLabel } from '../people'
import { Row } from './Row'
import { Button } from './ui'

export const personLink = (id: number) => `/person/${id}`

// A round photo, or the person's initials on a tint when there's none.
export function PersonPhoto({ id, name, hasPhoto, className = '' }: { id: number; name: string; hasPhoto: boolean; className?: string }) {
  const [failed, setFailed] = useState(false)
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .map((w) => w[0])
    .slice(0, 2)
    .join('')
  const h = hue(name)
  return (
    <div
      className={`relative grid place-items-center overflow-hidden rounded-full ring-1 ring-text/10 ${className}`}
      style={{ background: `linear-gradient(140deg, hsl(${h} 30% 28%), hsl(${(h + 40) % 360} 25% 14%))` }}
    >
      <span className="text-lg font-semibold text-white/80">{initials}</span>
      {hasPhoto && !failed && (
        <img
          src={personImageUrl(id)}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
          className="absolute inset-0 size-full object-cover"
        />
      )}
    </div>
  )
}

function PersonCard({ c }: { c: Credit }) {
  return (
    <Link to={personLink(c.personId)} className="group block w-28 shrink-0 snap-start text-center md:w-32">
      <PersonPhoto
        id={c.personId}
        name={c.name}
        hasPhoto={c.hasPhoto}
        className="mx-auto aspect-square w-24 transition group-hover:ring-2 group-hover:ring-accent/70 md:w-28"
      />
      <div className="mt-2 line-clamp-2 text-sm leading-tight font-medium group-hover:text-accent-strong">{c.name}</div>
      {c.role && <div className="mt-0.5 line-clamp-2 text-xs leading-tight text-muted">{roleLabel(c)}</div>}
    </Link>
  )
}

// The cast row under the header, and an episode's guest stars.
export function CastRows({ item }: { item: ItemDetail }) {
  const credits = item.credits
  if (!credits) return null
  return (
    <>
      {credits.cast.length > 0 && (
        <Row title={item.kind === 'episode' ? 'Series cast' : 'Cast'}>
          {credits.cast.map((c) => (
            <PersonCard key={c.personId} c={c} />
          ))}
        </Row>
      )}
      {!!credits.guests?.length && (
        <Row title="Guest stars">
          {credits.guests.map((c) => (
            <PersonCard key={c.personId} c={c} />
          ))}
        </Row>
      )}
    </>
  )
}

// "Directed by …", "Starring …" and the award line, for the main pane.
export function KeyPeople({ item }: { item: ItemDetail }) {
  const crew = item.credits ? crewLine(item.credits.crew) : []
  const stars = item.credits && item.kind !== 'episode' ? castLine(item.credits.cast) : []
  const awards = item.details?.awardSummary
  if (crew.length === 0 && stars.length === 0 && !awards) return null
  return (
    <div className="space-y-1.5 text-sm">
      {crew.map((line) => (
        <div key={line.label} className="flex gap-2">
          <span className="shrink-0 text-muted">{line.label}</span>
          <span className="min-w-0">
            <People list={line.people} />
          </span>
        </div>
      ))}
      {stars.length > 0 && (
        <div className="flex gap-2">
          <span className="shrink-0 text-muted">Starring</span>
          <span className="min-w-0">
            <People list={stars} />
          </span>
        </div>
      )}
      {awards && (
        <div className="flex items-center gap-2 text-text/90">
          <Trophy className="size-4 shrink-0 text-accent" />
          {awards}
        </div>
      )}
    </div>
  )
}

function People({ list }: { list: { id: number; name: string }[] }) {
  return (
    <>
      {list.map((p, i) => (
        <span key={p.id}>
          {i > 0 && ', '}
          <Link to={personLink(p.id)} className="hover:text-accent-strong hover:underline">
            {p.name}
          </Link>
        </span>
      ))}
    </>
  )
}

export function MoreDetailsButton({ item }: { item: ItemDetail }) {
  const [open, setOpen] = useState(false)
  if (!item.details && !item.credits?.crew.length) return null
  return (
    <>
      <Button variant="ghost" icon={<Info className="size-4" />} onClick={() => setOpen(true)}>
        More details
      </Button>
      {open && <MoreDetails item={item} onClose={() => setOpen(false)} />}
    </>
  )
}

function MoreDetails({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const d = item.details ?? {}
  const crew = item.credits?.crew ?? []

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label="More details"
        onClick={(e) => e.stopPropagation()}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-t-2xl border border-line/60 bg-surface p-5 shadow-2xl sm:rounded-2xl md:p-6"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold">{item.title}</h2>
            {d.tagline && <p className="text-sm text-muted italic">{d.tagline}</p>}
          </div>
          <button onClick={onClose} aria-label="Close" className="text-muted hover:text-text">
            <X className="size-5" />
          </button>
        </div>
        <div className="space-y-6">
          <Facts d={d} kind={item.kind} />
          {crew.length > 0 && (
            <Section title="Crew">
              <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                {crew.map((c, i) => (
                  <div key={`${c.personId}-${c.role}-${i}`} className="flex min-w-0 gap-2">
                    <dt className="w-36 shrink-0 text-muted">{c.role}</dt>
                    <dd className="min-w-0 truncate">
                      <Link to={personLink(c.personId)} onClick={onClose} className="hover:text-accent-strong hover:underline">
                        {c.name}
                      </Link>
                    </dd>
                  </div>
                ))}
              </dl>
            </Section>
          )}
          {!!d.awards?.length && <Awards awards={d.awards} />}
          {d.imdbId && (
            <p className="text-xs text-faint">
              IMDb {d.imdbId}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-xs font-semibold tracking-wider text-faint uppercase">{title}</h3>
      {children}
    </section>
  )
}

function Facts({ d, kind }: { d: Details; kind: string }) {
  const rows = detailFacts(d, kind)
  if (rows.length === 0) return null
  return (
    <Section title="About">
      <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
        {rows.map(([label, value]) => (
          <div key={label} className="flex min-w-0 gap-2">
            <dt className="w-36 shrink-0 text-muted">{label}</dt>
            <dd className="min-w-0">{value}</dd>
          </div>
        ))}
      </dl>
    </Section>
  )
}

function Awards({ awards }: { awards: Award[] }) {
  const [all, setAll] = useState(false)
  const shown = all ? awards : awards.slice(0, 12)
  return (
    <Section title="Awards">
      <ul className="space-y-1.5 text-sm">
        {shown.map((a, i) => (
          <li key={i} className="flex gap-2">
            <Trophy className={`mt-0.5 size-4 shrink-0 ${a.won ? 'text-accent' : 'text-faint'}`} />
            <span className="min-w-0">
              <span className={a.won ? '' : 'text-text/80'}>{a.name}</span>
              <span className="text-muted">
                {' · '}
                {a.won ? 'Won' : 'Nominated'}
                {a.year ? ` ${a.year}` : ''}
                {a.names?.length ? ` · ${a.names.join(', ')}` : ''}
              </span>
            </span>
          </li>
        ))}
      </ul>
      {awards.length > shown.length && (
        <button onClick={() => setAll(true)} className="mt-2 text-sm text-accent hover:text-accent-strong">
          Show all {awards.length}
        </button>
      )}
      <p className="mt-3 text-xs text-faint">Awards from Wikidata.</p>
    </Section>
  )
}
