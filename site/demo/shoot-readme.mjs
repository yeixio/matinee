// Takes the README's screenshots (docs/images/readme) from a Matinee server
// running on a library of public-domain and Creative Commons films,
// cartoons, sheet music and books (public-catalog.mjs), with their artwork
// from Wikimedia Commons:
//
//   cd site && pnpm install && pnpm shots:readme       # every screenshot
//   pnpm shots:readme home movie                       # just these
//
// Needs Go, FFmpeg and Playwright's Chromium, like pnpm shots. The phone
// and TV app screenshots also need the apps' web builds: in a checkout of
// yeixio/matinee-apps next to this one, run `pnpm build:web --clear` and
// `pnpm build:tv-web --clear` in mobile/ first, or set MATINEE_APPS to
// that mobile folder. Without them, those screenshots are skipped.

process.env.MATINEE_DEMO_LIBRARY = 'public'

import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright'
import { capture, desktop, sleep } from './capture.mjs'

const { addLibraries, admin, api, origin, setUp, startServer } = await import('./server.mjs')

const dir = path.resolve(import.meta.dirname, '../../docs/images/readme')
const only = new Set(process.argv.slice(2))
const { media, stop } = await startServer()
const browser = await chromium.launch()
const { open, shot, wanted } = capture(browser, { origin, only, paths: media, dir })

// Titles such as "Dr." and "Jr." have dots that search reads as spaces, so match on letters and digits.
const norm = (s) => s.toLowerCase().replace(/[^a-z0-9]/g, '')
async function find(title, kinds = ['movie', 'show']) {
  const found = (await api(token, 'GET', `/search?q=${encodeURIComponent(title.replace(/[^\w\s]/g, ' '))}`)).find((i) => norm(i.title) === norm(title) && kinds.includes(i.kind))
  if (!found) throw new Error(`${title} isn't in the library`)
  return found
}
const children = async (id) => {
  const r = await api(token, 'GET', `/items/${id}/children`)
  return r.items ?? r
}

let token
const servers = []
try {
  token = await setUp()
  const libs = await addLibraries(token, media)

  // Something half-watched, a few watched and a watch list.
  const felix = await find('Felix the Cat')
  const [s1] = await children(felix.id)
  const eps = await children(s1.id)
  for (const e of eps.slice(0, 3)) await api(token, 'PUT', `/items/${e.id}/progress`, { positionMs: 0, played: true })
  await api(token, 'PUT', `/items/${eps[3].id}/progress`, { positionMs: 3 * 60000, played: false })
  const metropolis = await find('Metropolis')
  await api(token, 'PUT', `/items/${metropolis.id}/progress`, { positionMs: 61 * 60000, played: false })
  const charade = await find('Charade')
  await api(token, 'PUT', `/items/${charade.id}/progress`, { positionMs: 38 * 60000, played: false })
  for (const t of ['Safety Last!', 'Big Buck Bunny', 'Sintel']) await api(token, 'PUT', `/items/${(await find(t)).id}/progress`, { positionMs: 0, played: true })
  for (const t of ['His Girl Friday', 'Detour', 'The Cabinet of Dr. Caligari']) await api(token, 'PUT', `/watchlist/${(await find(t)).id}`)

  {
    const page = await open(desktop, token)
    await page.goto(origin)
    await shot(page, 'home')

    await page.goto(`${origin}/library/${libs.movies.id}`)
    await shot(page, 'library')
    await page.getByRole('button', { name: 'Filters', exact: true }).click()
    await sleep(400)
    await page.getByRole('dialog').getByRole('button', { name: 'Comedy' }).click().catch(() => {})
    await shot(page, 'filters')
    await page.keyboard.press('Escape')

    await page.goto(`${origin}/item/${charade.id}`)
    await shot(page, 'movie')

    // Up next: an episode just before its end.
    if (wanted('up-next')) {
      await page.goto(`${origin}/watch/${eps[3].id}`)
      await sleep(2500)
      const seek = page.getByRole('slider', { name: 'Seek' })
      const box = await seek.boundingBox()
      const total = eps[3].durationMs / 1000
      await page.mouse.click(box.x + box.width * (1 - 20 / total), box.y + box.height / 2)
      await page.getByText('Up next').waitFor({ timeout: 20000 })
      // Hold it there, or the next episode starts while the page settles.
      await sleep(1500)
      await page.evaluate(() => document.querySelector('video')?.pause())
      await page.mouse.move(700, 400)
      await shot(page, 'up-next')
    }
    await page.context().close()
  }

  // The phone and TV apps, from their web builds, signed in to this server.
  const apps = process.env.MATINEE_APPS ?? path.resolve(import.meta.dirname, '../../../matinee-apps/mobile')
  const builds = [['dist-web', 8491], ['dist-tv', 8492]]
  if (!builds.every(([b]) => existsSync(path.join(apps, b, 'index.html')))) {
    console.log(`No app web builds in ${apps}; skipping the app screenshots.`)
  } else if (['app-phone-home', 'app-phone-details', 'app-tv-home'].some(wanted)) {
    for (const [b, port] of builds) servers.push(spawn('node', ['tests/tv/serve.mjs', b, String(port)], { cwd: apps, stdio: 'ignore' }))
    await sleep(1000)
    const me = await api(token, 'GET', '/me')
    const app = async (port, options) => {
      const ctx = await browser.newContext({ ...options, colorScheme: 'dark', locale: 'en-US', reducedMotion: 'reduce' })
      await ctx.addInitScript(([server, token, user]) => {
        localStorage.setItem('matinee.session', JSON.stringify({ current: server, servers: [{ url: server, name: 'Home theater', token, user }] }))
      }, [origin, token, { id: me.id, username: me.username, isAdmin: true }])
      const page = await ctx.newPage()
      const go = async (p) => {
        await page.goto(`http://localhost:${port}${p}`)
        await page.waitForLoadState('networkidle').catch(() => {})
        await sleep(2500)
      }
      return { page, go }
    }
    const phoneApp = await app(8491, { viewport: { width: 360, height: 640 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true })
    await phoneApp.go('/')
    await shot(phoneApp.page, 'app-phone-home')
    await phoneApp.go(`/item/${charade.id}`)
    await shot(phoneApp.page, 'app-phone-details')
    await phoneApp.page.context().close()
    const tvApp = await app(8492, { viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 })
    await tvApp.go('/')
    await shot(tvApp.page, 'app-tv-home')
    await tvApp.page.context().close()
  }
} finally {
  for (const s of servers) s.kill()
  await browser.close()
  stop()
}
console.log(`Done. The README's screenshots are in ${path.relative(process.cwd(), dir)}; the server's admin is ${admin.username}.`)
