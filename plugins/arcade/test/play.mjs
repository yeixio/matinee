// Plays a game with the arcade plugin's player in Chromium, the way the web
// player runs it (a sandboxed frame without an origin of its own, talking
// over postMessage), and checks it draws, saves and continues:
//
//   node plugins/arcade/test/play.mjs <unpacked package> <roms folder> <game> [screenshot.png]
//
// The arcade workflow runs it on gridlee, whose ROMs mamedev.org gives
// away. Needs playwright and pngjs.

import { readFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import path from 'node:path'
import { chromium } from 'playwright'
import { PNG } from 'pngjs'

const [pkg, roms, game, screenshot = 'arcade.png'] = process.argv.slice(2)
if (!game) {
  console.error('usage: play.mjs <unpacked package> <roms folder> <game> [screenshot.png]')
  process.exit(2)
}

const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.wasm': 'application/wasm' }

// The harness stands in for Matinee's web player: it answers the player's
// requests the way web/src/plugins.ts does, with storage kept in memory.
const harness = `<!doctype html>
<body style="margin:0;background:#000">
<iframe id="f" src="/player/index.html" sandbox="allow-scripts allow-pointer-lock allow-downloads"
  allow="gamepad; fullscreen; autoplay" style="position:fixed;inset:0;width:100%;height:100%;border:0"></iframe>
<script>
  const storage = (window.storage = new Map())
  const item = { id: 1, libraryId: 1, kind: 'game', title: 'Test game', year: 1983, facts: [], pluginData: __DATA__ }
  const frame = document.getElementById('f')
  const send = (msg, transfer = []) => frame.contentWindow.postMessage(msg, '*', transfer)
  const rom = async (name) => {
    const res = await fetch('/roms/' + name)
    if (!res.ok) throw new Error('not found')
    return res.arrayBuffer()
  }
  const methods = {
    file: async () => { const b = await rom(item.pluginData.name + item.pluginData.ext); return [b, [b]] },
    libraryFile: async ({ path }) => { const b = await rom(path.split('/').pop()); return [b, [b]] },
    'storage.list': async ({ prefix }) => [{ keys: [...storage.keys()].filter((k) => k.startsWith(prefix)).map((key) => ({ key, size: storage.get(key).byteLength })) }, []],
    'storage.get': async ({ key }) => { const v = storage.get(key); const b = v ? v.slice(0) : null; return [b, b ? [b] : []] },
    'storage.put': async ({ key, value }) => { storage.set(key, value); return [null, []] },
    'storage.delete': async ({ key }) => { storage.delete(key); return [null, []] },
    progress: async () => [null, []],
    exit: async () => [null, []],
  }
  addEventListener('message', async (e) => {
    if (e.source !== frame.contentWindow || e.data?.matinee !== 1) return
    const msg = e.data
    if (msg.type === 'ready') send({ matinee: 1, type: 'start', item, language: 'en' })
    if (msg.type === 'request') {
      try {
        const [result, transfer] = await methods[msg.method](msg.params ?? {})
        send({ matinee: 1, type: 'response', id: msg.id, result }, transfer)
      } catch (err) {
        send({ matinee: 1, type: 'response', id: msg.id, error: String(err.message ?? err) })
      }
    }
  })
</script>`

const data = await gameData(game)

const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x')
  try {
    if (url.pathname === '/') {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      return res.end(harness.replace('__DATA__', JSON.stringify(data)))
    }
    let file
    if (url.pathname.startsWith('/player/')) file = path.join(pkg, path.normalize(url.pathname))
    else if (url.pathname.startsWith('/roms/')) file = path.join(roms, path.basename(url.pathname))
    else throw new Error('no')
    const body = await readFile(file)
    res.writeHead(200, {
      'Content-Type': types[path.extname(file)] ?? 'application/octet-stream',
      // As Matinee serves plugin files: never as the site's own origin.
      'Content-Security-Policy': 'sandbox allow-scripts allow-pointer-lock allow-downloads',
      'Access-Control-Allow-Origin': '*',
    })
    res.end(body)
  } catch {
    res.writeHead(404)
    res.end()
  }
})
await new Promise((r) => server.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${server.address().port}`

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required', '--use-gl=swiftshader', '--enable-unsafe-swiftshader'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
page.on('console', (m) => console.log(`[player] ${m.text()}`))
page.on('pageerror', (e) => console.log(`[player error] ${e.message}`))
let failed = false
const check = (ok, what) => {
  console.log(`${ok ? 'ok' : 'FAIL'}: ${what}`)
  if (!ok) failed = true
}

try {
  await page.goto(origin)
  const player = () => page.frames().find((f) => f.url().endsWith('/player/index.html'))
  await page.waitForFunction(() => document.getElementById('f').contentWindow)
  await waitFor(() => player()?.locator('#play').isVisible())
  await player().locator('#play').click()
  await player().waitForFunction(() => globalThis.JSMAME?.running, null, { timeout: 120_000 })
  check(true, `${game} started`)

  await page.waitForTimeout(8000)
  await page.screenshot({ path: screenshot })
  check(lit(await readFile(screenshot)) > 0.01, 'the game draws something')

  await player().locator('#save').click({ force: true })
  await page.waitForFunction((key) => window.storage.get(key)?.byteLength > 0, `states/${game}/resume.sta`, { timeout: 20_000 })
  check(true, 'a saved state was kept')

  // Open the game again, as the web player would; storage stays.
  await page.evaluate(() => {
    const f = document.getElementById('f')
    f.src = f.src
  })
  await page.waitForTimeout(500)
  await waitFor(() => player()?.locator('#continue').isVisible())
  await player().locator('#continue').click()
  await player().waitForFunction(() => globalThis.JSMAME?.running, null, { timeout: 120_000 })
  check(true, 'continued from the saved state')
} catch (err) {
  check(false, err.message)
  await page.screenshot({ path: screenshot }).catch(() => {})
} finally {
  await browser.close()
  server.close()
}
process.exit(failed ? 1 : 0)

async function waitFor(fn, timeout = 30_000) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    if (await fn().catch(() => false)) return
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error(`timed out waiting for ${fn}`)
}

// lit is the share of a screenshot's pixels that aren't near black.
function lit(png) {
  const { data, width, height } = PNG.sync.read(png)
  let n = 0
  for (let i = 0; i < data.length; i += 4) if (data[i] + data[i + 1] + data[i + 2] > 60) n++
  return n / (width * height)
}

// gameData is what the server part would keep with the game, from the
// package's game list.
async function gameData(name) {
  const { gunzipSync } = await import('node:zlib')
  const db = JSON.parse(gunzipSync(await readFile(path.join(pkg, 'games.json.gz'))).toString())
  const g = db.games.find((x) => x.n === name)
  if (!g) throw new Error(`${name} isn't in the game list`)
  return {
    name: g.n,
    ext: '.zip',
    dir: '',
    needs: g.r ?? [],
    build: g.s.replace(/\//g, '_').replace(/\.cpp$/, ''),
    playable: true,
    rotate: g.rot ?? 0,
    width: g.w,
    height: g.h,
    saveState: !!g.ss,
    mame: db.mame,
  }
}
