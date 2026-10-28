// A stand-in for themoviedb.org that knows only the demo library
// (catalog.mjs), so the screenshot server gets posters, descriptions, cast
// and crew without a key or the internet. It answers the requests
// internal/metadata makes, and serves the artwork library.mjs rendered and
// the list of official plugins.

import { createReadStream, existsSync } from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { keywords, movies, people, shows } from './catalog.mjs'

const genreNames = {
  28: 'Action', 12: 'Adventure', 16: 'Animation', 35: 'Comedy', 80: 'Crime', 18: 'Drama', 10751: 'Family', 14: 'Fantasy',
  36: 'History', 9648: 'Mystery', 10749: 'Romance', 878: 'Science Fiction', 53: 'Thriller', 37: 'Western', 10762: 'Kids', 10765: 'Sci-Fi & Fantasy',
}
const genres = (ids) => ids.map((id) => ({ id, name: genreNames[id] }))
const person = (id) => people.find((p) => p.id === id)
const credit = (c) => ({ ...c, name: person(c.id).name, profile_path: `/person-${c.id}.jpg`, known_for_department: 'Acting' })
const crewOf = (list) => list.map(([id, job]) => ({ id, job, department: job === 'Director' ? 'Directing' : job === 'Screenplay' ? 'Writing' : 'Sound', name: person(id).name, profile_path: `/person-${id}.jpg` }))
const named = (list) => (list ?? []).map((name, i) => ({ id: i + 1, name }))

function searchResult(t, tv) {
  return {
    id: t.id, overview: t.overview, vote_average: t.vote, poster_path: `/poster-${t.id}.jpg`, backdrop_path: `/backdrop-${t.id}.jpg`,
    [tv ? 'first_air_date' : 'release_date']: t.date, genre_ids: t.genres,
  }
}

function movieDetails(m) {
  return {
    ...searchResult(m, false), title: m.title, original_title: m.title, original_language: 'en', tagline: m.tagline ?? '', status: 'Released',
    genres: genres(m.genres), production_companies: named(m.studios), production_countries: named(m.countries),
    spoken_languages: [{ iso_639_1: 'en', english_name: 'English', name: 'English' }],
    budget: m.budget ?? 0, revenue: m.revenue ?? 0, belongs_to_collection: m.collection ?? null, runtime: m.minutes,
    credits: { cast: m.cast.map(credit), crew: crewOf(m.crew) },
    release_dates: { results: [{ iso_3166_1: 'US', release_dates: [{ certification: m.cert, type: 3 }] }] },
    keywords: { keywords: keywords.filter((k) => m.keywords?.includes(k.id)) },
    recommendations: { results: movies.filter((o) => o.id !== m.id && o.genres.some((g) => m.genres.includes(g))).slice(0, 6).map((o) => ({ id: o.id, name: o.title })) },
    external_ids: {},
  }
}

function showDetails(s) {
  return {
    ...searchResult(s, true), name: s.title, original_name: s.title, original_language: 'en', status: s.status, last_air_date: s.ended ?? '',
    genres: genres(s.genres), networks: named(s.networks), production_countries: [],
    spoken_languages: [{ iso_639_1: 'en', english_name: 'English', name: 'English' }],
    number_of_seasons: s.seasons.length, number_of_episodes: s.seasons.flat().length,
    created_by: s.creators.map((id) => ({ id, name: person(id).name, profile_path: `/person-${id}.jpg` })),
    aggregate_credits: { cast: s.cast.map((c) => ({ ...credit(c), roles: [{ character: c.character, episode_count: s.seasons.flat().length }], total_episode_count: s.seasons.flat().length })), crew: [] },
    content_ratings: { results: [{ iso_3166_1: 'US', rating: s.cert }] },
    keywords: { results: [] },
    recommendations: { results: shows.filter((o) => o.id !== s.id).map((o) => ({ id: o.id, name: o.title })) },
    external_ids: {},
  }
}

