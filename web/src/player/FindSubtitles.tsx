import { useMutation, useQuery } from '@tanstack/react-query'
import { ArrowLeft, Check, Download, LoaderCircle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type FoundSubtitle, type SubtitleTrack } from '../api'
import { languageName } from '../media'
import { savedPreference } from './Subtitles'
import { foundLanguage, searchLanguageChoices, startLanguage } from './findSubtitles'

type Props = {
  itemId: number
  onBack: () => void
  /** A file was added to the video: show it. */
  onAdded: (track: SubtitleTrack) => void
}

// FindSubtitles looks for subtitles online, in the subtitles menu: a
// language to look in, then what turned up, best first. Picking one adds it
// to the video for everyone and shows it.
export function FindSubtitles({ itemId, onBack, onAdded }: Props) {
  const { t: tr, i18n } = useTranslation()
  const [language, setLanguage] = useState(() => startLanguage(savedPreference(), i18n.language))
  const search = useQuery({
    queryKey: ['subtitleSearch', itemId, language],
    queryFn: () => api.searchSubtitles(itemId, language),
    staleTime: 10 * 60_000,
    retry: false,
  })
  const add = useMutation({ mutationFn: (f: FoundSubtitle) => api.addSubtitle(itemId, f), onSuccess: onAdded })
  const choices = searchLanguageChoices(language)

  return (
    <div className="w-[min(26rem,calc(100vw-2rem))]">
      <div className="flex items-center gap-2 px-1.5 pt-1 pb-2">
        <button onClick={onBack} className="rounded-full p-1.5 hover:bg-white/10" aria-label={tr('back')}>
          <ArrowLeft className="size-4" />
        </button>
        <div className="flex-1 text-xs font-medium tracking-wide text-white/50 uppercase">{tr('player.findSubtitles.title')}</div>
        <label className="sr-only" htmlFor="find-subtitles-language">
          {tr('player.findSubtitles.language')}
        </label>
        <select
          id="find-subtitles-language"
          value={language}
          onChange={(e) => setLanguage(e.target.value)}
          className="max-w-40 rounded-md border border-white/15 bg-black/60 px-2 py-1 text-sm"
        >
          {choices.map((c) => (
            <option key={c} value={c}>
              {languageName(c)}
            </option>
          ))}
        </select>
      </div>
      {search.isPending && (
        <p className="flex items-center gap-2 px-3 py-6 text-sm text-white/70">
          <LoaderCircle className="size-4 animate-spin" /> {tr('player.findSubtitles.looking')}
        </p>
      )}
      {search.error && <p className="px-3 py-4 text-sm text-red-300">{search.error.message}</p>}
      {search.data && search.data.results.length === 0 && (
        <p className="px-3 py-4 text-sm text-white/70">
          {tr('player.findSubtitles.none', { language: languageName(language) })}
        </p>
      )}
      {add.error && <p className="px-3 pb-2 text-sm text-red-300">{add.error.message}</p>}
      {search.data && search.data.results.length > 0 && (
        <ul role="menu" className="max-h-80 overflow-y-auto">
          {search.data.results.map((f) => {
            const have = search.data.have.includes(f.id)
            const busy = add.isPending && add.variables?.id === f.id
            const notes = [
              f.matchesFile && tr('player.findSubtitles.matchesFile'),
              f.sdh && tr('common:media.sdh'),
              f.forced && tr('common:media.forced'),
              f.machine && tr('player.findSubtitles.machine'),
            ].filter(Boolean)
            return (
              <li key={f.id}>
                <button
                  role="menuitem"
                  disabled={add.isPending}
                  onClick={() => add.mutate(f)}
                  className="flex w-full items-start gap-3 rounded-md px-3 py-2 text-start text-sm hover:bg-white/10 disabled:opacity-60"
                >
                  {busy ? (
                    <LoaderCircle className="mt-0.5 size-4 shrink-0 animate-spin" />
                  ) : have ? (
                    <Check className="mt-0.5 size-4 shrink-0 text-accent" />
                  ) : (
                    <Download className="mt-0.5 size-4 shrink-0 text-white/60" />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{f.release || languageName(foundLanguage(f)) || f.label}</span>
                    <span className={`block truncate text-xs ${f.matchesFile ? 'text-accent' : 'text-white/50'}`}>
                      {[languageName(foundLanguage(f)) || f.label, ...notes].join(' · ')}
                    </span>
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
      <p className="px-3 pt-2 pb-1 text-[11px] text-white/40">{tr('player.findSubtitles.source')}</p>
    </div>
  )
}
