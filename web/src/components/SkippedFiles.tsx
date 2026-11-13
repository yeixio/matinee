import { useQuery } from '@tanstack/react-query'
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type SkipCount, type User } from '../api'

/**
 * Tells admins which files the last scan left out of a library and why,
 * such as arcade sets in a Games library, so they don't just go missing.
 */
export function SkippedFiles({ skipped }: { skipped?: SkipCount[] }) {
  const { t } = useTranslation()
  const { data: me } = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  if (!me?.isAdmin || !skipped?.length) return null
  const total = skipped.reduce((n, s) => n + s.count, 0)
  return (
    <details className="rounded-xl border border-line/60 bg-surface/50 px-4 py-3 text-sm">
      <summary className="flex cursor-pointer items-center gap-2 font-medium">
        <Info className="size-4 shrink-0 text-accent" />
        {t('library.skipped.title', { count: total })}
      </summary>
      <ul className="mt-2 space-y-2">
        {skipped.map((s) => (
          <li key={`${s.reason}/${s.group ?? ''}`}>
            <span className="font-medium" dir="auto">
              {s.group || t('library.skipped.topFolder')}
            </span>
            {': '}
            {s.reason === 'arcade'
              ? t('library.skipped.arcade', { count: s.count })
              : t('library.skipped.unknown', { count: s.count, example: s.example ?? '' })}
            {s.reason === 'arcade' && (
              <>
                {' '}
                <Link to="/settings/libraries" className="text-accent hover:underline">
                  {t('library.skipped.addArcade')}
                </Link>
              </>
            )}
          </li>
        ))}
      </ul>
    </details>
  )
}
