// Retakes the guide's watching screenshots on a real Matinee server and its
// library, from a computer on the same home network:
//
//   cd site && pnpm install && pnpm shots:library http://192.168.1.20:7171
//   pnpm shots:library http://192.168.1.20:7171 movie show    # just these
//
// It browses as the server's Home guest (no sign-in), the way any device at
// home can, so it only looks: every request that would change something on
// the server (other than starting a video) is answered here instead of being
// sent. The address must be one
// that counts as home: the server's own address on the network, or the
// home port (7172) when it runs behind an ingress. Settings, profiles and
// first-run screenshots come from the demo library (pnpm shots).

import { copyFileSync } from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright'
import sharp from 'sharp'
import { capture, desktop, out, phone, sleep } from './capture.mjs'

const [server, ...names] = process.argv.slice(2)
if (!server) {
  console.error('Usage: pnpm shots:library <server address, such as http://192.168.1.20:7171> [shot names]')
  process.exit(2)
}
const origin = new URL(server).origin

async function get(p) {
  const res = await fetch(`${origin}/api/v1${p}`)
  if (!res.ok) throw new Error(`GET ${p}: ${res.status} ${await res.text()}`)
  return res.json()
}
const list = (r) => r.items ?? r

const me = await get('/me').catch((e) => {
  console.error(`${origin} didn't let this computer in without signing in, so it doesn't count as home.\n${e.message}`)
  process.exit(1)
})
console.log(`Browsing ${origin} as ${me.username ?? 'the Home guest'}`)

// --- Pick what to show from the library ---
const libraries = await get('/libraries')
const lib = (kind) => libraries.find((l) => l.kind === kind)
const tags = (i) => i.mediaTags ?? []
const best = (items, ...prefer) => {
  const pool = items.filter((i) => i.hasBackdrop && i.overview)
  for (const ok of prefer) {
    const found = pool.filter(ok).sort((a, b) => (b.rating ?? 0) - (a.rating ?? 0))
    if (found.length) return found[0]
  }
  return pool.sort((a, b) => (b.rating ?? 0) - (a.rating ?? 0))[0] ?? items[0]
}

const movies = lib('movies') ? list(await get(`/libraries/${lib('movies').id}/items`)) : []
const shows = lib('tv') ? list(await get(`/libraries/${lib('tv').id}/items`)) : []
// A 4K HDR film shows off the badges and File details best.
const movie = best(movies, (m) => tags(m).includes('4k') && tags(m).some((t) => t.startsWith('hdr') || t === 'dolby-vision'), (m) => tags(m).includes('4k'))
const movieDetail = movie && (await get(`/items/${movie.id}`))
// A film with subtitles for the player, and one in a box set.
// H.264 plays as it is, so the player shot doesn't wait on a conversion.
let playable = movieDetail?.subtitles?.length && movie.videoCodec === 'h264' ? movie : undefined
let subtitled = movieDetail?.subtitles?.length ? movie : undefined
let boxSet = movieDetail?.details?.collection
for (const m of movies.filter((m) => m.hasBackdrop).slice(0, 40)) {
  if (playable && boxSet) break
  const d = await get(`/items/${m.id}`)
  if (!playable && d.subtitles?.length && m.videoCodec === 'h264') playable = m
  if (!subtitled && d.subtitles?.length) subtitled = m
  if (!boxSet && d.details?.collection) boxSet = d.details.collection
}
playable ??= subtitled ?? movie
const person = movieDetail?.credits?.cast?.find((c) => c.hasPhoto)
const show = best(shows, (s) => (s.childCount ?? 0) >= 2)
const seasons = show ? list(await get(`/items/${show.id}/children`)).filter((s) => (s.index ?? 1) > 0) : []
const episodes = seasons[0] ? list(await get(`/items/${seasons[0].id}/children`)) : []
const artists = lib('music') ? list(await get(`/libraries/${lib('music').id}/items`)) : []
const artist = artists.find((a) => a.hasPoster) ?? artists[0]
const album = artist && list(await get(`/items/${artist.id}/children`))[0]
// A word that finds a few things.
const word = (movie?.title ?? 'the')
  .split(/\W+/)
  .find((w) => w.length >= 4 && !['the', 'and', 'with', 'from'].includes(w.toLowerCase()))
const watchList = list(await get('/watchlist').catch(() => []))
const discover = (await get('/requests/options').catch(() => ({}))).discover

console.log({ movie: movie?.title, playable: playable?.title, boxSet: boxSet?.name, person: person?.name, show: show?.title, album: album?.title, word })

// --- Shoot ---
const browser = await chromium.launch()
const { open: openContext, shot } = capture(browser, { origin, only: new Set(names) })

async function open(options) {
  const page = await openContext(options)
  // Look, don't touch: answer anything that would change the server here.
  // Starting a stream (…/hls) only starts a conversion, so it goes through.
  await page.route('**/api/v1/**', (route) => {
    const r = route.request()
    return r.method() === 'GET' || new URL(r.url()).pathname.endsWith('/hls') ? route.continue() : route.fulfill({ status: 204 })
  })
  await page.goto(origin)
  // A Home account with profiles asks who's watching first.
  if (await page.getByText("Who's watching?").isVisible({ timeout: 3000 }).catch(() => false)) {
    await page.locator('main button').first().click()
    await sleep(500)
  }
  return page
}

/** Waits for the video to play from where it is (a seek restarts a conversion). */
const playing = (page) =>
  page
    .waitForFunction(() => {
      const v = document.querySelector('video')
      return v && v.readyState >= 3 && !v.seeking && !v.paused
    }, null, { timeout: 45000 })
    .catch(() => console.log('the video took a long time to start'))

