import { describe, expect, it } from 'vitest'
import { activeCount, clearPicks, decades, fromSearchParams, runtimeLabel, toSearchParams, toggleIn, yearLabel } from './filter'

describe('filter', () => {
  it('round-trips through the URL with the server’s parameter names', () => {
    const f = {
      genre: ['Comedy', 'Drama'],
      yearMin: 1980,
      yearMax: 1989,
      watched: 'unwatched' as const,
      person: [1, 2],
      watchList: true,
      ratingMin: 7.5,
      sort: 'year' as const,
      order: 'asc' as const,
    }
    const p = toSearchParams(f)
    expect(p.toString()).toBe(
      'genre=Comedy&genre=Drama&yearMin=1980&yearMax=1989&ratingMin=7.5&watched=unwatched&person=1&person=2&watchList=true&sort=year&order=asc',
    )
    expect(fromSearchParams(p)).toEqual(f)
  })

  it('ignores other parameters and bad values', () => {
    expect(fromSearchParams(new URLSearchParams('yearMin=abc&person=1,x,3&other=1&watchList=no&genre='))).toEqual({ person: [1, 3] })
  })

  it('counts and clears what picks titles, keeping the sort', () => {
    const f = { genre: ['A', 'B'], watched: 'watched' as const, sort: 'rating' as const }
    expect(activeCount(f)).toBe(3)
    expect(activeCount({ yearMin: 1980, yearMax: 1989, runtimeMax: 90 })).toBe(2)
    expect(clearPicks(f)).toEqual({ sort: 'rating', order: undefined })
  })

  it('toggles values in lists', () => {
    expect(toggleIn(undefined, 'a')).toEqual(['a'])
    expect(toggleIn(['a'], 'a')).toBeUndefined()
    expect(toggleIn([1, 2], 2)).toEqual([1])
  })

  it('labels ranges', () => {
    expect(decades(1979, 2004)).toEqual([2000, 1990, 1980, 1970])
    expect(yearLabel({ yearMin: 1980, yearMax: 1989 })).toBe('1980s')
    expect(yearLabel({ yearMin: 1994, yearMax: 1994 })).toBe('1994')
    expect(yearLabel({ yearMin: 2000 })).toBe('2000 or later')
    expect(runtimeLabel({ runtimeMax: 90 }, false)).toBe('Under 90 min')
    expect(runtimeLabel({ runtimeMin: 30, runtimeMax: 60 }, true)).toBe('Episodes 30–60 min')
  })
})
