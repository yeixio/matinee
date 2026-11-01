// A stand-in OpenSubtitles API, for the guide's subtitle search
// screenshots. Whatever the video, it finds a few made-up files in the
// language asked for, one made for the file itself, and serves one short
// subtitle file for any of them.

import http from 'node:http'

const names = { en: 'English', fr: 'French', es: 'Spanish', de: 'German', 'pt-br': 'Portuguese (Brazil)' }

function results(lang, hashOnly) {
  const l = (lang || 'en').split(',')[0]
  const all = [
    { id: 1, release: 'Harbor.Lights.2019.1080p.BluRay', downloads: 4210, match: true },
    { id: 2, release: 'Harbor.Lights.2019.720p.WEB', downloads: 9120, sdh: true },
    { id: 3, release: 'Harbor.Lights.2019.DVDRip', downloads: 1385 },
    { id: 4, release: 'Harbor.Lights.2019.2160p.WEB', downloads: 640, machine: true },
  ]
  return (hashOnly ? all.filter((r) => r.match) : all).map((r) => ({
    id: String(100 + r.id),
    type: 'subtitle',
    attributes: {
      language: l, download_count: r.downloads, hearing_impaired: !!r.sdh, foreign_parts_only: false,
      machine_translated: !!r.machine, ai_translated: false, from_trusted: !!r.match, moviehash_match: !!r.match,
      release: r.release, files: [{ file_id: 7000 + r.id, file_name: `${r.release}.${names[l] ?? l}.srt` }],
    },
  }))
}

const srt = `1
00:00:01,000 --> 00:00:04,000
The harbor lights came on one by one.

2
00:00:05,000 --> 00:00:08,000
I always thought they were waiting for someone.
`

export function startFakeOpenSubtitles(port) {
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, `http://127.0.0.1:${port}`)
    const json = (v) => {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(v))
    }
    if (url.pathname === '/api/v1/infos/formats') return json({ data: { output_formats: ['srt', 'webvtt'] } })
    if (url.pathname === '/api/v1/subtitles') {
      const data = results(url.searchParams.get('languages'), url.searchParams.has('moviehash'))
      return json({ total_count: data.length, total_pages: 1, page: 1, data })
    }
    if (url.pathname === '/api/v1/download') return json({ link: `http://127.0.0.1:${port}/file.srt`, file_name: 'file.srt', requests: 1, remaining: 19 })
    if (url.pathname === '/file.srt') {
      res.writeHead(200, { 'Content-Type': 'application/x-subrip' })
      return res.end(srt)
    }
    res.writeHead(404)
    res.end()
  })
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)))
}
