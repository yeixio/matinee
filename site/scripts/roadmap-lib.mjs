// The parts of scripts/roadmap.mjs that don't talk to GitHub, kept apart so
// roadmap-lib.test.mjs can check them.

/**
 * The roadmap items for the issues of one repository: every issue with a
 * target/* label that is open, or closed as completed. Issues closed as not
 * planned or as duplicates stay off the page, and so do pull requests.
 */
export function itemsFor(repo, issues, targets) {
  const items = []
  for (const issue of issues) {
    if (issue.pull_request) continue
    if (issue.state === 'closed' && issue.state_reason !== 'completed') continue
    const labels = issue.labels.map((l) => (typeof l === 'string' ? l : l.name))
    const target = labels.find((l) => l.startsWith('target/'))?.slice('target/'.length)
    if (!target) continue
    if (!targets.has(target)) throw new Error(`${repo}#${issue.number} has target/${target}, which src/roadmap.json doesn't list`)
    const priority = Number(labels.find((l) => /^priority\/P\d$/.test(l))?.slice(-1) ?? 3)
    const item = { repo, number: issue.number, title: issue.title, target, priority }
    if (issue.state === 'closed') {
      item.done = true
      item.closed = issue.closed_at?.slice(0, 10)
    }
    items.push(item)
  }
  return items
}

/** Open items by priority, then done items, newest first. */
export function sortItems(items) {
  return items.sort(
    (a, b) =>
      Number(Boolean(a.done)) - Number(Boolean(b.done)) ||
      (a.done ? (b.closed ?? '').localeCompare(a.closed ?? '') : a.priority - b.priority) ||
      a.repo.localeCompare(b.repo) ||
      a.number - b.number,
  )
}

/** Whether two lists of items say the same thing. */
export function sameItems(a, b) {
  return JSON.stringify(a ?? []) === JSON.stringify(b ?? [])
}
