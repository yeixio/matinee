import type { FoundSubtitle } from '../api'

// Languages to look for subtitles in, as BCP 47 tags the server turns into
// OpenSubtitles' codes: the ones Matinee speaks, and other common ones.
// prettier-ignore
export const searchLanguages = [
  'en', 'es', 'es-419', 'fr', 'de', 'it', 'pt-BR', 'pt', 'nl', 'pl', 'sv', 'no', 'da', 'fi', 'is', 'cs', 'sk', 'sl', 'hr', 'sr', 'bg',
  'mk', 'el', 'hu', 'ro', 'tr', 'ru', 'uk', 'et', 'lv', 'lt', 'ca', 'eu', 'gl', 'id', 'ms', 'vi', 'th', 'hi', 'bn', 'ta', 'te', 'ja', 'ko',
  'zh-Hans', 'zh-Hant', 'ar', 'he', 'fa', 'ur',
]

// The language a search starts in: the one the viewer last watched
// subtitles in, otherwise the page's.
export function startLanguage(saved: string | null, page: string): string {
  if (saved && saved !== 'off') return match(saved) ?? 'en'
  return match(page) ?? 'en'
}

function match(tag: string): string | undefined {
  const lower = tag.toLowerCase()
  const exact = searchLanguages.find((l) => l.toLowerCase() === lower)
  if (exact) return exact
  if (lower === 'nb' || lower === 'nn') return 'no'
  if (lower === 'zh-tw' || lower === 'zh-hk') return 'zh-Hant'
  if (lower.startsWith('zh')) return 'zh-Hans'
  const base = lower.split('-')[0]
  return searchLanguages.find((l) => l === base)
}

// The choices for the language picker: the list, with the current one in
// it even when it isn't.
export function searchLanguageChoices(current: string): string[] {
  return searchLanguages.includes(current) ? searchLanguages : [current, ...searchLanguages]
}

// A tag to name a result's language by, from OpenSubtitles' own codes
// where they say more than the language.
export function foundLanguage(f: FoundSubtitle): string {
  switch (f.variant) {
    case 'pt-br':
      return 'pt-BR'
    case 'ea':
      return 'es-419'
    case 'zh-cn':
      return 'zh-Hans'
    case 'zh-tw':
      return 'zh-Hant'
  }
  return f.language ?? ''
}
