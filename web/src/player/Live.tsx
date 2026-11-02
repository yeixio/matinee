import { useTranslation } from 'react-i18next'

// Stands in for the seek bar while a radio station plays: it's live.
export function LivePill({ className = '' }: { className?: string }) {
  const { t } = useTranslation()
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full bg-accent/15 px-2.5 py-0.5 text-[11px] font-semibold tracking-wider text-accent-strong uppercase ${className}`}>
      <span className="size-1.5 animate-pulse rounded-full bg-accent" aria-hidden />
      {t('common:radio.live')}
    </span>
  )
}
