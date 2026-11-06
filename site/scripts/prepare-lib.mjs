// Version numbers: "v1.2.3" to [1, 2, 3], or [] for anything else.
export const semver = (v) => (v.match(/^v?(\d+)\.(\d+)\.(\d+)$/) ?? []).slice(1).map(Number)

export function compare(a, b) {
  const [x, y] = [semver(a), semver(b)]
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i]
  return 0
}

// The feature table (docs/features.json): which app has which feature.
const issueRef = /^(matinee|matinee-apps)#(\d+)$/

/**
 * featureProblems lists what's wrong with the feature table: an app left out
 * of a feature, a value that isn't true, an issue or a reason, a since that
 * isn't a version, or a link to a guide page that isn't there. hasPage says
 * whether a /docs/... path is a page of the guide.
 */
export function featureProblems(data, hasPage = () => true) {
  const problems = []
  for (const table of data.tables ?? []) {
    const ids = table.apps.map((a) => a.id)
    for (const group of table.groups ?? []) {
      for (const f of group.features ?? []) {
        const at = `${table.title} → ${f.name ?? '(no name)'}`
        if (!f.name) problems.push(`${at}: no name`)
        if (f.since && semver(f.since).length !== 3) problems.push(`${at}: since ${f.since} isn't a version like v0.3.0`)
        if (!f.doc || !hasPage(f.doc)) problems.push(`${at}: doc ${f.doc ?? '(none)'} isn't a guide page`)
        for (const id of ids) {
          const v = f.apps?.[id]
          if (v === undefined) problems.push(`${at}: says nothing about ${id}; use true, the issue that adds it ("matinee-apps#123"), or {"na": "why it doesn't need it"}`)
          else if (!(v === true || (typeof v === 'string' && issueRef.test(v)) || (typeof v?.na === 'string' && v.na.trim()))) problems.push(`${at}: ${id} is ${JSON.stringify(v)}`)
        }
        for (const id of Object.keys(f.apps ?? {})) if (!ids.includes(id)) problems.push(`${at}: ${id} isn't one of this table's apps`)
      }
    }
  }
  return problems
}

/** featureMarkdown is the feature table as the guide shows it: one table per group, with notes for what an app doesn't need. */
export function featureMarkdown(data) {
  const list = (names) => (names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`)
  const cell = (v) => {
    if (v === true) return '✓'
    if (typeof v === 'string') {
      const [, repo, n] = v.match(issueRef)
      // matinee-apps is private, so only the server's issues are linked.
      return repo === 'matinee' ? `[Planned](https://github.com/yeixio/matinee/issues/${n})` : 'Planned'
    }
    return '–'
  }
  const out = []
  for (const table of data.tables) {
    out.push(`## ${table.title}`, '')
    for (const group of table.groups) {
      out.push(`### ${group.title}`, '')
      out.push('<div class="feature-table">', '')
      out.push(`| | ${table.apps.map((a) => a.name).join(' | ')} |`, `|---|${table.apps.map(() => ':-:').join('|')}|`)
      const notes = []
      for (const f of group.features) {
        out.push(`| [${f.name}](${f.doc}) | ${table.apps.map((a) => cell(f.apps[a.id])).join(' | ')} |`)
        // Apps that don't need it for the same reason share a line.
        const why = new Map()
        for (const a of table.apps) if (f.apps[a.id]?.na) why.set(f.apps[a.id].na, [...(why.get(f.apps[a.id].na) ?? []), a.name])
        for (const [reason, names] of why) notes.push(`- **${f.name}** on ${list(names)}: ${reason}`)
      }
      out.push('', '</div>', '')
      if (notes.length) out.push('Not needed (–):', '', ...notes, '')
    }
  }
  return out.join('\n')
}
