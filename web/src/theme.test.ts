import { describe, expect, it } from 'vitest'
import type { Theme, ThemeList } from './api'
import { activeTheme, themeCSS } from './theme'

const theme = (id: string, extra: Partial<Theme> = {}): Theme => ({
  id,
  name: id,
  scheme: 'dark',
  builtIn: true,
  fonts: { display: 'orbitron', body: 'inter' },
  colors: { ink: '#000000', accentStrong: '#7cf1ff', onAccent: '#03121a' } as Theme['colors'],
  ...extra,
})

describe('themes', () => {
  it('turns a theme into the CSS variables styles.css defines', () => {
    const css = themeCSS(theme('sci-fi', { scheme: 'light' }))
    expect(css).toContain('--color-ink:#000000')
    expect(css).toContain('--color-accent-strong:#7cf1ff')
    expect(css).toContain('--color-on-accent:#03121a')
    expect(css).toContain("--font-display:'Orbitron Variable'")
    expect(css).toContain("--font-sans:'Inter Variable'")
    expect(css).toContain('color-scheme:light')
  })

  it("wears someone's own pick, else the server's, else the first", () => {
    const list: ThemeList = { default: 'light', themes: [theme('matinee'), theme('light'), theme('sci-fi')] }
    expect(activeTheme(list, 'sci-fi')?.id).toBe('sci-fi')
    expect(activeTheme(list, '')?.id).toBe('light')
    expect(activeTheme(list, 'removed')?.id).toBe('light')
    expect(activeTheme({ ...list, default: 'gone' }, undefined)?.id).toBe('matinee')
    expect(activeTheme(undefined, 'sci-fi')).toBeUndefined()
  })
})
