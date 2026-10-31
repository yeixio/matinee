// Takes the user guide's screenshots (docs/guide/images) from a real Matinee
// server running on the demo library. Run it after a change to the web
// player that the guide shows:
//
//   cd site && pnpm install && pnpm shots            # every screenshot
//   pnpm shots home movie                            # just these
//
// Needs Go, FFmpeg and Playwright's Chromium. The first run makes the demo
// library in demo/.cache, which takes a few minutes.

import { chromium } from 'playwright'
import { cover } from './art.mjs'
import { capture, desktop, phone, sleep } from './capture.mjs'
import { feedURL } from './fake-podcasts.mjs'
import { addLibraries, admin, api, origin, overseerrURL, podcastsPort, setUp, startServer } from './server.mjs'

const { media, stop } = await startServer()
const browser = await chromium.launch()
const { open, dress, shot } = capture(browser, { origin, only: new Set(process.argv.slice(2)), paths: media })

/** A settings section, by its heading. */
const section = (page, heading) => page.locator('section').filter({ has: page.getByRole('heading', { name: heading, exact: true, level: 2 }) }).first()

let token
// Whether a screenshot was asked for (all of them when none were named).
const only = new Set(process.argv.slice(2))
const wanted = (name) => only.size === 0 || only.has(name)

/** Signs in as Home Assistant does, with a code an admin approves, and returns its token. */
async function homeAssistant() {
  const code = await api(null, 'POST', '/auth/device', { deviceName: 'Home Assistant', platform: 'homeassistant' })
  await api(token, 'POST', `/auth/device/${code.userCode}/approve`)
  return (await api(null, 'POST', '/auth/device/token', { deviceCode: code.deviceCode })).token
}
const find = async (title, kinds = ['movie', 'show']) => (await api(token, 'GET', `/search?q=${encodeURIComponent(title)}`)).find((i) => i.title === title && kinds.includes(i.kind))
const children = async (id) => {
  const r = await api(token, 'GET', `/items/${id}/children`)
  return r.items ?? r
}