try {
  {
    const page = await open(desktop)
    await page.goto(origin)
    await shot(page, 'home')
    await page.mouse.wheel(0, 700)
    await shot(page, 'home-rows')

    if (lib('movies')) {
      await page.goto(`${origin}/library/${lib('movies').id}`)
      await shot(page, 'library')
      await page.getByRole('button', { name: 'Filters', exact: true }).click()
      await sleep(400)
      const genre = movie?.genres?.[0]
      if (genre) await page.getByRole('dialog').getByRole('button', { name: genre, exact: true }).first().click().catch(() => {})
      await shot(page, 'filters')
      await page.keyboard.press('Escape')
    }

    if (word) {
      await page.goto(`${origin}/search`)
      await page.getByRole('searchbox').or(page.getByRole('textbox')).first().fill(word)
      await shot(page, 'search')
    }

    if (movie) {
      await page.goto(`${origin}/item/${movie.id}`)
      await shot(page, 'movie')
      await page.getByRole('heading', { name: 'Cast' }).scrollIntoViewIfNeeded().catch(() => {})
      await page.mouse.wheel(0, 500)
      await shot(page, 'movie-more')
      await page.mouse.wheel(0, -5000)
      await page.getByRole('button', { name: 'More details' }).click()
      await shot(page, 'more-details', { target: page.getByRole('dialog') })
      await page.keyboard.press('Escape')
      await page.getByRole('button', { name: 'File details' }).click()
      await shot(page, 'file-details', { target: page.getByRole('dialog') })
      await page.keyboard.press('Escape')
    }
    if (person) {
      await page.goto(`${origin}/person/${person.personId}`)
      await shot(page, 'person')
    }
    if (show) {
      await page.goto(`${origin}/item/${show.id}`)
      await shot(page, 'show')
      await page.goto(`${origin}/item/${seasons[0].id}`)
      await shot(page, 'season')
    }
    if (watchList.length) {
      await page.goto(`${origin}/watchlist`)
      await shot(page, 'watch-list')
    }
    await page.goto(`${origin}/collections`)
    await shot(page, 'collections')
    if (boxSet) {
      await page.goto(`${origin}/box-set/${boxSet.id}`)
      await shot(page, 'box-set')
    }
    if (lib('music')) {
      await page.goto(`${origin}/library/${lib('music').id}`)
      await shot(page, 'music')
    }
    if (album) {
      await page.goto(`${origin}/item/${album.id}`)
      await page.getByRole('button', { name: 'Play' }).first().click()
      await sleep(1500)
      await shot(page, 'album')
    }
    if (discover) {
      await page.goto(`${origin}/discover`)
      await page.getByRole('heading', { name: 'Trending' }).waitFor().catch(() => {})
      await shot(page, 'discover')
      if (word) {
        await page.getByPlaceholder('Find any movie or show…').fill(word)
        await sleep(1500)
        await shot(page, 'discover-search')
      }
    }
    await page.context().close()
  }

  // --- The player ---
  if (playable) {
    const page = await open(desktop)
    await page.goto(`${origin}/watch/${playable.id}`)
    await sleep(3000)
    const seek = page.getByRole('slider', { name: 'Seek' })
    const box = await seek.boundingBox()
    await page.mouse.click(box.x + box.width * 0.35, box.y + box.height / 2)
    await playing(page)
    await sleep(1000)
    await page.mouse.move(700, 500)
    await page.mouse.move(720, 520)
    await shot(page, 'player')
    await page.getByRole('button', { name: 'Subtitles' }).click()
    await shot(page, 'player-subtitles')
    await page.context().close()
  }
  if (episodes.length >= 2) {
    const page = await open(desktop)
    await page.goto(`${origin}/watch/${episodes[0].id}`)
    await sleep(3000)
    const seek = page.getByRole('slider', { name: 'Seek' })
    const box = await seek.boundingBox()
    const total = episodes[0].durationMs / 1000
    await page.mouse.click(box.x + box.width * (1 - 20 / total), box.y + box.height / 2)
    await playing(page)
    if (await page.getByRole('button', { name: 'Play now' }).waitFor({ timeout: 30000 }).then(() => true, () => false)) {
      // The countdown waits while the video is paused, so it's still there for the shot.
      await page.evaluate(() => document.querySelector('video')?.pause())
      await sleep(500)
      await shot(page, 'up-next')
    } else console.log('no Up next card; skipped up-next')
    await page.context().close()
  }

  // --- On a phone ---
  {
    const page = await open(phone)
    await page.goto(origin)
    await shot(page, 'phone-home', { maxWidth: 1170 })
    if (movie) {
      await page.goto(`${origin}/item/${movie.id}`)
      await shot(page, 'phone-movie', { maxWidth: 1170 })
    }
    await page.context().close()
  }
} finally {
  await browser.close()
}

// The landing page shows some of the same screens.
const assets = path.resolve(import.meta.dirname, '../src/assets')
for (const name of ['home', 'movie', 'up-next', 'filters', 'collections', 'album']) {
  if (!names.length || names.includes(name)) copyFileSync(path.join(out, `${name}.webp`), path.join(assets, `${name}.webp`))
}
if (!names.length || names.includes('file-details')) {
  const file = path.join(out, 'file-details.webp')
  const { width, height } = await sharp(file).metadata()
  await sharp(file)
    .extract({ left: 0, top: 0, width, height: Math.min(height, Math.round(width * 0.76)) })
    .toFile(path.join(assets, 'file-details.webp'))
}
console.log('done; check the shots in docs/guide/images, then commit them')
