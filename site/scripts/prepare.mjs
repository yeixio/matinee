// Copies the guide (docs/guide) and the release notes (docs/release-notes)
// into src/content/docs/docs, where Starlight reads them.
//
//   DOCS_SRC      the repository folder to read docs/ from (default: this checkout)
//   DOCS_VERSION  the release being built, such as v0.2.0, or "next" or "dev"
//
// A page whose front matter says `since: v0.3.0` is left out of the guide
// for releases before v0.3.0, and so is a part of a page between
// <!-- since: v0.3.0 --> and <!-- /since -->, so the guide for an older
// release written after the fact only describes what that release had.
import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { compare, semver } from './prepare-lib.mjs'

const site = path.resolve(import.meta.dirname, '..')
const src = path.resolve(process.env.DOCS_SRC ?? path.join(site, '..'), 'docs')
const out = path.join(site, 'src/content/docs/docs')
const version = process.env.DOCS_VERSION ?? 'dev'
const base = process.env.SITE_BASE ?? '/'

const released = semver(version).length === 3

rmSync(path.join(site, 'src/content/docs'), { recursive: true, force: true })
mkdirSync(out, { recursive: true })
cpSync(path.join(src, 'guide'), out, { recursive: true })
rmSync(path.join(out, 'README.md'), { force: true })

function walk(dir) {
  return readdirSync(dir).flatMap((f) => {
    const p = path.join(dir, f)
    return statSync(p).isDirectory() ? walk(p) : [p]
  })
}

for (const file of walk(out).filter((f) => /\.mdx?$/.test(f))) {
  let text = readFileSync(file, 'utf8')
  const since = text.match(/^---\n[\s\S]*?^since:\s*(\S+)/m)?.[1]
  if (released && since && compare(since, version) > 0) {
    rmSync(file)
    continue
  }
  text = text.replace(/<!-- since: (\S+) -->\n?([\s\S]*?)<!-- \/since -->\n?/g, (_, v, part) => (released && compare(v, version) > 0 ? '' : part))
  // Links between pages are written from the site's root (/docs/...); keep
  // them inside this version's copy of the guide.
  text = text.replace(/\]\(\/docs\//g, `](${base}docs/`)
  if (!released) {
    const rel = path.relative(out, file)
    text = text.replace(/^---\n/, `---\neditUrl: https://github.com/yeixio/matinee/edit/main/docs/guide/${rel}\n`)
  }
  writeFileSync(file, text)
}

// Release notes, newest first, up to the release being built.
const notesDir = path.join(src, 'release-notes')
const notes = existsSync(notesDir)
  ? readdirSync(notesDir)
      .map((f) => f.replace(/\.md$/, ''))
      .filter((v) => semver(v).length === 3 && (!released || compare(v, version) <= 0))
      .sort((a, b) => compare(b, a))
  : []
mkdirSync(path.join(out, 'releases'), { recursive: true })
notes.forEach((v, i) => {
  const body = readFileSync(path.join(notesDir, `${v}.md`), 'utf8')
  writeFileSync(path.join(out, 'releases', `${v}.md`), `---\ntitle: Matinee ${v}\nsidebar:\n  label: ${v}\n  order: ${i}\n---\n\n${body}`)
})

// The sidebar comes from the guide's sidebar.json, plus the release notes.
// A section whose pages all came after this release is left out.
const hasPages = (dir) => existsSync(dir) && walk(dir).some((f) => /\.mdx?$/.test(f))
const sidebar = JSON.parse(readFileSync(path.join(out, 'sidebar.json'), 'utf8')).filter(
  (group) => !group.items?.every((i) => i.autogenerate && !hasPages(path.join(out, i.autogenerate.directory.replace(/^docs\/?/, '')))),
)
if (notes.length) sidebar.push({ label: 'Release notes', collapsed: true, items: [{ autogenerate: { directory: 'docs/releases' } }] })
writeFileSync(path.join(out, 'sidebar.json'), JSON.stringify(sidebar, null, 2))
console.log(`docs ${version}: ${walk(out).filter((f) => /\.mdx?$/.test(f)).length} pages`)
