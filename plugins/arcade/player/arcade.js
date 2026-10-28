// The arcade player: runs a game with MAME built to WebAssembly, one build
// per MAME driver (player/mame/<driver>.js and .wasm). Matinee hands it the
// game's ROM set and the sets it needs; it keeps each person's place in a
// saved state, in the plugin's storage.

import { connect } from './matinee-player.js'
import { strings } from './strings.js'

const $ = (id) => document.getElementById(id)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const matinee = await connect()
const t = strings(matinee.language)
const item = matinee.item
const game = item.pluginData ?? {}
const stateKey = `states/${game.name}/resume.sta`
const statePath = '/resume.sta'
const vertical = game.rotate === 90 || game.rotate === 270

document.documentElement.lang = matinee.language || 'en'
document.documentElement.dir = ['ar', 'fa', 'he', 'ur'].includes(String(matinee.language).split('-')[0]) ? 'rtl' : 'ltr'
document.title = item.title
document.documentElement.style.setProperty('--aspect', vertical ? '3 / 4' : '4 / 3')
$('title').textContent = item.title
$('play').textContent = t.play
$('continue').textContent = t.continue
$('save').textContent = t.save
$('load').textContent = t.load
$('help').textContent = t.controls
$('full').textContent = t.fullScreen
$('close').textContent = t.close
for (const id of ['keys', 'helpkeys']) {
  for (const [what, keys] of t.keys) {
    const dt = document.createElement('dt')
    dt.textContent = what
    const dd = document.createElement('dd')
    dd.textContent = keys
    $(id).append(dt, dd)
  }
}
$('start').hidden = false

const message = (text, error = false) => {
  $('message').textContent = text
  $('message').classList.toggle('error', error)
}

if (!game.playable) {
  message(t.notPlayable)
} else {
  let saved = false
  if (game.saveState) {
    try {
      saved = (await matinee.storage.list(`states/${game.name}/`)).keys.some((k) => k.key === stateKey)
    } catch {
      // Storage is a nicety; the game still plays.
    }
  }
  $('keys').hidden = false
  $('play').textContent = saved ? t.newGame : t.play
  $('play').classList.toggle('primary', !saved)
  $('play').hidden = false
  $('continue').hidden = !saved
  $('play').onclick = () => run(false)
  $('continue').onclick = () => run(true)
  ;(saved ? $('continue') : $('play')).focus()
}

async function run(resume) {
  $('choices').hidden = true
  $('keys').hidden = true
  try {
    message(t.loading)
    const files = await romSets()
    const state = resume ? await matinee.storage.get(stateKey) : null
    message(t.starting)
    await startMAME(files, state)
  } catch (err) {
    message(`${t.failed} ${err.message ?? err}`, true)
    $('choices').hidden = false
  }
}

// romSets fetches the game's set and the sets MAME reads some of its ROMs
// from: the parent, the BIOS, devices. Those are looked for next to the
// game, as a zip or a 7z; with a merged or non-merged collection some
// aren't there, and MAME says so if it needed one.
async function romSets() {
  const files = [[game.name + game.ext, await matinee.file()]]
  for (const need of game.needs ?? []) {
    for (const ext of ['.zip', '.7z']) {
      try {
        files.push([need + ext, await matinee.libraryFile(game.dir + need + ext)])
        break
      } catch {
        // Not there as this kind of file.
      }
    }
  }
  return files
}

const log = []

