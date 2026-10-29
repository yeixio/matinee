// Runs a Matinee server on the demo library for the docs' screenshots:
// startServer builds it pointed at the TMDB stand-in and starts it with a
// fresh data folder; setUp makes the admin and the libraries, and waits for
// the scan, the metadata and the media analysis to finish.

import { execFileSync, spawn } from 'node:child_process'
import { mkdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import { startFakeOverseerr } from './fake-overseerr.mjs'
import { startFakeTMDB } from './fake-tmdb.mjs'
import { makeLibrary } from './library.mjs'

const repo = path.resolve(import.meta.dirname, '../..')
export const cache = path.join(import.meta.dirname, '.cache')
const tmdbPort = 7399
/** Where the stand-in Overseerr listens, for Settings → Overseerr. */
export const overseerrURL = 'http://127.0.0.1:7400'
export const port = 7398
export const origin = `http://127.0.0.1:${port}`
export const admin = { username: 'sam', password: 'popcorn-and-ice' }

// The server's version, as Settings shows it: the release tag, or main.
const version = () => {
  try {
    return execFileSync('git', ['describe', '--tags', '--exact-match'], { cwd: repo, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim()
  } catch {
    return 'main'
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

export async function api(token, method, p, body) {
  const res = await fetch(origin + '/api/v1' + p, {
    method,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  if (!res.ok) throw new Error(`${method} ${p}: ${res.status} ${text}`)
  return text ? JSON.parse(text) : null
}

export async function startServer() {
  const { media, images } = await makeLibrary(cache)
  const fake = await startFakeTMDB(tmdbPort, images)
  const overseerr = await startFakeOverseerr(Number(new URL(overseerrURL).port))

  const bin = path.join(cache, 'matinee')
  const pkg = 'github.com/yeixio/matinee/internal/metadata'
  // The server embeds the web player, so build it from this checkout first.
  execFileSync('pnpm', ['install', '--frozen-lockfile'], { cwd: path.join(repo, 'web'), stdio: 'inherit' })
  execFileSync('pnpm', ['build'], { cwd: path.join(repo, 'web'), stdio: 'inherit' })
  execFileSync('go', ['build', '-o', bin, '-ldflags', `-X github.com/yeixio/matinee/internal/version.Version=${version()} -X ${pkg}.TMDBBaseURL=http://127.0.0.1:${tmdbPort}/3 -X ${pkg}.TMDBImageURL=http://127.0.0.1:${tmdbPort}/t/p/ -X github.com/yeixio/matinee/internal/overseerr.imageBase=http://127.0.0.1:${tmdbPort}/t/p/`, './cmd/matinee'], { cwd: repo, stdio: 'inherit' })

  const data = path.join(cache, 'data')
  rmSync(data, { recursive: true, force: true })
  mkdirSync(data, { recursive: true })
  const proc = spawn(bin, [], {
    env: {
      ...process.env,
      MATINEE_ADDR: `127.0.0.1:${port}`,
      MATINEE_DATA_DIR: data,
      MATINEE_TMDB_KEY: 'demo0000000000000000000000007c3e', // a made-up key, as long as a real one
      MATINEE_WIKIDATA: 'false',
      MATINEE_MUSICBRAINZ: 'false',
      MATINEE_OPENLIBRARY: 'false',
      MATINEE_DISCOVERY: 'false',
      MATINEE_SCAN_INTERVAL: '0',
      MATINEE_MEDIA_ROOT: media,
      MATINEE_SERVER_NAME: 'Home theater',
      MATINEE_PLUGIN_CATALOG: `http://127.0.0.1:${tmdbPort}/plugins.json`,
    },
    stdio: ['ignore', 'ignore', process.env.DEBUG ? 'inherit' : 'ignore'],
  })
  const stop = () => {
    proc.kill()
    fake.close()
    overseerr.close()
  }
  for (let i = 0; ; i++) {
    try {
      if ((await fetch(origin + '/healthz')).ok) break
    } catch {}
    if (i > 100) throw new Error('server did not start')
    await sleep(100)
  }

  return { media, stop }
}

/** Makes the admin account; resolves to its token. */
export async function setUp() {
  const { token } = await api(null, 'POST', '/setup', admin)
  // Analyze files as fast as possible instead of gently in the background.
  await api(token, 'PUT', '/analysis', { mode: 'normal' })
  return token
}

export const libraries = [
  { name: 'Movies', kind: 'movies', dir: 'Movies' },
  { name: 'TV Shows', kind: 'tv', dir: 'TV' },
  { name: 'Music', kind: 'music', dir: 'Music' },
  { name: 'Audiobooks', kind: 'audiobooks', dir: 'Audiobooks' },
]

/** Adds the libraries not there yet, then waits for their details and the media analysis. */
export async function addLibraries(token, media) {
  const have = await api(token, 'GET', '/libraries')
  const libs = {}
  for (const l of libraries) {
    libs[l.kind] = have.find((h) => h.kind === l.kind) ?? (await api(token, 'POST', '/libraries', { name: l.name, kind: l.kind, paths: [path.join(media, l.dir)] }))
  }
  for (let i = 0; ; i++) {
    const a = await api(token, 'GET', '/analysis')
    const movies = await api(token, 'GET', `/libraries/${libs.movies.id}/items`)
    const list = movies.items ?? movies
    const described = list.length > 0 && list.every((m) => m.overview)
    if (described && a.total > 0 && a.done === a.total) break
    if (i % 20 === 19) console.log('waiting for the library:', JSON.stringify(a), `${list.filter((m) => m.overview).length}/${list.length} described`)
    if (i > 600) throw new Error(`library not ready: ${JSON.stringify(a)}`)
    await sleep(500)
  }
  return libs
}
