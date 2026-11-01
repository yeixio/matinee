// A stand-in podcast host and Podcast Index, for the guide's podcast
// screenshots. It serves an RSS feed for each made-up show in catalog.mjs,
// their covers, one quiet hour of audio for every episode, and answers
// Podcast Index's search and trending requests with the same shows.

import { createReadStream, statSync } from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { podcasts } from './catalog.mjs'

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const slug = (i) => `show-${i}`

function feed(base, p, i) {
  const day = 86400000
  const items = p.episodes.map(([title, minutes, daysAgo, notes, type], n) => `
    <item>
      <title>${esc(title)}</title>
      <guid isPermaLink="false">${slug(i)}-${p.episodes.length - n}</guid>
      <pubDate>${new Date(Date.now() - daysAgo * day).toUTCString()}</pubDate>
      <description>${esc(notes)}</description>
      <itunes:duration>${minutes * 60}</itunes:duration>
      ${type ? `<itunes:episodeType>${type}</itunes:episodeType>` : ''}
      <enclosure url="${base}/audio/${slug(i)}-${n}.m4a" length="7200000" type="audio/mp4"/>
    </item>`)
  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel>
    <title>${esc(p.title)}</title>
    <link>${base}/${slug(i)}</link>
    <description>${esc(p.description)}</description>
    <language>en</language>
    <itunes:author>${esc(p.author)}</itunes:author>
    <itunes:image href="${base}/art/${slug(i)}.jpg"/>
    ${items.join('')}
  </channel>
</rss>`
}

function directoryEntry(base, p, i) {
  return {
    id: 9000 + i, title: p.title, url: `${base}/feeds/${slug(i)}.xml`, link: `${base}/${slug(i)}`, description: p.description,
    author: p.author, artwork: `${base}/art/${slug(i)}.jpg`, language: 'en', episodeCount: p.episodes.length, dead: 0,
    categories: Object.fromEntries(p.categories.map((c, n) => [String(n + 1), c])),
  }
}

/** Serves a file, with the Range requests a player seeks with. */
function sendFile(req, res, file, type) {
  const size = statSync(file).size
  const m = /bytes=(\d*)-(\d*)/.exec(req.headers.range ?? '')
  if (!m) {
    res.writeHead(200, { 'Content-Type': type, 'Content-Length': size, 'Accept-Ranges': 'bytes' })
    return createReadStream(file).pipe(res)
  }
  const start = m[1] ? Number(m[1]) : size - Number(m[2])
  const end = m[1] && m[2] ? Math.min(Number(m[2]), size - 1) : size - 1
  res.writeHead(206, { 'Content-Type': type, 'Content-Length': end - start + 1, 'Content-Range': `bytes ${start}-${end}/${size}`, 'Accept-Ranges': 'bytes' })
  createReadStream(file, { start, end }).pipe(res)
}

/** Starts the stand-in; episode is the audio file every episode plays. */
export function startFakePodcasts(port, images, episode) {
  const base = `http://127.0.0.1:${port}`
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, base)
    const p = url.pathname
    const json = (v) => {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(v))
    }
    let m
    if ((m = /^\/feeds\/show-(\d+)\.xml$/.exec(p)) && podcasts[m[1]]) {
      res.writeHead(200, { 'Content-Type': 'application/rss+xml' })
      return res.end(feed(base, podcasts[m[1]], Number(m[1])))
    }
    if ((m = /^\/art\/show-(\d+)\.jpg$/.exec(p)) && podcasts[m[1]]) return sendFile(req, res, path.join(images, `podcast-${m[1]}.jpg`), 'image/jpeg')
    if (p.startsWith('/audio/')) return sendFile(req, res, episode, 'audio/mp4')
    if (p === '/api/1.0/search/byterm') {
      const q = (url.searchParams.get('q') ?? '').toLowerCase()
      const found = podcasts.map((s, i) => [s, i]).filter(([s]) => `${s.title} ${s.author} ${s.categories.join(' ')} ${s.description}`.toLowerCase().includes(q))
      return json({ status: 'true', feeds: found.map(([s, i]) => directoryEntry(base, s, i)) })
    }
    if (p === '/api/1.0/podcasts/trending') {
      // cat is Podcast Index's category numbers; these are the ones the made-up shows use.
      const names = { 1: 'Arts', 16: 'Comedy', 26: 'Fiction', 28: 'History', 42: 'Leisure', 53: 'Music', 67: 'Science', 77: 'Society & Culture', 104: 'TV & Film', 105: 'TV & Film' }
      const want = (url.searchParams.get('cat') ?? '').split(',').filter(Boolean).map((n) => names[n] ?? '?')
      const found = podcasts.map((s, i) => [s, i]).filter(([s]) => !want.length || s.categories.some((c) => want.includes(c)))
      return json({ status: 'true', feeds: found.map(([s, i]) => directoryEntry(base, s, i)) })
    }
    res.writeHead(404)
    res.end()
  })
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)))
}

export const feedURL = (port, i) => `http://127.0.0.1:${port}/feeds/${slug(i)}.xml`
