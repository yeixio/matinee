import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { api, type Theme, type User } from '../api'
import { useSetMyTheme, useThemes } from '../theme'

// Appearance: everyone picks their own theme, which follows them to every
// device; an admin also picks the server's default.
export function Appearance({ me }: { me: User }) {
  const { list, mine, active } = useThemes(me.id)
  const pick = useSetMyTheme(me.id)
  const fallback = list?.themes.find((t) => t.id === list.default)
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 className="text-lg font-semibold">Appearance</h2>
        {mine && fallback && (
          <button onClick={() => pick.mutate('')} className="text-sm text-muted underline-offset-4 hover:text-text hover:underline">
            Use the server's theme ({fallback.name})
          </button>
        )}
      </div>
      <p className="text-sm text-muted">
        {me.guest
          ? 'Everyone at home without an account shares this theme.'
          : 'Your theme follows you to the Matinee apps on your phone and TV.'}
      </p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3" role="radiogroup" aria-label="Theme">
        {list?.themes.map((t) => (
          <ThemeCard key={t.id} theme={t} selected={t.id === active?.id} onPick={() => pick.mutate(t.id)} />
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
      className={`group rounded-xl p-1.5 text-left transition ${selected ? 'bg-accent/10 ring-2 ring-accent' : 'ring-1 ring-line hover:ring-faint'}`}
    >
      <ThemePreview theme={theme} />
      <span className="flex items-center gap-1.5 px-1.5 pt-2 text-sm font-medium">
        {theme.name}
        {selected && <Check className="size-4 text-accent" />}
      </span>
      {theme.description && <span className="line-clamp-2 px-1.5 pb-1 text-xs text-muted">{theme.description}</span>}
    </button>
  )
}

// ThemePreview draws a little home screen in a theme's own colours and font.
export function ThemePreview({ theme }: { theme: Theme }) {
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
          Tonight
        </span>
        <span className="flex gap-1">
          <span className="rounded px-1.5 py-0.5 text-[9px] font-semibold" style={{ background: c.accent, color: c.onAccent }}>
            Play
          </span>
          <span className="rounded px-1.5 py-0.5 text-[9px]" style={{ background: c.raised, color: c.text }}>
            Trailer
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
          <span className="block font-medium">Server theme</span>
          <span className="mt-1 block text-sm text-muted">What everyone sees until they pick their own, and on the sign-in page.</span>
        </span>
        <select
          className="rounded-lg border border-line bg-raised px-3 py-2 text-sm"
          value={data?.theme ?? ''}
          disabled={!data || save.isPending}
          onChange={(e) => save.mutate({ theme: e.target.value })}
        >
          {themes.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
              {t.builtIn ? '' : ' (custom)'}
            </option>
          ))}
        </select>
      </label>
      <p className="text-xs text-faint">
        To add your own, put a theme file in the server's <code className="text-muted">themes</code> folder, in its data
        folder.{' '}
        <a
          href="https://github.com/yeixio/matinee/blob/main/docs/themes.md"
          target="_blank"
          rel="noreferrer"
          className="underline hover:text-muted"
        >
          How to make a theme
        </a>
      </p>
      {!!data?.themeProblems.length && (
        <ul className="space-y-1 text-sm text-danger">
          {data.themeProblems.map((p) => (
            <li key={p}>Skipped {p}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
