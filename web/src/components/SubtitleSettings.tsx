import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api'
import i18n from '../i18n'
import { languageName } from '../media'
import { savedPreference } from '../player/Subtitles'
import {
  defaultSubtitles,
  fromOldChoice,
  subtitleLook,
  type SubtitleColor,
  type SubtitleEdge,
  type SubtitleMode,
  type SubtitlePosition,
  type SubtitlePrefs,
  type SubtitleSize,
} from '../player/subtitlePrefs'
import { Field, inputClass } from './ui'

// Languages people can add, beyond the ones they already have.
const commonLanguages = [
  'ar', 'bg', 'ca', 'cs', 'da', 'de', 'el', 'en', 'es', 'et', 'fa', 'fi', 'fr', 'he', 'hi', 'hr', 'hu', 'id', 'is', 'it', 'ja',
  'ko', 'lt', 'lv', 'ms', 'nb', 'nl', 'pl', 'pt', 'ro', 'ru', 'sk', 'sl', 'sr', 'sv', 'ta', 'th', 'tl', 'tr', 'uk', 'ur', 'vi', 'zh',
]

const modes: SubtitleMode[] = ['off', 'forced', 'foreign', 'always']
const sizes: SubtitleSize[] = ['small', 'medium', 'large', 'huge']
const colors: SubtitleColor[] = ['white', 'yellow', 'cyan', 'green']
const edges: SubtitleEdge[] = ['shadow', 'outline', 'none']
const positions: SubtitlePosition[] = ['bottom', 'raised']

// SubtitleSettings are the person's subtitle choices: when they come on, in
// which languages, and how they look. They follow the person to every app.
export function SubtitleSettings() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['playback'], queryFn: api.playback })
  const save = useMutation({ mutationFn: api.setPlayback, onSuccess: (p) => qc.setQueryData(['playback'], p) })
  const p: SubtitlePrefs = { ...defaultSubtitles, ...(data?.subtitles ?? fromOldChoice(savedPreference())) }
  const change = (c: Partial<SubtitlePrefs>) => save.mutate({ subtitles: { ...p, ...c } })
  const disabled = !data
  const languages = p.languages ?? []
  const look = subtitleLook(p)

  const choice = <T extends string>(label: string, value: T, options: T[], key: keyof SubtitlePrefs, names: string) => (
    <Field label={label}>
      <select className={inputClass} value={value} disabled={disabled} onChange={(e) => change({ [key]: e.target.value })}>
        {options.map((o) => (
          <option key={o} value={o}>
            {t(`common:subtitlePrefs.${names}.${o}`)}
          </option>
        ))}
      </select>
    </Field>
  )

  const addable = commonLanguages
    .filter((l) => !languages.includes(l))
    .map((l) => ({ code: l, name: languageName(l) }))
    .sort((a, b) => a.name.localeCompare(b.name, i18n.language))

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('common:subtitlePrefs.title')}</h2>
      <p className="text-sm text-muted">{t('common:subtitlePrefs.intro')}</p>
      <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
        <Field label={t('common:subtitlePrefs.when')}>
          <select className={inputClass} value={p.mode} disabled={disabled} onChange={(e) => change({ mode: e.target.value as SubtitleMode })}>
            {modes.map((m) => (
              <option key={m} value={m}>
                {t(`common:subtitlePrefs.mode.${m}`)}
              </option>
            ))}
          </select>
        </Field>
        <p className="-mt-2 text-sm text-muted">{t(`common:subtitlePrefs.modeHint.${p.mode}`)}</p>

        <div className="space-y-1.5">
          <span className="text-xs font-medium tracking-wide text-muted uppercase">{t('common:subtitlePrefs.languages')}</span>
          <div className="flex flex-wrap items-center gap-2">
            {languages.length === 0 && <span className="text-sm text-muted">{t('common:subtitlePrefs.appLanguage', { language: languageName(i18n.language) })}</span>}
            {languages.map((l, i) => (
              <span key={l} className="inline-flex items-center gap-1 rounded-full border border-line bg-surface px-3 py-1 text-sm">
                <span className="text-xs text-muted tabular-nums">{i + 1}</span> {languageName(l)}
                <button
                  className="rounded-full p-0.5 text-muted hover:text-text"
                  disabled={disabled}
                  aria-label={t('common:subtitlePrefs.removeLanguage', { language: languageName(l) })}
                  onClick={() => change({ languages: languages.filter((x) => x !== l) })}
                >
                  <X className="size-3.5" />
                </button>
              </span>
            ))}
            <select
              className="h-9 rounded-lg border border-line bg-surface px-2 text-sm"
              value=""
              disabled={disabled || languages.length >= 10}
              aria-label={t('common:subtitlePrefs.addLanguage')}
              onChange={(e) => e.target.value && change({ languages: [...languages, e.target.value] })}
            >
              <option value="">{t('common:subtitlePrefs.addLanguage')}</option>
              {addable.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.name}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          {choice(t('common:subtitlePrefs.size'), p.size ?? 'medium', sizes, 'size', 'sizes')}
          {choice(t('common:subtitlePrefs.color'), p.color ?? 'white', colors, 'color', 'colors')}
          {choice(t('common:subtitlePrefs.edge'), p.edge ?? 'shadow', edges, 'edge', 'edges')}
          {choice(t('common:subtitlePrefs.position'), p.position ?? 'bottom', positions, 'position', 'positions')}
        </div>
        <Field label={t('common:subtitlePrefs.background')}>
          <input
            type="range"
            min={0}
            max={100}
            step={5}
            className="w-full accent-[var(--color-accent)]"
            disabled={disabled}
            defaultValue={p.background ?? 55}
            key={p.background}
            onPointerUp={(e) => change({ background: Number(e.currentTarget.value) })}
            onKeyUp={(e) => change({ background: Number(e.currentTarget.value) })}
          />
        </Field>

        <div aria-hidden className="flex h-28 items-end justify-center rounded-lg bg-gradient-to-br from-slate-600 via-slate-800 to-slate-900 p-4">
          <span
            className="rounded px-[0.4em] py-[0.1em] text-center leading-snug font-medium"
            style={{ fontSize: `${1.1 * look.scale}rem`, color: look.color, textShadow: look.textShadow, backgroundColor: look.backgroundColor }}
          >
            {t('common:subtitlePrefs.sample')}
          </span>
        </div>
      </div>
    </section>
  )
}
