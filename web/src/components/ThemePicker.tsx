import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { api, type Theme, type User } from '../api'
import { themeDescription, themeName, useSetMyTheme, useThemes } from '../theme'

// Appearance: everyone picks their own theme, which follows them to every
// device; an admin also picks the server's default.
export function Appearance({ me }: { me: User }) {
  const { t } = useTranslation()
  const { list, mine, active } = useThemes(me.id)
  const pick = useSetMyTheme(me.id)
  const fallback = list?.themes.find((t) => t.id === list.default)
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 className="text-lg font-semibold">{t('appearance.title')}</h2>
        {mine && fallback && (
          <button onClick={() => pick.mutate('')} className="text-sm text-muted underline-offset-4 hover:text-text hover:underline">
            {t('appearance.useServerTheme', { name: themeName(fallback) })}
          </button>
        )}
      </div>
      <p className="text-sm text-muted">
        {me.guest
          ? t('appearance.guestHint')
          : t('appearance.hint')}
      </p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3" role="radiogroup" aria-label={t('appearance.theme')}>
        {list?.themes.map((th) => (
          <ThemeCard key={th.id} theme={th} selected={th.id === active?.id} onPick={() => pick.mutate(th.id)} />
        ))}
      </div>
      {me.isAdmin && list && <ServerTheme themes={list.themes} />}
    </section>
  )
}

function ThemeCard({ theme, selected, onPick }: { theme: Theme; selected: boolean; onPick: () => void }) {
  return (
    <button
      role="radio"
      aria-checked={selected}
      onClick={onPick}
      className={`group rounded-xl p-1.5 text-start transition ${selected ? 'bg-accent/10 ring-2 ring-accent' : 'ring-1 ring-line hover:ring-faint'}`}
    >
      <ThemePreview theme={theme} />
      <span className="flex items-center gap-1.5 px-1.5 pt-2 text-sm font-medium">
        {themeName(theme)}
        {selected && <Check className="size-4 text-accent" />}
      </span>
      {theme.description && <span className="line-clamp-2 px-1.5 pb-1 text-xs text-muted">{themeDescription(theme)}</span>}
    </button>
  )
}

// ThemePreview draws a little home screen in a theme's own colours and font.
export function ThemePreview({ theme }: { theme: Theme }) {
  const { t } = useTranslation()
  const c = theme.colors
  const display = { fraunces: "'Fraunces Variable', serif", orbitron: "'Orbitron Variable', sans-serif" }[theme.fonts.display] ?? "'Inter Variable', sans-serif"
  return (
    <span aria-hidden className="block overflow-hidden rounded-lg" style={{ background: c.ink, boxShadow: `inset 0 0 0 1px ${c.line}` }}>
      <span className="flex items-center gap-1 px-2 py-1.5" style={{ background: c.surface }}>
        <span className="size-1.5 rounded-full" style={{ background: c.accent }} />
        <span className="h-1 w-6 rounded-full" style={{ background: c.muted }} />
        <span className="h-1 w-4 rounded-full" style={{ background: c.faint }} />
      </span>
      <span className="block space-y-1.5 p-2">
        <span className="block truncate text-sm leading-tight font-semibold" style={{ color: c.text, fontFamily: display }}>
          {t('appearance.preview.tonight')}
        </span>
        <span className="flex gap-1">
          <span className="rounded px-1.5 py-0.5 text-[9px] font-semibold" style={{ background: c.accent, color: c.onAccent }}>
            {t('item.play')}
          </span>
          <span className="rounded px-1.5 py-0.5 text-[9px]" style={{ background: c.raised, color: c.text }}>
            {t('appearance.preview.trailer')}
          </span>
        </span>
        <span className="grid grid-cols-4 gap-1">
          {[0, 1, 2, 3].map((i) => (
            <span
              key={i}
              className="aspect-[2/3] rounded-sm"
              style={{ background: c.raised, boxShadow: i === 0 ? `0 0 0 1.5px ${c.accent}, 0 0 6px ${c.glow}` : undefined }}
            />
          ))}
        </span>
      </span>
    </span>
  )
}

function ServerTheme({ themes }: { themes: Theme[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => {
      qc.setQueryData(['settings'], s)
      qc.invalidateQueries({ queryKey: ['themes'] })
    },
  })
  return (
    <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
      <label className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="min-w-0 flex-1">
          <span className="block font-medium">{t('appearance.serverTheme')}</span>
          <span className="mt-1 block text-sm text-muted">{t('appearance.serverThemeHint')}</span>
        </span>
        <select
          className="rounded-lg border border-line bg-raised px-3 py-2 text-sm"
          value={data?.theme ?? ''}
          disabled={!data || save.isPending}
          onChange={(e) => save.mutate({ theme: e.target.value })}
        >
          {themes.map((th) => (
            <option key={th.id} value={th.id}>
              {th.builtIn ? themeName(th) : t('appearance.custom', { name: th.name })}
            </option>
          ))}
        </select>
      </label>
      <p className="text-xs text-faint">
        <Trans i18nKey="appearance.addYourOwn" components={{ code: <code className="text-muted" /> }} />{' '}
        <a
          href="https://github.com/yeixio/matinee/blob/main/docs/themes.md"
          target="_blank"
          rel="noreferrer"
          className="underline hover:text-muted"
        >
          {t('appearance.howTo')}
        </a>
      </p>
      {!!data?.themeProblems.length && (
        <ul className="space-y-1 text-sm text-danger">
          {data.themeProblems.map((p) => (
            <li key={p}>{t('appearance.skipped', { problem: p })}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
