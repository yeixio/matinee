// A stand-in for Overseerr, for the guide's Discover screenshots. It answers
// the requests internal/overseerr makes with the made-up titles in
// catalog.mjs; their artwork comes from the fake TMDB's image server.

import http from 'node:http'
import { discover, movies, recommendations, shows } from './catalog.mjs'

const users = [
  { id: 1, displayName: 'sam', username: 'sam', email: 'sam@example.com' },
  { id: 2, displayName: 'alex', username: 'alex', email: 'alex@example.com' },
  { id: 3, displayName: 'Guests', username: 'guests', email: 'guests@example.com' },
]

// The demo library's own titles, so recommendations can include one.
const library = [...movies.map((m) => ({ ...m, kind: 'movie' })), ...shows.map((s) => ({ ...s, kind: 'tv' }))]
const byId = (id) => discover.find((t) => t.id === id) ?? library.find((t) => t.id === id)

function result(t) {
  const tv = t.kind === 'tv'
  return {
    id: t.id,
    mediaType: t.kind,
    [tv ? 'name' : 'title']: t.title,
    [tv ? 'firstAirDate' : 'releaseDate']: t.date,
    overview: t.overview,
    posterPath: `/poster-${t.id}.jpg`,
    backdropPath: `/backdrop-${t.id}.jpg`,
    voteAverage: t.vote,
    ...(t.status ? { mediaInfo: { status: t.status } } : {}),
  }
}

const page = (list) => ({ page: 1, totalPages: 1, totalResults: list.length, results: list.map(result) })
const onList = (name, kind) => discover.filter((t) => t.lists.includes(name) && (!kind || t.kind === kind))

function answer(method, p, q) {
  if (p === '/api/v1/status') return { version: '1.33.2' }
  if (p === '/api/v1/auth/me') return users[0]
  if (p === '/api/v1/user') return { results: users }
  if (p === '/api/v1/discover/trending') return page(onList('trending'))
  if (p === '/api/v1/discover/movies') return page(onList('popular-movies', 'movie'))
  if (p === '/api/v1/discover/tv') return page(onList('popular-shows', 'tv'))
  if (p === '/api/v1/discover/movies/upcoming') return page(onList('upcoming-movies'))
  if (p === '/api/v1/discover/tv/upcoming') return page(onList('upcoming-shows'))
  if (p === '/api/v1/search') {
    const words = (q.get('query') ?? '').toLowerCase()
    return page([...discover, ...library].filter((t) => t.title.toLowerCase().includes(words)))
  }
  if (method === 'POST' && p === '/api/v1/request') return { id: 1, status: 1 }
  let m
  if ((m = p.match(/^\/api\/v1\/(movie|tv)\/(\d+)\/recommendations$/))) {
    return page((recommendations[m[2]] ?? recommendations.default).map(byId))
  }
  if ((m = p.match(/^\/api\/v1\/(movie|tv)\/(\d+)$/))) {
    const t = byId(Number(m[2]))
    if (!t) return null
    const seasons = Array.from({ length: t.seasons?.length ?? t.seasons ?? 0 }, (_, i) => ({ seasonNumber: i + 1 }))
    return { ...result(t), seasons }
  }
  return null
}

/** Starts the stand-in on port and resolves once it listens. */
export function startFakeOverseerr(port) {
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://x')
    req.resume()
    req.on('end', () => {
      const body = answer(req.method, url.pathname, url.searchParams)
      if (!body) return res.writeHead(404, { 'Content-Type': 'application/json' }).end('{"message":"Not found"}')
      res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(body))
    })
  })
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)))
}