function seasonDetails(s, n) {
  const eps = s.seasons[n - 1]
  if (!eps) return null
  const year = Number(s.date.slice(0, 4)) + n - 1
  return {
    id: s.id * 100 + n, overview: '', poster_path: `/poster-${s.id}-s${n}.jpg`, air_date: `${year}${s.date.slice(4)}`,
    episodes: eps.map((_, i) => ({ episode_number: i + 1, air_date: `${year}-${String(1 + Math.floor(i / 4)).padStart(2, '0')}-${String(1 + (i % 4) * 7).padStart(2, '0')}`, crew: crewOf([[s.creators[0], 'Director']]), guest_stars: [] })),
    aggregate_credits: { cast: s.cast.map((c) => ({ ...credit(c), roles: [{ character: c.character, episode_count: eps.length }], total_episode_count: eps.length })) },
  }
}

function episode(s, n, e) {
  const name = s.seasons[n - 1]?.[e - 1]
  if (!name) return null
  return { id: s.id * 10000 + n * 100 + e, name, overview: s.episodeOverviews[(e - 1 + (n - 1) * 2) % s.episodeOverviews.length], vote_average: 7 + ((e * 3) % 20) / 10, still_path: `/still-${s.id}-${n}-${e}.jpg` }
}

const norm = (s) => s.toLowerCase().replace(/[^a-z0-9]/g, '')

function answer(p, q) {
  let m
  if (p === '/3/search/movie') return { results: movies.filter((t) => norm(t.title) === norm(q.get('query') ?? '')).map((t) => searchResult(t, false)) }
  if (p === '/3/search/tv') return { results: shows.filter((t) => norm(t.title) === norm(q.get('query') ?? '')).map((t) => searchResult(t, true)) }
  if (p === '/3/search/keyword') return { results: keywords.filter((k) => k.name === (q.get('query') ?? '').toLowerCase()) }
  if ((m = p.match(/^\/3\/movie\/(\d+)\/videos$/)) || (m = p.match(/^\/3\/tv\/(\d+)(\/season\/\d+)?\/videos$/))) return { results: [] }
  if ((m = p.match(/^\/3\/movie\/(\d+)$/))) {
    const t = movies.find((x) => x.id === Number(m[1]))
    return t && movieDetails(t)
  }
  if ((m = p.match(/^\/3\/tv\/(\d+)$/))) {
    const t = shows.find((x) => x.id === Number(m[1]))
    return t && showDetails(t)
  }
  if ((m = p.match(/^\/3\/tv\/(\d+)\/season\/(\d+)$/))) {
    const t = shows.find((x) => x.id === Number(m[1]))
    return t && seasonDetails(t, Number(m[2]))
  }
  if ((m = p.match(/^\/3\/tv\/(\d+)\/season\/(\d+)\/episode\/(\d+)$/))) {
    const t = shows.find((x) => x.id === Number(m[1]))
    return t && episode(t, Number(m[2]), Number(m[3]))
  }
  if ((m = p.match(/^\/3\/person\/(\d+)$/))) {
    const x = person(Number(m[1]))
    return x && { biography: x.bio, birthday: x.born, place_of_birth: x.place, known_for_department: x.known ?? 'Acting', profile_path: `/person-${x.id}.jpg` }
  }
  return null
}

/** Starts the stand-in on port and resolves once it listens. */
export function startFakeTMDB(port, images) {
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://x')
    // Images: /t/p/<size>/<file>
    const img = url.pathname.match(/^\/t\/p\/[^/]+\/([\w.-]+\.jpg)$/)
    if (img) {
      const file = path.join(images, img[1])
      if (!existsSync(file)) return res.writeHead(404).end()
      res.writeHead(200, { 'Content-Type': 'image/jpeg' })
      return createReadStream(file).pipe(res)
    }
    // The official plugins, as the website lists them.
    if (url.pathname === '/plugins.json') {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      return createReadStream(path.resolve(import.meta.dirname, '../public/plugins.json')).pipe(res)
    }
    const body = answer(url.pathname, url.searchParams)
    if (!body) return res.writeHead(404, { 'Content-Type': 'application/json' }).end('{"status_code":34}')
    res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(body))
  })
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)))
}
