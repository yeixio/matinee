// A stand-in podcast host and Podcast Index, for the guide's podcast
// screenshots. It serves an RSS feed for each made-up show in catalog.mjs,
// their covers, one quiet hour of audio for every episode, and answers
// Podcast Index's search and trending requests with the same shows. It's
// also the radio stations' host and directory (Radio Browser's search),
// with live streams that say what's playing, and live TV's: iptv-org's
// lists of made-up channels, their guide, and one clip played live.

import { createReadStream, readFileSync, statSync } from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { channels, onAir, podcasts, stations } from './catalog.mjs'

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

const stationSlug = (i) => `station-${i}`

/** A station as Radio Browser lists it. */
const stationEntry = (base, st, i) => ({
  name: st.name, url: `${base}/radio/${stationSlug(i)}.mp3`, url_resolved: `${base}/radio/${stationSlug(i)}.mp3`, homepage: `${base}/${stationSlug(i)}`,
  favicon: `${base}/art/${stationSlug(i)}.jpg`, tags: st.tags.join(','), countrycode: st.country, codec: 'MP3', bitrate: 64, hls: 0, lastcheckok: 1,
})

// A live stream, as an Icecast server sends one: the MP3 at its own pace,
// with the song playing (StreamTitle) every metaint bytes when asked.
function radioStream(req, res, file, st, i) {
  const metaint = 8000
  const icy = req.headers['icy-metadata'] === '1'
  res.writeHead(200, { 'Content-Type': 'audio/mpeg', 'icy-name': st.name, 'icy-genre': st.tags[0], 'icy-br': '64', ...(icy ? { 'icy-metaint': String(metaint) } : {}) })
  const audio = readFileSync(file)
  let at = 0
  let sinceMeta = 0
  const meta = () => {
    const title = onAir[(i + Math.floor(Date.now() / 60000)) % onAir.length]
    const text = Buffer.from(`StreamTitle='${title}';`)
    const n = Math.ceil(text.length / 16)
    const block = Buffer.alloc(1 + n * 16)
    block[0] = n
    text.copy(block, 1)
    return block
  }
  // 64 kbit/s is 8000 bytes a second; a little extra keeps the player fed.
  const tick = setInterval(() => {
    let want = 2400
    while (want > 0) {
      const n = Math.min(want, audio.length - at, icy ? metaint - sinceMeta : Infinity)
      res.write(audio.subarray(at, at + n))
      at = (at + n) % audio.length
      want -= n
      sinceMeta += n
      if (icy && sinceMeta === metaint) {
        res.write(meta())
        sinceMeta = 0
      }
    }
  }, 250)
  const done = () => clearInterval(tick)
  req.on('close', done)
  res.on('error', done)
}


// Live TV. iptv-org's lists, by country, category and language.
const channelEntry = (base, c, i) => `#EXTINF:-1 tvg-id="ch${i}.demo" tvg-logo="${base}/art/channel-${i}.jpg" group-title="${c.categories.map((x) => x[0].toUpperCase() + x.slice(1)).join(';')}",${c.name}\n${base}/tv/channel-${i}/index.m3u8\n`
const iptvList = (base, keep) => '#EXTM3U\n' + channels.map((c, i) => (keep(c) ? channelEntry(base, c, i) : '')).join('')

// The guide: each channel's shows back to back from the start of the
// hour before now, an hour or half an hour each, for a day.
function guide() {
  const fmt = (ms) => new Date(ms).toISOString().replace(/[-:T]/g, '').slice(0, 14) + ' +0000'
  const start = Math.floor(Date.now() / 3600000) * 3600000 - 3600000
  const out = ['<?xml version="1.0" encoding="UTF-8"?>', '<tv>']
  channels.forEach((c, i) => out.push(`<channel id="ch${i}.demo"><display-name>${esc(c.name)}</display-name></channel>`))
  channels.forEach((c, i) => {
    let at = start - (i % 3) * 900000
    for (let n = 0; at < start + 86400000; n++) {
      const len = (n + i) % 3 === 0 ? 1800000 : 3600000
      const title = c.shows[n % c.shows.length]
      out.push(`<programme start="${fmt(at)}" stop="${fmt(at + len)}" channel="ch${i}.demo"><title>${esc(title)}</title><desc>${esc(`${title}, live on ${c.name}.`)}</desc></programme>`)
      at += len
    }
  })
  out.push('</tv>')
  return out.join('\n')
}

