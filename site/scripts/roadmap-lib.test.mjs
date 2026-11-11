import assert from 'node:assert/strict'
import test from 'node:test'
import { itemsFor, sameItems, sortItems } from './roadmap-lib.mjs'

const targets = new Set(['v0.3', 'later'])
const issue = (number, state, labels, extra = {}) => ({ number, title: `Issue ${number}`, state, labels: labels.map((name) => ({ name })), ...extra })

test('open and completed issues with a target make the roadmap', () => {
  const items = itemsFor(
    'matinee',
    [
      issue(1, 'open', ['target/v0.3', 'priority/P1']),
      issue(2, 'closed', ['target/v0.3', 'priority/P2'], { state_reason: 'completed', closed_at: '2026-11-04T10:00:00Z' }),
      issue(3, 'closed', ['target/v0.3'], { state_reason: 'not_planned' }),
      issue(4, 'closed', ['target/v0.3'], { state_reason: 'duplicate' }),
      issue(5, 'open', ['bug']),
      issue(6, 'open', ['target/later'], { pull_request: {} }),
      issue(7, 'open', ['target/later']),
    ],
    targets,
  )
  assert.deepEqual(items, [
    { repo: 'matinee', number: 1, title: 'Issue 1', target: 'v0.3', priority: 1 },
    { repo: 'matinee', number: 2, title: 'Issue 2', target: 'v0.3', priority: 2, done: true, closed: '2026-11-04' },
    { repo: 'matinee', number: 7, title: 'Issue 7', target: 'later', priority: 3 },
  ])
})

test('an unknown target fails loudly', () => {
  assert.throws(() => itemsFor('matinee', [issue(1, 'open', ['target/v9'])], targets), /target\/v9/)
})

test('open items come first by priority, done items newest first', () => {
  const items = sortItems([
    { repo: 'matinee', number: 9, priority: 3 },
    { repo: 'matinee', number: 8, priority: 2, done: true, closed: '2026-11-01' },
    { repo: 'matinee-apps', number: 1, priority: 1 },
    { repo: 'matinee', number: 7, priority: 1, done: true, closed: '2026-11-05' },
    { repo: 'matinee', number: 2, priority: 1 },
  ])
  assert.deepEqual(
    items.map((i) => `${i.repo}#${i.number}`),
    ['matinee#2', 'matinee-apps#1', 'matinee#9', 'matinee#7', 'matinee#8'],
  )
})

test('sameItems compares the whole list', () => {
  assert.ok(sameItems([{ number: 1 }], [{ number: 1 }]))
  assert.ok(!sameItems([{ number: 1 }], [{ number: 1, done: true }]))
  assert.ok(!sameItems(undefined, [{ number: 1 }]))
})
