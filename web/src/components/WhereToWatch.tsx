import { useQuery } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type TitleKind, type WatchProvider } from '../api'
import { formatLocale } from '../i18n'

// WhereToWatch says where a title from Discover can be watched outside
// the library: the network a show is made for, and the streaming
// services, rentals and shops that have it, so someone can play it on a
// service they already pay for. It comes from a connected Box Office;
// without one, or when nothing is known, it shows nothing. The country is
// the browser's until someone picks another, kept on this device.

const countryKey = 'matinee.watchCountry'

// browserCountry is the country in the browser's languages, such as "US"
// from en-US, or "" when none says.
export function browserCountry(languages: readonly string[] = navigator.languages ?? [navigator.language]): string {
  for (const l of languages) {
    const m = /[-_]([A-Za-z]{2})(?:$|[-_])/.exec(l)
    if (m) return m[1].toUpperCase()
  }
  return ''
}

function savedCountry(): string {
  try {
    return localStorage.getItem(countryKey) ?? ''
  } catch {
    return ''
  }
}

function countryName(code: string): string {
  try {
    return new Intl.DisplayNames([formatLocale()], { type: 'region' }).of(code) ?? code
  } catch {
    return code
  }
}

function Logos({ label, providers, link, wide }: { label: string; providers?: WatchProvider[]; link?: string; wide?: boolean }) {
  if (!providers?.length) return null
  return (
    <>
      <dt className="pt-2 text-sm text-muted">{label}</dt>
      <dd>
        <ul className="flex flex-wrap gap-2">
          {providers.map((p) => {
            const face = wide ? (
              <span className="flex h-10 items-center justify-center rounded-lg bg-white px-3">
                {p.logo ? <img src={p.logo} alt={p.name} referrerPolicy="no-referrer" loading="lazy" className="max-h-6 max-w-24 object-contain" /> : <span className="text-sm font-semibold text-black">{p.name}</span>}
              </span>
            ) : p.logo ? (
              <img src={p.logo} alt={p.name} referrerPolicy="no-referrer" loading="lazy" className="size-10 rounded-lg object-cover ring-1 ring-text/10" />
            ) : (
              <span className="flex h-10 items-center rounded-lg border border-line px-2 text-xs font-medium">{p.name}</span>
            )
            return (
              <li key={p.id} title={p.name}>
                {link ? (
                  <a href={link} target="_blank" rel="noreferrer" className="block rounded-lg transition hover:ring-2 hover:ring-accent">
                    {face}
                  </a>
                ) : (
                  face
                )}
              </li>
            )
          })}
        </ul>
      </dd>
    </>
  )
}

export function WhereToWatch({ kind, tmdbId }: { kind: TitleKind; tmdbId: number }) {
  const { t } = useTranslation()
  const [country, setCountry] = useState(() => savedCountry() || browserCountry())
  const watch = useQuery({
    queryKey: ['discoverWatch', kind, tmdbId, country],
    queryFn: () => api.discoverWatch(kind, tmdbId, country),
    staleTime: 60 * 60_000,
    retry: false,
    placeholderData: (prev) => prev,
  })
  const w = watch.data
  if (!w || (w.networks.length === 0 && w.countries.length === 0)) return null
  const offers = !!(w.stream?.length || w.free?.length || w.rent?.length || w.buy?.length)
  const choices = w.countries.includes(w.country) ? w.countries : [...w.countries, w.country].sort()
  const pick = (c: string) => {
    setCountry(c)
    try {
      localStorage.setItem(countryKey, c)
    } catch {
      // Not remembered; it still applies here.
    }
  }
  return (
    <section aria-label={t('discover.watch.title')} className="space-y-2 pt-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-display text-base font-semibold">{t('discover.watch.title')}</h3>
        {w.countries.length > 0 && (
          <select
            aria-label={t('discover.watch.country')}
            value={w.country}
            onChange={(e) => pick(e.target.value)}
            className="max-w-48 rounded-md border border-line bg-surface px-2 py-1 text-xs"
          >
            {choices.map((c) => (
              <option key={c} value={c}>
                {countryName(c)}
              </option>
            ))}
          </select>
        )}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-4 gap-y-2">
        <Logos label={t('discover.watch.network')} providers={w.networks} wide />
        <Logos label={t('discover.watch.stream')} providers={w.stream} link={w.link} />
        <Logos label={t('discover.watch.free')} providers={w.free} link={w.link} />
        <Logos label={t('discover.watch.rent')} providers={w.rent} link={w.link} />
        <Logos label={t('discover.watch.buy')} providers={w.buy} link={w.link} />
      </dl>
      {!offers && w.countries.length > 0 && <p className="text-sm text-muted">{t('discover.watch.none', { country: countryName(w.country) })}</p>}
      {w.countries.length > 0 && (
        <p className="text-xs text-muted">
          {w.link ? (
            <a href={w.link} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-text">
              {t('discover.watch.source')} <ExternalLink className="size-3" aria-hidden />
            </a>
          ) : (
            t('discover.watch.source')
          )}
        </p>
      )}
    </section>
  )
}
