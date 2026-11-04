import { describe, expect, it } from 'vitest'
import type { SubtitleTrack } from '../api'
import { afterPick, defaultSubtitles, fromOldChoice, pickSubtitles, subtitleLook } from './subtitlePrefs'

const t = (id: string, language: string, extra: Partial<SubtitleTrack> = {}): SubtitleTrack => ({ id, label: id, language, codec: 'subrip', ...extra })
const en = t('en', 'en')
const enSdh = t('en-sdh', 'en', { sdh: true })
const fr = t('fr', 'fr')
const forcedEn = t('forced-en', 'en', { forced: true })
const forcedFr = t('forced-fr', 'fr', { forced: true, default: true })
const tracks = [enSdh, en, fr, forcedEn, forcedFr]

describe('subtitle choices', () => {
  it('off is off, even with a forced default track', () => {
    expect(pickSubtitles(tracks, { mode: 'off' }, { appLanguage: 'en' })).toBeUndefined()
  })
  it('forced lines in the language of the sound', () => {
    expect(pickSubtitles(tracks, defaultSubtitles, { appLanguage: 'en', audioLanguage: 'en' })?.id).toBe('forced-en')
    expect(pickSubtitles(tracks, defaultSubtitles, { appLanguage: 'en' })?.id).toBe('forced-fr')
  })
  it('always: my languages in order, plain over SDH', () => {
    expect(pickSubtitles(tracks, { mode: 'always', languages: ['de', 'fr'] }, { appLanguage: 'en' })?.id).toBe('fr')
    expect(pickSubtitles(tracks, { mode: 'always' }, { appLanguage: 'en-GB' })?.id).toBe('en')
  })
  it('foreign: only when the sound is in another language', () => {
    const p = { mode: 'foreign' as const, languages: ['en'] }
    expect(pickSubtitles(tracks, p, { appLanguage: 'en', audioLanguage: 'ja' })?.id).toBe('en')
    expect(pickSubtitles(tracks, p, { appLanguage: 'en', audioLanguage: 'en' })?.id).toBe('forced-en')
  })
  it('a pick in the player is remembered', () => {
    expect(afterPick(defaultSubtitles, undefined, 'en').mode).toBe('off')
    expect(afterPick({ mode: 'off', languages: ['en'] }, fr, 'en')).toMatchObject({ mode: 'always', languages: ['fr', 'en'] })
    expect(afterPick({ mode: 'always', languages: ['fr'] }, forcedEn, 'en').mode).toBe('forced')
  })
  it('reads the old browser choice', () => {
    expect(fromOldChoice('off').mode).toBe('off')
    expect(fromOldChoice('fr')).toMatchObject({ mode: 'always', languages: ['fr'] })
    expect(fromOldChoice(null).mode).toBe('forced')
  })
  it('looks', () => {
    expect(subtitleLook({ mode: 'always', background: 0, color: 'yellow' })).toMatchObject({ color: '#ffe14d', backgroundColor: 'rgb(0 0 0 / 0)' })
  })
})

describe('language codes', () => {
  it('treats three-letter and two-letter codes alike', () => {
    const tracks = [{ id: 'a', label: 'English', codec: 'subrip', language: 'eng' }]
    expect(pickSubtitles(tracks, { mode: 'foreign', languages: ['en'] }, { appLanguage: 'en', audioLanguage: 'fre' })?.id).toBe('a')
    expect(pickSubtitles(tracks, { mode: 'foreign', languages: ['en'] }, { appLanguage: 'en', audioLanguage: 'eng' })).toBeUndefined()
  })
})
