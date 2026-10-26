// Labels for cast, crew and details, shared by the details and person pages.
// The phone and TV apps keep a copy in src/people.ts.
import type { Credit } from './api'

// "Walter White · 62 episodes", or the job for crew.
export function roleLabel(c: Credit): string {
  const role = c.role ?? ''
  if (!c.episodes) return role
  const eps = c.episodes === 1 ? '1 episode' : `${c.episodes} episodes`
  return role ? `${role} · ${eps}` : eps
}

type Person = { id: number; name: string }
export type CrewLine = { label: string; people: Person[] }

// The jobs named on the main pane, and how the line reads.
const keyJobs: { label: string; plural?: string; jobs: string[] }[] = [
  { label: 'Created by', jobs: ['Creator'] },
  { label: 'Director', plural: 'Directors', jobs: ['Director'] },
  { label: 'Writer', plural: 'Writers', jobs: ['Screenplay', 'Writer', 'Teleplay'] },
  { label: 'Based on', jobs: ['Novel', 'Author'] },
  { label: 'Music', jobs: ['Original Music Composer', 'Music'] },
]

// "Director: …", "Writers: …" and so on, each person once per line. A show's
// directors change by the episode, so its creators stand in for them.
export function crewLine(crew: Credit[], max = 3): CrewLine[] {
  const out: CrewLine[] = []
  const created = crew.some((c) => c.role === 'Creator')
  for (const k of keyJobs) {
    if (created && k.label === 'Director') continue
    const people: Person[] = []
    for (const c of crew) {
      if (c.role && k.jobs.includes(c.role) && !people.some((p) => p.id === c.personId)) {
        people.push({ id: c.personId, name: c.name })
      }
    }
    if (people.length === 0) continue
    out.push({ label: people.length > 1 && k.plural ? k.plural : k.label, people: people.slice(0, max) })
  }
  return out
}

// The leads, for "Starring …".
export function castLine(cast: Credit[], max = 3): Person[] {
  return cast.slice(0, max).map((c) => ({ id: c.personId, name: c.name }))
}

// "15 December 1995" from YYYY-MM-DD; "" when unknown.
export function dateLabel(date?: string): string {
  if (!date) return ''
  const [y, m, d] = date.split('-').map(Number)
  if (!y) return ''
  if (!m || !d) return String(y)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  })
}

// "$187.4 million"; "" for unknown.
export function money(usd?: number): string {
  if (!usd) return ''
  if (usd >= 1e9) return `$${trim(usd / 1e9)} billion`
  if (usd >= 1e6) return `$${trim(usd / 1e6)} million`
  return `$${Math.round(usd).toLocaleString('en-US')}`
}

const trim = (n: number) => (Math.round(n * 10) / 10).toString()

// "Born 25 April 1940 in New York City", "1940 – 2020" and the like.
export function lifeLabel(p: { birthday?: string; deathday?: string; birthplace?: string }): string {
  const born = dateLabel(p.birthday)
  if (p.deathday) {
    const by = p.birthday?.slice(0, 4)
    const dy = p.deathday.slice(0, 4)
    return by ? `${by} – ${dy}` : `Died ${dateLabel(p.deathday)}`
  }
  if (!born) return p.birthplace ? `Born in ${p.birthplace}` : ''
  return p.birthplace ? `Born ${born} in ${p.birthplace}` : `Born ${born}`
}
