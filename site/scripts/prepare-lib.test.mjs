// cd site && node --test scripts/*.test.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { featureMarkdown, featureProblems } from './prepare-lib.mjs'

const table = (apps) => ({ tables: [{ id: 't', title: 'Watching', apps: [{ id: 'web', name: 'Web' }, { id: 'tv', name: 'TV' }], groups: [{ title: 'Playing', features: [{ name: 'Skip intro', doc: '/docs/x/', since: 'v0.1.10', apps }] }] }] })

test('a complete table has no problems', () => {
  assert.deepEqual(featureProblems(table({ web: true, tv: 'matinee-apps#12' })), [])
  assert.deepEqual(featureProblems(table({ web: true, tv: { na: 'No need.' } })), [])
})

test('every app has to be accounted for', () => {
  const [p] = featureProblems(table({ web: true }))
  assert.match(p, /Skip intro: says nothing about tv/)
  assert.equal(featureProblems(table({ web: true, tv: false })).length, 1)
  assert.equal(featureProblems(table({ web: true, tv: '#12' })).length, 1)
  assert.equal(featureProblems(table({ web: true, tv: { na: ' ' } })).length, 1)
  assert.equal(featureProblems(table({ web: true, tv: true, car: true })).length, 1)
})

test('links have to be guide pages', () => {
  assert.match(featureProblems(table({ web: true, tv: true }), () => false)[0], /isn't a guide page/)
})

test('only the server repository is linked', () => {
  const md = featureMarkdown(table({ web: 'matinee#43', tv: 'matinee-apps#129' }))
  assert.match(md, /\| \[Skip intro\]\(\/docs\/x\/\) \| \[Planned\]\(https:\/\/github.com\/yeixio\/matinee\/issues\/43\) \| Planned \|/)
})

test('reasons are listed under the table', () => {
  const md = featureMarkdown(table({ web: { na: 'Browsers stream.' }, tv: { na: 'Browsers stream.' } }))
  assert.match(md, /- \*\*Skip intro\*\* on Web and TV: Browsers stream\./)
})

test('the real table is complete', () => {
  const data = JSON.parse(readFileSync(new URL('../../docs/features.json', import.meta.url), 'utf8'))
  assert.deepEqual(featureProblems(data), [])
})