// A live stream: a window of four pieces that moves along every four
// seconds, round and round the clip's six.
function liveStream(res, base) {
  const n = Math.floor(Date.now() / 4000)
  const lines = ['#EXTM3U', '#EXT-X-VERSION:3', '#EXT-X-TARGETDURATION:4', `#EXT-X-MEDIA-SEQUENCE:${n}`, `#EXT-X-DISCONTINUITY-SEQUENCE:${Math.floor(n / 6)}`]
  for (let k = n; k < n + 4; k++) {
    if (k > n && k % 6 === 0) lines.push('#EXT-X-DISCONTINUITY')
    lines.push('#EXTINF:4.000,', `${base}/seg${k % 6}.ts`)
  }
  res.writeHead(200, { 'Content-Type': 'application/vnd.apple.mpegurl', 'Cache-Control': 'no-cache' })
  res.end(lines.join('\n') + '\n')
}

/** Starts the stand-in; episode is the audio file every episode plays, radio the one every station plays. */
export function startFakePodcasts(port, images, episode, radio, live) {
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
    // The station directory (Radio Browser), its logos and its streams.
    if ((m = /^\/art\/station-(\d+)\.jpg$/.exec(p)) && stations[m[1]]) return sendFile(req, res, path.join(images, `station-${m[1]}.jpg`), 'image/jpeg')
    if ((m = /^\/radio\/station-(\d+)\.mp3$/.exec(p)) && stations[m[1]]) return radioStream(req, res, radio, stations[m[1]], Number(m[1]))
    if (p === '/json/stations/search') {
      const name = (url.searchParams.get('name') ?? '').toLowerCase()
      const tag = (url.searchParams.get('tag') ?? '').toLowerCase()
      const found = stations.map((st, i) => [st, i]).filter(([st]) => (name && st.name.toLowerCase().includes(name)) || (tag && st.tags.includes(tag)))
      return json(found.map(([st, i]) => stationEntry(base, st, i)))
    }
    // Live TV: iptv-org's choices and lists, the guide, logos and streams.
    if (p === '/iptv-org/api/countries.json')
      return json([['US', 'United States', '🇺🇸'], ['GB', 'United Kingdom', '🇬🇧'], ['CA', 'Canada', '🇨🇦'], ['MX', 'Mexico', '🇲🇽']].map(([code, name, flag]) => ({ code, name, flag })))
    if (p === '/iptv-org/api/languages.json') return json([{ code: 'eng', name: 'English' }, { code: 'spa', name: 'Spanish' }])
    if (p === '/iptv-org/api/categories.json')
      return json(['classic', 'cooking', 'documentary', 'kids', 'movies', 'news', 'sports', 'travel', 'weather'].map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1) })))
    if ((m = /^\/iptv-org\/iptv\/(countries|categories|languages)\/([a-z]+)\.m3u$/.exec(p))) {
      const [, kind, code] = m
      const keep = (c) => (kind === 'countries' ? c.country.toLowerCase() === code : kind === 'languages' ? c.language === code : c.categories.includes(code))
      res.writeHead(200, { 'Content-Type': 'audio/x-mpegurl' })
      return res.end(iptvList(base, keep))
    }
    if (p === '/iptv-org/iptv/index.m3u') {
      res.writeHead(200, { 'Content-Type': 'audio/x-mpegurl' })
      return res.end(iptvList(base, () => true))
    }
    if (p === '/tv/guide.xml') {
      res.writeHead(200, { 'Content-Type': 'application/xml' })
      return res.end(guide())
    }
    if ((m = /^\/art\/channel-(\d+)\.jpg$/.exec(p)) && channels[m[1]]) return sendFile(req, res, path.join(images, `channel-${m[1]}.jpg`), 'image/jpeg')
    if ((m = /^\/tv\/channel-(\d+)\/index\.m3u8$/.exec(p)) && channels[m[1]]) return liveStream(res, `${base}/tv/channel-${m[1]}`)
    if ((m = /^\/tv\/channel-\d+\/seg(\d)\.ts$/.exec(p))) return sendFile(req, res, path.join(live, `seg${m[1]}.ts`), 'video/mp2t')
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
/** A made-up station as the directory lists it, to add it as someone would. */
export const stationFound = (port, i) => stationEntry(`http://127.0.0.1:${port}`, stations[i], i)
export const guideURL = (port) => `http://127.0.0.1:${port}/tv/guide.xml`
