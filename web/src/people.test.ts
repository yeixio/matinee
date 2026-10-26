import { describe, expect, it } from 'vitest'
import type { Credit } from './api'
import { crewLine, lifeLabel, money, roleLabel } from './people'

const c = (personId: number, name: string, role: string, episodes?: number): Credit => ({ personId, name, role, episodes, hasPhoto: false })

describe('crewLine', () => {
  it('names each key job once, with plurals', () => {
    const lines = crewLine([
      c(1, 'Michael Mann', 'Director'),
      c(1, 'Michael Mann', 'Screenplay'),
      c(1, 'Michael Mann', 'Writer'),
      c(2, 'Someone Else', 'Writer'),
      c(3, 'Elliot Goldenthal', 'Original Music Composer'),
      c(4, 'A Producer', 'Producer'),
    ])
    expect(lines.map((l) => `${l.label}: ${l.people.map((p) => p.name).join(', ')}`)).toEqual([
      'Director: Michael Mann',
      'Writers: Michael Mann, Someone Else',
      'Music: Elliot Goldenthal',
    ])
  })

  it('names a show by its creators rather than its directors', () => {
    const lines = crewLine([c(1, 'Vince Gilligan', 'Creator'), c(2, 'Episode Director', 'Director')])
    expect(lines.map((l) => l.label)).toEqual(['Created by'])
  })
})

describe('labels', () => {
  it('counts episodes', () => {
    expect(roleLabel(c(1, 'Bryan Cranston', 'Walter White', 62))).toBe('Walter White · 62 episodes')
    expect(roleLabel(c(1, 'Guest', '', 1))).toBe('1 episode')
    expect(roleLabel(c(1, 'Al Pacino', 'Vincent Hanna'))).toBe('Vincent Hanna')
  })

  it('formats money', () => {
    expect(money(187436818)).toBe('$187.4 million')
    expect(money(2_923_706_026)).toBe('$2.9 billion')
    expect(money(0)).toBe('')
  })

  it('describes a life', () => {
    expect(lifeLabel({ birthday: '1940-04-25', deathday: '2020-01-01' })).toBe('1940 – 2020')
    expect(lifeLabel({ birthplace: 'Cleveland, Ohio' })).toBe('Born in Cleveland, Ohio')
  })
})
