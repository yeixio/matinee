import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { api, type Theme, type ThemeColor, type ThemeList } from './api'

// Themes swap the colour and font variables that styles.css defines (the
// Tailwind tokens: bg-ink, text-muted, bg-accent...), so a new theme needs no
// rebuild. The server sends complete themes; see docs/themes.md.

// The typefaces every client ships. A theme names one of these.
const fontStacks: Record<string, string> = {
  fraunces: "'Fraunces Variable', ui-serif, Georgia, serif",
  inter: "'Inter Variable', ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
  orbitron: "'Orbitron Variable', 'Inter Variable', ui-sans-serif, system-ui, sans-serif",
}

// accentStrong -> --color-accent-strong, as Tailwind names them.
const cssVar = (name: string) => '--color-' + name.replace(/[A-Z]/g, (c) => '-' + c.toLowerCase())

// themeCSS is the theme as inline style for <html>.
export function themeCSS(t: Theme): string {
  const vars = Object.entries(t.colors).map(([k, v]) => `${cssVar(k)}:${v}`)
  vars.push(`--font-display:${fontStacks[t.fonts.display] ?? fontStacks.fraunces}`)
  vars.push(`--font-sans:${fontStacks[t.fonts.body] ?? fontStacks.inter}`)
  vars.push(`color-scheme:${t.scheme}`)
  return vars.join(';')
}

// The applied theme is kept so index.html can put it on before the first
// paint, without a flash of the default look.
const storageKey = 'matinee-theme'

export function applyTheme(t: Theme) {
  const css = themeCSS(t)
  const root = document.documentElement
  if (root.style.cssText !== css) root.style.cssText = css
  root.dataset.theme = t.id
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', t.colors.ink)
  try {
    localStorage.setItem(storageKey, JSON.stringify({ id: t.id, css, ink: t.colors.ink }))
  } catch {
    // private browsing; the theme still applies, a moment later next time
  }
}

// activeTheme is someone's own pick, else the server's default, else the first.
export function activeTheme(list: ThemeList | undefined, mine: string | undefined): Theme | undefined {
  if (!list) return undefined
  const find = (id?: string) => (id ? list.themes.find((t) => t.id === id) : undefined)
  return find(mine) ?? find(list.default) ?? list.themes[0]
}

// userId is who's signed in (the Home account counts), if anyone.
export function useThemes(userId?: number) {
  const list = useQuery({ queryKey: ['themes'], queryFn: api.themes, staleTime: 5 * 60_000 })
  const mine = useQuery({ queryKey: ['my-theme', userId], queryFn: api.myTheme, enabled: userId !== undefined, staleTime: Infinity })
  return { list: list.data, mine: mine.data?.theme ?? '', active: activeTheme(list.data, mine.data?.theme) }
}

// ThemeSync wears the right theme on every page, the sign-in page included.
export function ThemeSync({ userId }: { userId?: number }) {
  const { active } = useThemes(userId)
  useEffect(() => {
    if (active) applyTheme(active)
  }, [active])
  return null
}

export function useSetMyTheme(userId?: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.setMyTheme,
    // Wear it right away; the server keeps it for every device.
    onMutate: (theme) => qc.setQueryData(['my-theme', userId], { theme }),
    onError: () => qc.invalidateQueries({ queryKey: ['my-theme', userId] }),
  })
}

export type { Theme, ThemeColor }
