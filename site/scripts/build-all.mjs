// Builds the whole website into dist/: the landing page and the guide for
// the latest release at /, the guide for every release at /<tag>/, and the
// guide for main at /next/. versions.json at the root lists them for the
// version picker.
//
// Each release's guide is read from docs/ at its tag. A release tagged
// before the guide existed gets main's guide, less pages marked `since` a
// later release (see prepare.mjs).

import { execFileSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { compare, semver } from './prepare-lib.mjs'

const site = path.resolve(import.meta.dirname, '..')
const repo = path.resolve(site, '..')
const out = path.join(site, 'dist-all')
const git = (...args) => execFileSync('git', args, { cwd: repo, encoding: 'utf8' }).trim()

// Releases, newest first. Pre-releases (v1.2.0-beta.1) don't get a guide.
const tags = git('tag', '--list', 'v*')
  .split('\n')
  .filter((t) => semver(t).length === 3)
  .sort((a, b) => compare(b, a))
const latest = tags[0] ?? ''

function build(version, base, docsSrc) {
  console.log(`\n== ${version} at ${base}`)
  execFileSync('pnpm', ['run', 'build'], {
    cwd: site,
    stdio: 'inherit',
    env: { ...process.env, DOCS_VERSION: version, DOCS_LATEST: latest, SITE_BASE: base, DOCS_SRC: docsSrc },
  })
  const dest = path.join(out, base)
  mkdirSync(dest, { recursive: true })
  cpSync(path.join(site, 'dist'), dest, { recursive: true })
  if (base !== '/') {
    // Only the root build keeps the landing page.
    rmSync(path.join(dest, 'index.html'))
    writeFileSync(path.join(dest, 'index.html'), `<!doctype html><meta http-equiv="refresh" content="0; url=${base}docs/"><link rel="canonical" href="${base}docs/">`)
  }
}

/** A checkout of docs/ at a tag, or main's when the tag has no guide. */
function docsAt(tag) {
  const dir = mkdtempSync(path.join(tmpdir(), `matinee-docs-${tag}-`))
  const tar = path.join(dir, 'docs.tar')
  try {
    execFileSync('git', ['archive', '-o', tar, tag, 'docs/guide', 'docs/release-notes'], { cwd: repo, stdio: 'ignore' })
  } catch {
    return repo
  }
  execFileSync('tar', ['-xf', tar, '-C', dir])
  // Release notes for the tag itself are often written after it.
  cpSync(path.join(repo, 'docs/release-notes'), path.join(dir, 'docs/release-notes'), { recursive: true })
  return dir
}

rmSync(out, { recursive: true, force: true })
const versions = []
if (latest) {
  const src = docsAt(latest)
  build(latest, '/', src)
  build(latest, `/${latest}/`, src)
  versions.push({ id: latest, label: `${latest} (latest)`, base: '/' })
}
build('next', latest ? '/next/' : '/', repo)
versions.push({ id: 'next', label: 'Next (unreleased)', base: '/next/' })
for (const tag of tags.slice(1)) {
  build(tag, `/${tag}/`, docsAt(tag))
  versions.push({ id: tag, label: tag, base: `/${tag}/` })
}
writeFileSync(path.join(out, 'versions.json'), JSON.stringify(versions, null, 2))
if (!existsSync(path.join(out, 'index.html'))) throw new Error('no landing page was built')
console.log(`\nBuilt ${versions.map((v) => v.id).join(', ')} into ${out}`)