try {
  // --- First run: create the admin, add a library ---
  {
    const page = await open(desktop)
    await page.goto(origin)
    await page.getByLabel('Username').waitFor()
    await page.getByLabel('Username').fill(admin.username)
    await page.getByLabel('Password').fill(admin.password)
    await shot(page, 'setup')
    await Promise.all([page.waitForResponse((r) => r.url().endsWith('/setup') && r.request().method() === 'POST'), page.getByRole('button', { name: 'Create account' }).click()])
    await page.context().close()
  }
  token = (await api(null, 'POST', '/auth/login', admin)).token
  await api(token, 'PUT', '/analysis', { mode: 'normal' })
  {
    const page = await open(desktop, token)
    await page.goto(origin + '/settings')
    await page.getByRole('button', { name: 'Add a library' }).click()
    await page.getByRole('button', { name: 'Movies', exact: true }).click()
    await page.locator('form button', { hasText: 'Movies' }).last().click() // the folder
    await sleep(500)
    await page.getByRole('button', { name: 'Use this folder' }).click()
    await shot(page, 'add-library', { target: section(page, 'Libraries') })
    await page.locator('form').getByRole('button', { name: 'Add a library' }).click()
    await sleep(1000)
    await page.context().close()
  }
  const libs = await addLibraries(token, media)

  // What the screenshots show: something half-watched, a few watched, a
  // watch list, a second person and a kids profile.
  const lm = await find('The Long Matinee')
  const crumb = await find('Crumb Street')
  const [s1] = await children(crumb.id)
  const eps = await children(s1.id)
  await api(token, 'PUT', `/items/${eps[0].id}/progress`, { positionMs: 0, played: true })
  await api(token, 'PUT', `/items/${eps[1].id}/progress`, { positionMs: 0, played: true })
  await api(token, 'PUT', `/items/${eps[2].id}/progress`, { positionMs: 14 * 60000, played: false })
  await api(token, 'PUT', `/items/${lm.id}/progress`, { positionMs: 47 * 60000, played: false })
  const velvet = await find('Velvet Static')
  await api(token, 'PUT', `/items/${velvet.id}/progress`, { positionMs: 71 * 60000, played: false })
  for (const t of ['Northbound', 'Harbor Lights', 'Last Reel']) await api(token, 'PUT', `/items/${(await find(t)).id}/progress`, { positionMs: 0, played: true })
  for (const t of ['A Quiet Orbit', 'Signal Fires', 'Salt & Cedar', 'The Projectionist']) await api(token, 'PUT', `/watchlist/${(await find(t)).id}`)
  await api(token, 'POST', '/users', { username: 'alex', password: 'alex-password', isAdmin: false })
  await api(token, 'POST', '/users', { username: 'jo', password: 'jo-password', isAdmin: false })

  // --- Watching ---
  {
    let page = await open(desktop, token)
    await page.goto(origin)
    await shot(page, 'home')
    await page.mouse.wheel(0, 700)
    await shot(page, 'home-rows')

    await page.goto(`${origin}/library/${libs.movies.id}`)
    await shot(page, 'library')
    await page.getByRole('button', { name: 'Filters', exact: true }).click()
    await sleep(400)
    await page.getByRole('dialog').getByRole('button', { name: 'Science Fiction' }).click().catch(() => {})
    await shot(page, 'filters')
    await page.keyboard.press('Escape')

    await page.goto(`${origin}/search`)
    await page.getByRole('searchbox').or(page.getByRole('textbox')).first().fill('harbor')
    await shot(page, 'search')

    await page.goto(`${origin}/item/${lm.id}`)
    await shot(page, 'movie')
    await page.getByRole('heading', { name: 'Cast' }).scrollIntoViewIfNeeded()
    await page.mouse.wheel(0, 500)
    await shot(page, 'movie-more')
    await page.getByRole('button', { name: 'More details' }).click()
    await shot(page, 'more-details', { target: page.getByRole('dialog') })
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'File details' }).click()
    // The demo files are stills at one frame every ten seconds; show what a real film would.
    await dress(page, { '0.1 fps': '23.976 fps', '96.2 MB': '58.4 GB', '103 kb/s': '62.8 Mb/s' })
    await shot(page, 'file-details', { target: page.getByRole('dialog') })
    await page.keyboard.press('Escape')

    await page.goto(`${origin}/person/1`)
    await shot(page, 'person')

    await page.goto(`${origin}/item/${crumb.id}`)
    await shot(page, 'show')
    await page.goto(`${origin}/item/${s1.id}`)
    await shot(page, 'season')

    await page.goto(`${origin}/watchlist`)
    await shot(page, 'watch-list')

    await page.goto(`${origin}/collections`)
    await shot(page, 'collections')
    await page.getByRole('button', { name: 'New smart collection' }).click()
    await sleep(500)
    await shot(page, 'smart-collection')
    await page.keyboard.press('Escape')
    await page.goto(`${origin}/box-set/901`)
    await shot(page, 'box-set')

    const album = await find('Signal Fires', ['album'])
    await page.goto(`${origin}/library/${libs.music.id}`)
    await shot(page, 'music')
    if (album) {
      await page.goto(`${origin}/item/${album.id}`)
      await page.getByRole('button', { name: 'Play' }).first().click()
      await page.evaluate(() => window.scrollTo(0, 0))
      await sleep(1500)
      await shot(page, 'album')
      if (wanted('cover-picker')) {
        // Covers found online, as the picker lists them; the demo server is offline.
        const svg = (hue, name) => 'data:image/svg+xml,' + encodeURIComponent(cover(202, 'Signal Fires', name, hue))
        const options = [
          ['Cover Art Archive', 'Signal Fires', '2021-04-16 · US · CD', 0],
          ['Cover Art Archive', 'Signal Fires', '2021 · GB · 12" Vinyl', 40],
          ['Cover Art Archive', 'Signal Fires (Deluxe)', '2022-11-18 · XW · Digital Media', 300],
          ['Apple', 'Signal Fires · Aurora Static', '2021', 200],
          ['Apple', 'Signal Fires (Live) · Aurora Static', '2023', 120],
          ['Deezer', 'Signal Fires · Aurora Static', '', 260],
        ].map(([source, title, detail, hue], i) => ({ url: `https://covers.example/${i}.jpg`, thumb: svg(hue, 'Aurora Static'), source, title, detail }))
        // Its own page, so the album's player below starts as it always has.
        const page = await open(desktop, token)
        await page.goto(`${origin}/item/${album.id}`)
        await page.route('**/api/v1/items/*/covers', (route) => route.fulfill({ json: { options, chosen: false } }))
        await page.getByRole('button', { name: 'Change cover' }).click()
        await page.getByRole('dialog').getByText('Deezer').waitFor()
        await shot(page, 'cover-picker', { target: page.getByRole('dialog') })
        await page.context().close()
      }
      await page.getByRole('button', { name: 'Open the player' }).click()
      await sleep(1200)
      await shot(page, 'now-playing')
      // The visualizer, a few seconds in, when the song has got going.
      await page.getByRole('button', { name: 'Visualizer' }).click()
      await sleep(3000)
      await shot(page, 'visualizer')
      await page.keyboard.press('ArrowRight')
      await sleep(1500)
      await shot(page, 'visualizer-halo')
      await page.keyboard.press('ArrowLeft')
      await page.keyboard.press('Escape')
      await page.keyboard.press('Escape')
      await page.goto(`${origin}/item/${album.artistId}`)
      await sleep(1000)
      await shot(page, 'artist')
    }

    // Playlists: two made from songs across albums, then adding to one.
    {
      const songs = await api(token, 'GET', `/libraries/${libs.music.id}/items?kind=track&sort=title`)
      const pick = (n, step) => songs.filter((_, i) => i % step === 0).slice(0, n).map((s) => s.id)
      const drive = await api(token, 'POST', '/playlists', { name: 'Night drive', items: pick(9, 2) })
      await api(token, 'POST', '/playlists', { name: 'Sunday morning', items: pick(6, 3).reverse() })
      await api(token, 'POST', '/playlists', { name: 'Focus', items: [] })
      await page.goto(`${origin}/library/${libs.music.id}?view=playlists`)
      await sleep(1000)
      await shot(page, 'playlists')
      await page.goto(`${origin}/playlist/${drive.id}`)
      await sleep(1000)
      await page.evaluate(() => window.scrollTo(0, 0))
      await shot(page, 'playlist')
      if (album) {
        await page.goto(`${origin}/item/${album.id}`)
        await sleep(800)
        await page.getByRole('button', { name: 'More options' }).first().click()
        await page.getByRole('menuitem', { name: 'Add to playlist' }).click()
        await sleep(600)
        await shot(page, 'add-to-playlist', { target: page.getByRole('dialog') })
        await page.keyboard.press('Escape')
      }
    }

    // Audiobooks: an hour into the first book of a series.
    const tide = await find('The Tide Keeper', ['book'])
    if (tide && libs.audiobooks) {
      await api(token, 'PUT', `/items/${tide.id}/progress`, { positionMs: 63 * 60000 })
      // The album above is still playing; close its player first.
      const close = page.getByRole('button', { name: 'Close player' })
      if (await close.count()) await close.first().click()
      await page.goto(`${origin}/library/${libs.audiobooks.id}`)
      await sleep(1000)
      await shot(page, 'audiobooks')
      await page.goto(`${origin}/item/${tide.id}`)
      await page.getByRole('button', { name: 'Resume' }).click()
      await page.evaluate(() => window.scrollTo(0, 0))
      await sleep(1500)
      await shot(page, 'book')
      await page.getByRole('button', { name: 'Open the player' }).click()
      await sleep(1200)
      await shot(page, 'book-player')
      await page.keyboard.press('Escape')
    }

    // Podcasts: three shows from the stand-in host, two followed, one
    // episode part-way through and one played (see docs/guide/watching/podcasts.md).
    if (['podcasts', 'podcast', 'podcast-player', 'podcasts-add', 'podcast-search'].some(wanted)) {
      // A fresh page, so the book above isn't in the player.
      await page.context().close()
      page = await open(desktop, token)
      const lib = await api(token, 'POST', '/libraries', { name: 'Podcasts', kind: 'podcasts', paths: [] })
      await api(token, 'PUT', '/integrations/podcastindex', { key: 'DEMOKEY0000000000000', secret: 'demo-secret-0000000000000000000000000000' })
      const shows = []
      for (const i of [0, 1, 2]) shows.push(await api(token, 'POST', '/podcasts', { libraryId: lib.id, feedUrl: feedURL(podcastsPort, i) }))
      await api(token, 'DELETE', `/podcasts/${shows[2].id}/follow`)
      const booth = await children(shows[0].id)
      await api(token, 'PUT', `/items/${booth[1].id}/progress`, { positionMs: 18 * 60000 })
      await api(token, 'PUT', `/items/${booth[2].id}/progress`, { positionMs: 0, played: true })

      await page.goto(`${origin}/library/${lib.id}`)
      await sleep(1000)
      await shot(page, 'podcasts')
      await page.goto(`${origin}/item/${shows[0].id}`)
      await page.getByRole('button', { name: 'Resume' }).or(page.getByRole('button', { name: 'Play latest' })).first().click()
      await page.evaluate(() => window.scrollTo(0, 0))
      await sleep(1500)
      await shot(page, 'podcast')
      await page.getByRole('button', { name: 'Open the player' }).click()
      await page.getByRole('button', { name: 'Show notes' }).click().catch(() => {})
      await sleep(1200)
      await shot(page, 'podcast-player')
      await page.keyboard.press('Escape')
      await page.getByRole('button', { name: 'Close player' }).first().click()
      await page.goto(`${origin}/library/${lib.id}?view=add`)
      await sleep(1000)
      await shot(page, 'podcasts-add')
      await page.getByRole('searchbox').or(page.getByPlaceholder('Search podcasts')).first().fill('film')
      await page.keyboard.press('Enter')
      await sleep(1200)
      await shot(page, 'podcast-search')
    }
    await page.context().close()
  }

  // --- The player ---
  {
    const page = await open(desktop, token)
    const harbor = await find('Harbor Lights')
    await api(token, 'PUT', `/items/${harbor.id}/progress`, { positionMs: 23 * 60000, played: false })
    await page.goto(`${origin}/watch/${harbor.id}`)
    await sleep(2500)
    await page.mouse.move(700, 500)
    await page.mouse.move(720, 520)
    await shot(page, 'player')
    await page.getByRole('button', { name: 'Subtitles' }).click()
    await shot(page, 'player-subtitles')
    await page.mouse.click(700, 300) // closes the menu, and pauses
    await page.getByRole('button', { name: 'Sleep timer' }).click()
    await page.getByRole('menuitemradio', { name: '30 minutes' }).click()
    await page.getByRole('button', { name: /^Sleep timer/ }).click()
    await shot(page, 'player-sleep')
    await page.getByRole('menuitemradio', { name: 'Off' }).click()

    // Adaptive quality: an admin turns it on, and the player offers Auto,
    // the quality steps and Original (see docs/guide/server/streaming-quality.md).
    if (wanted('player-quality')) {
      await api(token, 'PUT', '/settings', { adaptiveQuality: true })
      await page.reload()
      await sleep(2500)
      await page.mouse.move(700, 500)
      await page.getByRole('button', { name: /^Quality/ }).click()
      await page.getByRole('menuitemradio', { name: /720p/ }).click()
      await sleep(4000)
      await page.mouse.move(720, 520)
      await page.getByRole('button', { name: /^Quality/ }).click()
      await shot(page, 'player-quality')
      await page.mouse.click(700, 300) // closes the menu, and pauses
      await api(token, 'PUT', '/settings', { adaptiveQuality: false })
    }

    // Home Assistant pauses the film (see docs/guide/server/home-assistant.md).
    if (wanted('player-remote')) {
      const ha = await homeAssistant()
      await page.mouse.click(700, 300) // plays again
      await sleep(1500)
      const [client] = (await api(ha, 'GET', '/clients')).filter((c) => c.playing)
      await api(ha, 'POST', `/clients/${client.id}/commands`, { type: 'pause' })
      const notice = page.getByText('Controlled from Home Assistant')
      await notice.waitFor()
      // The notice leaves after 3 seconds, before the shot settles: keep a copy.
      await notice.evaluate((el) => {
        const box = el.parentElement
        const copy = box.cloneNode(true)
        copy.dataset.pinned = ''
        box.parentElement.append(copy)
      })
      await shot(page, 'player-remote')
      await page.evaluate(() => document.querySelector('[data-pinned]')?.remove())
    }

    // Up next: an episode just before its end.
    await page.goto(`${origin}/watch/${eps[3].id}`)
    await sleep(2500)
    // Click the seek bar 20 seconds from the end, inside the last 30 seconds where the card shows.
    const seek = page.getByRole('slider', { name: 'Seek' })
    const box = await seek.boundingBox()
    const total = eps[3].durationMs / 1000
    await page.mouse.click(box.x + box.width * (1 - 20 / total), box.y + box.height / 2)
    await page.getByText('Up next').waitFor({ timeout: 20000 })
    await sleep(1500)
    await shot(page, 'up-next')
    await page.context().close()
  }

  // --- On a phone ---
  {
    const page = await open(phone, token)
    await page.goto(origin)
    await shot(page, 'phone-home', { maxWidth: 1170 })
    await page.goto(`${origin}/item/${lm.id}`)
    await shot(page, 'phone-movie', { maxWidth: 1170 })
    await page.goto(`${origin}/libraries`)
    await shot(page, 'phone-libraries', { maxWidth: 1170 })
    await page.context().close()
  }

  // --- People, profiles, themes ---
  {
    const page = await open(desktop, token)
    await page.goto(`${origin}/settings`)
    await shot(page, 'people', { target: section(page, 'People') })
    if (wanted('find-artwork')) {
      const libraries = section(page, 'Libraries')
      const music = libraries.locator('div.flex.items-center.gap-4').filter({ hasText: 'Music' }).first()
      await music.getByRole('button', { name: 'Find artwork' }).click()
      await libraries.getByText('Looking for missing covers').waitFor()
      await shot(page, 'find-artwork', { target: libraries })
    }
    const profiles = section(page, 'Profiles')
    await profiles.getByRole('button', { name: 'Add profile' }).last().click()
    await profiles.getByLabel('Name').fill('Riley')
    await profiles.getByRole('combobox').first().selectOption({ index: 2 })
    await shot(page, 'profile-form', { target: profiles })
    await profiles.getByRole('button', { name: /save|add/i }).last().click()
    await sleep(800)
    await shot(page, 'profiles', { target: section(page, 'Profiles') })
    await shot(page, 'themes', { target: section(page, 'Appearance') })
    await shot(page, 'playback', { target: section(page, 'Playback') })
    await shot(page, 'new-files', { target: section(page, 'New files') })
    await shot(page, 'media-analysis', { target: section(page, 'Media analysis') })
    await shot(page, 'tmdb-key', { target: section(page, 'Posters and details') })
    await shot(page, 'home-network', { target: section(page, 'Home network') })
    await shot(page, 'trailers', { target: section(page, 'Trailers') })
    await shot(page, 'holiday-rows', { target: section(page, 'Home') })
    await shot(page, 'transcoding', { target: section(page, 'Transcoding') })
    if (wanted('adaptive-quality')) {
      await api(token, 'PUT', '/settings', { adaptiveQuality: true, remoteQualityCap: '720p' })
      await page.reload()
      await sleep(1000)
      await shot(page, 'adaptive-quality', { target: section(page, 'Transcoding') })
      await api(token, 'PUT', '/settings', { adaptiveQuality: false, remoteQualityCap: '' })
      await page.reload()
      await sleep(1000)
    }
    await shot(page, 'devices', { target: section(page, 'Your devices') })
    await shot(page, 'account', { target: section(page, 'Your account') })
    const plugins = section(page, 'Plugins')
    await plugins.getByRole('button', { name: 'Add plugin' }).click()
    await shot(page, 'plugins-add', { target: plugins })
    await plugins.getByRole('button', { name: 'Close' }).click()
    const arr = section(page, 'Radarr and Sonarr')
    await arr.getByRole('button', { name: 'Connect' }).first().click()
    await sleep(400)
    await shot(page, 'radarr-connect', { target: section(page, 'Radarr and Sonarr') })

    await page.goto(`${origin}/profiles`)
    await shot(page, 'who-is-watching')

    for (const theme of ['light', 'sci-fi', 'drive-in']) {
      await api(token, 'PUT', '/me/theme', { theme })
      await page.goto(origin)
      await shot(page, `theme-${theme}`)
    }
    await api(token, 'PUT', '/me/theme', { theme: '' })

    await page.goto(`${origin}/link`)
    await shot(page, 'link')

    // Home Assistant asking to sign in, as its setup shows the code.
    const code = await api(null, 'POST', '/auth/device', { deviceName: 'Home Assistant', platform: 'homeassistant' })
    await page.goto(`${origin}/link?code=${code.userCode}`)
    await page.getByText('Home Assistant', { exact: true }).waitFor()
    await shot(page, 'link-home-assistant')
    await page.context().close()
  }

  // --- Discover, last so the other screenshots don't show its tab ---
  {
    const page = await open(desktop, token)
    // With profiles set up, a new browser asks who's watching first.
    await page.goto(origin + '/profiles')
    await page.getByRole('button', { name: admin.username }).first().click()
    await sleep(500)
    await page.goto(origin + '/settings')
    const card = page.getByText('Not connected · for Discover').locator('xpath=ancestor::div[contains(@class, "rounded-xl")][1]')
    await card.getByRole('button', { name: 'Connect' }).click()
    await card.getByLabel('Address').fill(overseerrURL)
    await card.getByLabel('API key').fill('demo-overseerr-key')
    await card.getByRole('button', { name: 'Test' }).click()
    await card.getByText('answers at').waitFor()
    await dress(page, { [overseerrURL]: 'http://overseerr:5055' })
    await card.locator('input').first().evaluate((i) => (i.value = 'http://overseerr:5055'))
    await shot(page, 'overseerr-connect', { target: card })
    await card.getByRole('button', { name: 'Save' }).click()
    await page.getByText('Connected · ').first().waitFor()

    await page.goto(origin + '/discover')
    await page.getByRole('heading', { name: 'Trending' }).waitFor()
    await shot(page, 'discover')
    await page.getByText('Night Shift at the Planetarium').first().click()
    await page.getByRole('button', { name: 'Request the show' }).waitFor()
    await shot(page, 'discover-title')
    await page.getByRole('button', { name: 'Close' }).click()
    await page.getByPlaceholder('Find any movie or show…').fill('ferry')
    await page.getByText('The Ferry Years').first().waitFor()
    await shot(page, 'discover-search')
    await page.context().close()
  }
} finally {
  await browser.close()
  stop()
}
