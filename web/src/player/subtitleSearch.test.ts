import { describe, expect, it } from 'vitest'
import { foundLanguage, searchLanguageChoices, startLanguage } from './subtitleSearch'

describe('subtitle search languages', () => {
  it('starts in the last subtitle language, else the page language', () => {
    expect(startLanguage('fr', 'en')).toBe('fr')
    expect(startLanguage(null, 'pt-BR')).toBe('pt-BR')
    expect(startLanguage('off', 'de-AT')).toBe('de')
    expect(startLanguage(null, 'zh-TW')).toBe('zh-Hant')
    expect(startLanguage(null, 'nb')).toBe('no')
    expect(startLanguage('', 'xx')).toBe('en')
  })
  it('keeps the current language among the choices', () => {
    expect(searchLanguageChoices('en')[0]).toBe('en')
    expect(searchLanguageChoices('kk')[0]).toBe('kk')
  })
  it('names results by their variant', () => {
    expect(
      foundLanguage({
        id: 1,
        label: 'x',
        downloads: 0,
        language: 'pt',
        variant: 'pt-br',
      }),
    ).toBe('pt-BR')
    expect(foundLanguage({ id: 1, label: 'x', downloads: 0, language: 'en' })).toBe('en')
  })
})
