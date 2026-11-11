// Refreshes src/roadmap.json, the data behind the Roadmap page, from the
// target/* and priority/* labels on the issues in yeixio/matinee and
// yeixio/matinee-apps: open issues are what's planned, issues closed as
// completed are what's done.
//
//   cd site && pnpm roadmap [--previous <roadmap.json>]
//
// The Site workflow runs this before every build, and on issue changes and
// on a schedule, so the page follows GitHub without anyone running it. It
// needs the GitHub CLI signed in (GH_TOKEN works). matinee-apps is private:
// ROADMAP_APPS_TOKEN, if set, is used for it instead. When the apps' issues
// can't be read, their items are kept from the previous data.
//
// The targets themselves (names and summaries) are written by hand in
// src/roadmap.json; this only replaces the issues. An issue with no target/*
// label stays off the page.
//
// --previous names the data the live site was built from. When the issues
// haven't changed since, its `updated` date is kept, and "changed=false" is
// written to $GITHUB_OUTPUT so the workflow can skip the rebuild.
import { execFileSync } from 'node:child_process'
import { appendFileSync, existsSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { itemsFor, sameItems, sortItems } from './roadmap-lib.mjs'

const file = path.resolve(import.meta.dirname, '../src/roadmap.json')
const roadmap = JSON.parse(readFileSync(file, 'utf8'))
const targets = new Set(roadmap.targets.map((t) => t.id))

const flag = process.argv.indexOf('--previous')
const previousFile = flag > 0 ? process.argv[flag + 1] : undefined
const previous = previousFile && existsSync(previousFile) ? JSON.parse(readFileSync(previousFile, 'utf8')) : undefined

/** Every issue in a repository, open and closed. */
function issues(repo, token) {
  const env = token ? { ...process.env, GH_TOKEN: token } : process.env
  const all = []
  // Page by page rather than --paginate: some proxies refuse the
  // repositories/{id} links GitHub returns for the next page.
  for (let page = 1; ; page++) {
    const out = execFileSync('gh', ['api', `repos/yeixio/${repo}/issues?state=all&per_page=100&page=${page}`], {
      encoding: 'utf8',
      env,
      maxBuffer: 64 << 20,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    const batch = JSON.parse(out)
    all.push(...batch)
    if (batch.length < 100) return all
  }
}

const items = []
for (const [repo, token] of [
  ['matinee', undefined],
  ['matinee-apps', process.env.ROADMAP_APPS_TOKEN],
]) {
  try {
    items.push(...itemsFor(repo, issues(repo, token), targets))
  } catch (err) {
    if (repo === 'matinee' || err.message.includes('target/')) throw err
    const kept = (previous ?? roadmap).items.filter((i) => i.repo === repo)
    console.warn(`Couldn't read the ${repo} issues (${String(err.stderr || err.message).trim()}); keeping its ${kept.length} items from before.`)
    console.warn('Set the ROADMAP_TOKEN secret to a token that can read its issues.')
    items.push(...kept)
  }
}
sortItems(items)

const changed = !previous || !sameItems(previous.items, items)
roadmap.updated = changed || !previous.updated ? new Date().toISOString().slice(0, 10) : previous.updated
roadmap.items = items
writeFileSync(file, JSON.stringify(roadmap, null, 2) + '\n')
if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `changed=${changed}\n`)
const done = items.filter((i) => i.done).length
console.log(`Wrote ${items.length - done} planned and ${done} done issues to ${path.relative(process.cwd(), file)}${changed ? '' : ' (no change)'}`)
