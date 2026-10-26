import { describe, expect, it } from 'vitest'
import type { Library } from '../api'
import { every, watchSummary } from './LibraryWatch'

const lib = (watch: Library['watch']): Library => ({ id: 1, name: 'Movies', kind: 'movies', paths: watch.map((w) => w.path), scanning: false, watch })

describe('every', () => {
  it('picks the largest whole unit', () => {
    expect(every(30)).toBe('30 seconds')
    expect(every(60)).toBe('1 minute')
    expect(every(900)).toBe('15 minutes')
    expect(every(7200)).toBe('2 hours')
    expect(every(90)).toBe('90 seconds')
  })
})

describe('watchSummary', () => {
  it('says how a library notices new files', () => {
    expect(watchSummary(lib([]), 30)).toBe('')
    expect(watchSummary(lib([{ path: '/m', method: 'events' }]), 30)).toBe('Watching for new files')
    expect(watchSummary(lib([{ path: '/m', method: 'polling', reason: 'network share (cifs)' }]), 30)).toBe(
      'Checked for new files every 30 seconds (network share)',
    )
    expect(watchSummary(lib([{ path: '/m', method: 'polling', reason: 'set to check on a timer' }]), 60)).toBe('Checked for new files every 1 minute')
    expect(
      watchSummary(
        lib([
          { path: '/a', method: 'events' },
          { path: '/b', method: 'polling' },
        ]),
        30,
      ),
    ).toBe('Some folders checked every 30 seconds')
    expect(watchSummary(lib([{ path: '/m', method: 'off' }]), 30)).toBe('Not watched for new files')
  })
})
