import { useTranslation } from 'react-i18next'
import type { User } from '../api'
import i18n, { applyLanguage, availableLanguages, languageName, languages, pseudoLocale, pseudoRtlLocale, resolveLanguage, systemLanguages } from '../i18n'
import { useMyLanguage, useSetMyLanguage } from '../i18n/sync'

// Language: everyone picks their own, which follows them to every device, or
// leaves it to each browser. Titles and descriptions follow it where TMDB has
// them in that language.
export function LanguagePicker({ me }: { me: User }) {
  const { t } = useTranslation()
  const saved = useMyLanguage(me.id).data?.language
  const pick = useSetMyLanguage(me.id)
  const shown = languages.filter((l) => availableLanguages.includes(l.code))
  const browser = resolveLanguage('', systemLanguages())
  const current = shown.find((l) => l.code === i18n.language)
  // Pseudo-languages are for checking layouts while developing, so they stay in this browser.
  const pseudo = import.meta.env.DEV ? [pseudoLocale, pseudoRtlLocale] : []
  const value = pseudo.includes(i18n.language) ? i18n.language : (saved ?? '')

  const change = (code: string) => {
    if (pseudo.includes(code)) void applyLanguage(code)
    else pick.mutate(code)
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('language.title')}</h2>
      <label className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-line/60 bg-surface/50 p-4">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('language.label')}</span>
          <span className="mt-1 block text-sm text-muted">{me.guest ? t('language.guestHint') : t('language.hint')}</span>
        </span>
        <select
          className="rounded-lg border border-line bg-raised px-3 py-2 text-sm"
          value={value}
          disabled={saved === undefined || pick.isPending}
          onChange={(e) => change(e.target.value)}
        >
          <option value="">{t('language.automatic', { language: languageName(browser) })}</option>
          {shown.map((l) => (
            <option key={l.code} value={l.code} lang={l.code}>
              {l.name}
            </option>
          ))}
          {pseudo.map((code) => (
            <option key={code} value={code}>
              {code}
            </option>
          ))}
        </select>
      </label>
      {current?.status === 'machine' && (
        <p className="text-xs text-faint">
          {t('language.machine')}{' '}
          <a
            href="https://github.com/yeixio/matinee/blob/main/i18n/README.md"
            target="_blank"
            rel="noreferrer"
            className="underline hover:text-muted"
          >
            {t('language.help')}
          </a>
        </p>
      )}
      {pick.error && <p className="text-sm text-danger">{pick.error.message}</p>}
    </section>
  )
}