function startMAME(files, state) {
  return new Promise((resolve, reject) => {
    const canvas = $('canvas')
    const [w, h] = resolution()
    const fail = () => reject(new Error(log.filter((l) => !/^(Warning|WARNING)/.test(l)).slice(-3).join(' ') || t.crashed))
    globalThis.Module = {
      canvas,
      arguments: [
        game.name,
        '-rompath', '/roms',
        '-cfg_directory', '/cfg',
        '-nvram_directory', '/nvram',
        '-skip_gameinfo',
        // Browsers have WebGL, which is OpenGL ES 2, but not the desktop
        // OpenGL MAME draws with by default. accel draws through an SDL
        // renderer, and opengles2 is SDL's WebGL one. (The browser build
        // has no soft renderer: asking for it falls back to OpenGL.)
        '-video', 'accel',
        '-renderdriver', 'opengles2',
        '-sound', 'js',
        '-resolution', `${w}x${h}`,
        ...(state ? ['-state', statePath] : []),
      ],
      preRun: [
        () => {
          const fs = FS()
          fs.mkdir('/roms')
          for (const [name, data] of files) fs.writeFile(`/roms/${name}`, new Uint8Array(data))
          if (state) fs.writeFile(statePath, new Uint8Array(state))
        },
      ],
      locateFile: (path) => (path.endsWith('.wasm') ? `mame/${game.build}.wasm` : path),
      print: (line) => log.push(line),
      printErr: (line) => log.push(line),
      onAbort: fail,
      onExit: fail,
    }
    const script = document.createElement('script')
    script.src = `mame/${game.build}.js`
    script.onerror = () => reject(new Error(t.crashed))
    document.body.append(script)

    // MAME marks itself running once the game has started.
    const started = Date.now()
    const wait = setInterval(() => {
      if (globalThis.JSMAME?.running) {
        clearInterval(wait)
        playing()
        resolve()
      } else if (Date.now() - started > 120_000) {
        clearInterval(wait)
        fail()
      }
    }, 100)
  })
}

// MAME draws at a fixed size; pick one that fills the screen at the
// cabinet's 4:3 (or 3:4) shape, sharp on high density screens.
function resolution() {
  const aspect = vertical ? 3 / 4 : 4 / 3
  const scale = Math.min(devicePixelRatio || 1, 2)
  let width = Math.min(innerWidth, innerHeight * aspect) * scale
  width = Math.max(320, Math.min(width, 1920))
  return [Math.round(width), Math.round(width / aspect)]
}

// FS is Emscripten's file system, which the arcade workflow exports.
function FS() {
  return globalThis.Module.FS ?? globalThis.FS
}

function playing() {
  $('start').hidden = true
  $('bar').hidden = false
  $('canvas').focus()
  $('save').disabled = !game.saveState
  try {
    FS().readFile(statePath)
  } catch {
    $('load').disabled = true
  }
  if (!game.saveState) $('save').title = t.noSaving

  // Browsers start sound only after a click or key press.
  const wake = () => globalThis.jsmame_web_audio?.get_context()?.resume()
  addEventListener('pointerdown', wake)
  addEventListener('keydown', wake)

  const begun = Date.now()
  setInterval(() => matinee.progress(Date.now() - begun).catch(() => {}), 15_000)
  if (game.saveState) {
    setInterval(() => saveState(true), 60_000)
    document.addEventListener('visibilitychange', () => document.hidden && saveState(true))
  }
}

let saving = false

// saveState has MAME write a saved state and keeps it in storage, where
// Continue finds it next time.
async function saveState(quiet = false) {
  if (saving || !globalThis.JSMAME?.running) return
  saving = true
  try {
    const fs = FS()
    try {
      fs.unlink(statePath)
    } catch {
      // There's none yet.
    }
    globalThis.JSMAME.save(statePath)
    let data
    for (let i = 0; i < 50 && !data; i++) {
      await sleep(100)
      try {
        data = fs.readFile(statePath)
      } catch {
        // Not written yet.
      }
    }
    if (!data) throw new Error(t.noSaving)
    await matinee.storage.put(stateKey, data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength))
    $('load').disabled = false
    if (!quiet) status(t.saved)
  } catch (err) {
    if (!quiet) status(err.message ?? String(err))
  } finally {
    saving = false
  }
}

function loadState() {
  try {
    FS().readFile(statePath)
  } catch {
    return
  }
  globalThis.JSMAME.load(statePath)
  status(t.loaded)
  $('canvas').focus()
}

let statusTimer
function status(text) {
  $('status').textContent = text
  clearTimeout(statusTimer)
  statusTimer = setTimeout(() => ($('status').textContent = ''), 3000)
}

$('save').onclick = () => {
  saveState()
  $('canvas').focus()
}
$('load').onclick = loadState
$('help').onclick = () => $('helpbox').showModal()
$('helpbox').addEventListener('close', () => $('canvas').focus())
$('full').onclick = () => {
  if (document.fullscreenElement) document.exitFullscreen()
  else document.documentElement.requestFullscreen?.().catch(() => {})
  $('canvas').focus()
}

// Escape would quit MAME and leave a black screen; Matinee's own back
// button leaves the game instead.
addEventListener(
  'keydown',
  (e) => {
    if (e.key === 'Escape' && !$('helpbox').open) {
      e.preventDefault()
      e.stopImmediatePropagation()
    }
  },
  true,
)
