// Refreshes src/roadmap.json, the data behind the Roadmap page, from the
// target/* and priority/* labels on the open issues in yeixio/matinee and
// yeixio/matinee-apps.
//
//   cd site && pnpm roadmap
//
// Needs the GitHub CLI signed in with access to both repositories
// (matinee-apps is private). The targets themselves (names and summaries)
// are written by hand in src/roadmap.json; this only replaces the issues.
// An open issue with no target/* label stays off the page.
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'

const file = path.resolve(import.meta.dirname, '../src/roadmap.json')
const roadmap = JSON.parse(readFileSync(file, 'utf8'))
const targets = new Set(roadmap.targets.map((t) => t.id))

/** Every open issue (not pull request) in a repository. */
function openIssues(repo) {
  const out = execFileSync('gh', ['api', '--paginate', `repos/yeixio/${repo}/issues?state=open&per_page=100`, '--jq', '.[] | select(.pull_request == null) | {number, title, labels: [.labels[].name]}'], {
    encoding: 'utf8',
    maxBuffer: 64 << 20,
  })
  return out
    .split('\n')
    .filter(Boolean)
    .map((line) => JSON.parse(line))
}

const items = []
for (const repo of ['matinee', 'matinee-apps']) {
  for (const issue of openIssues(repo)) {
    const target = issue.labels.find((l) => l.startsWith('target/'))?.slice('target/'.length)
    if (!target) continue
    if (!targets.has(target)) throw new Error(`${repo}#${issue.number} has target/${target}, which src/roadmap.json doesn't list`)
    const priority = Number(issue.labels.find((l) => /^priority\/P\d$/.test(l))?.slice(-1) ?? 3)
    items.push({ repo, number: issue.number, title: issue.title, target, priority })
  }
}
items.sort((a, b) => a.priority - b.priority || a.repo.localeCompare(b.repo) || a.number - b.number)

roadmap.updated = new Date().toISOString().slice(0, 10)
roadmap.items = items
writeFileSync(file, JSON.stringify(roadmap, null, 2) + '\n')
console.log(`Wrote ${items.length} issues to ${path.relative(process.cwd(), file)}`)
