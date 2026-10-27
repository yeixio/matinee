// Labels for cast, crew and details, shared by the details and person pages.
// The phone and TV apps keep a copy in src/people.ts.
import type { Credit, Details } from './api'
import i18n, { formatLocale } from './i18n'

// "Walter White · 62 episodes", or the job for crew.
export function roleLabel(c: Credit): string {
  const role = jobName(c.role ?? '')
  if (!c.episodes) return role
  const eps = i18n.t('common:people.episodes', { count: c.episodes })
  return role ? `${role} · ${eps}` : eps
}

type Person = { id: number; name: string }
export type CrewLine = { label: string; people: Person[] }

// A crew member's job in the page's language, such as Regie for Director.
// Characters, and jobs the catalog doesn't know, stay as they are.
export function jobName(job: string): string {
  return job && i18n.exists(`common:jobs.${job}`) ? i18n.t(`common:jobs.${job}`) : job
}

// The jobs named on the main pane, and how the line reads (keys in
// common:people.lines, with _one and _other for the ones that count).
const keyJobs: { key: string; counted?: boolean; jobs: string[] }[] = [
  { key: 'createdBy', jobs: ['Creator'] },
  { key: 'director', counted: true, jobs: ['Director'] },
  { key: 'writer', counted: true, jobs: ['Screenplay', 'Writer', 'Teleplay'] },
  { key: 'basedOn', jobs: ['Novel', 'Author'] },
  { key: 'music', jobs: ['Original Music Composer', 'Music'] },
]

// "Director: …", "Writers: …" and so on, each person once per line. A show's
// directors change by the episode, so its creators stand in for them.
export function crewLine(crew: Credit[], max = 3): CrewLine[] {
  const out: CrewLine[] = []
  const created = crew.some((c) => c.role === 'Creator')
  for (const k of keyJobs) {
    if (created && k.key === 'director') continue
    const people: Person[] = []
    for (const c of crew) {
      if (c.role && k.jobs.includes(c.role) && !people.some((p) => p.id === c.personId)) {
        people.push({ id: c.personId, name: c.name })
      }
    }
    if (people.length === 0) continue
    const label = k.counted ? i18n.t(`common:people.lines.${k.key}`, { count: people.length }) : i18n.t(`common:people.lines.${k.key}`)
    out.push({ label, people: people.slice(0, max) })
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
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(formatLocale(), {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  })
}

// "$187.4 million" ("187,4 Millionen $" in German); "" for unknown.
export function money(usd?: number): string {
  if (!usd) return ''
  const big = usd >= 1e6
  const amount = new Intl.NumberFormat(formatLocale(), {
    notation: big ? 'compact' : 'standard',
    compactDisplay: 'long',
    maximumFractionDigits: big ? 1 : 0,
  }).format(usd)
  return i18n.t('common:people.dollars', { amount })
}

// "Born 25 April 1940 in New York City", "1940 – 2020" and the like.
export function lifeLabel(p: { birthday?: string; deathday?: string; birthplace?: string }): string {
  const born = dateLabel(p.birthday)
  if (p.deathday) {
    const by = p.birthday?.slice(0, 4)
    const dy = p.deathday.slice(0, 4)
    return by ? `${by} – ${dy}` : i18n.t('common:people.died', { date: dateLabel(p.deathday) })
  }
  if (!born) return p.birthplace ? i18n.t('common:people.bornIn', { place: p.birthplace }) : ''
  return p.birthplace ? i18n.t('common:people.bornOnIn', { date: born, place: p.birthplace }) : i18n.t('common:people.bornOn', { date: born })
}

// The rows of a title's About section, in reading order, skipping unknowns.
export function detailFacts(d: Details, kind: string): [string, string][] {
  const show = kind === 'show'
  const rows: [string, string][] = []
  const f = (key: string, count?: number) => i18n.t(`common:facts.${key}`, count === undefined ? {} : { count })
  const add = (label: string, value?: string) => value && rows.push([label, value])
  const list = (key: string, values?: string[]) => values?.length && add(f(key, values.length), values.join(', '))
  add(f(show ? 'firstAired' : kind === 'movie' ? 'released' : 'aired'), dateLabel(d.releaseDate))
  add(f('ended'), dateLabel(d.endDate))
  if (show && d.status !== 'Ended') add(f('status'), d.status && statusName(d.status))
  if (show && d.seasons) add(f('seasons'), i18n.t('common:facts.seasonsValue', { seasons: d.seasons, count: d.episodes ?? 0 }))
  add(f('rated'), d.certification)
  add(f('genres'), d.genres?.map(genreName).join(', '))
  add(f('originalTitle'), d.originalTitle)
  add(f('originalLanguage'), d.originalLanguage)
  if (d.languages && d.languages.length > 1) add(f('spokenLanguages'), d.languages.join(', '))
  list('studio', d.studios)
  list('network', d.networks)
  list('country', d.countries)
  add(f('collection'), d.collection?.name)
  add(f('budget'), money(d.budget))
  add(f('boxOffice'), money(d.revenue))
  return rows
}

// A genre in the page's language, such as Komödie for Comedy. Genres are
// kept in English, as the online match names them.
export function genreName(genre: string): string {
  return i18n.exists(`common:genres.${genre}`) ? i18n.t(`common:genres.${genre}`) : genre
}

// A show's status, such as Returning Series, in the page's language.
function statusName(status: string): string {
  return i18n.exists(`common:showStatus.${status}`) ? i18n.t(`common:showStatus.${status}`) : status
}

// A genre row's title on Home, such as "Comedies", in the page's language.
export function genreRowTitle(genre: string): string {
  return i18n.exists(`common:genreRows.${genre}`) ? i18n.t(`common:genreRows.${genre}`) : genreName(genre)
}

// What a person is known for, such as Actor or Directing, from TMDB's department.
export function knownForLabel(department: string): string {
  return i18n.exists(`common:departments.${department}`) ? i18n.t(`common:departments.${department}`) : department
}
