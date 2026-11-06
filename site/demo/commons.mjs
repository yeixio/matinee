// Artwork from Wikimedia Commons for the README's demo library
// (public-catalog.mjs): downloads each original once into the cache, then
// cuts it to the posters, backdrops and covers the stand-in TMDB serves.

import { existsSync, mkdirSync, statSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import path from 'node:path'
import sharp from 'sharp'

// Wikimedia asks automated clients to say who they are.
const agent = 'MatineeDocsScreenshots/1.0 (https://github.com/yeixio/matinee)'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** The original file's address on upload.wikimedia.org, from its name on Commons. */
export function commonsURL(name) {
  const file = name.replaceAll(' ', '_')
  const h = createHash('md5').update(file).digest('hex')
  return `https://upload.wikimedia.org/wikipedia/commons/${h[0]}/${h.slice(0, 2)}/${encodeURIComponent(file)}`
}

/** Downloads a Commons original into dir unless it's there, waiting out rate limits. */
export async function download(name, dir) {
  mkdirSync(dir, { recursive: true })
  const file = path.join(dir, name.replaceAll('/', '_'))
  if (existsSync(file) && statSync(file).size > 10000) return file
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(commonsURL(name), { headers: { 'User-Agent': agent } })
    if (res.ok) {
      writeFileSync(file, Buffer.from(await res.arrayBuffer()))
      return file
    }
    if (res.status !== 429 || attempt > 8) throw new Error(`${name}: ${res.status} from Wikimedia Commons`)
    const wait = Number(res.headers.get('retry-after')) * 1000 || attempt * 15000
    console.log(`Wikimedia Commons asked us to slow down; trying ${name} again in ${Math.round(wait / 1000)}s`)
    await sleep(wait)
  }
}

/**
 * Cuts src to width×height. A picture of a different shape is cropped,
 * centred across and at focus (0 top, 1 bottom) down.
 */
export async function cut(src, out, width, height, focus = 0.5) {
  if (existsSync(out)) return
  mkdirSync(path.dirname(out), { recursive: true })
  const img = sharp(src).rotate().flatten({ background: '#000' })
  const { width: w, height: h } = await img.metadata()
  const scale = Math.max(width / w, height / h)
  const sw = Math.round(w * scale)
  const sh = Math.round(h * scale)
  const left = Math.round((sw - width) / 2)
  const top = Math.round((sh - height) * focus)
  await img.resize(sw, sh).extract({ left, top, width, height }).jpeg({ quality: 88 }).toFile(out)
}

/**
 * A wide backdrop from a picture: a landscape one cut to fit, a portrait
 * poster shown whole on the right over a blurred, darker copy of itself,
 * so the page's title and buttons on the left sit on the blur.
 */
export async function backdrop(src, out, width, height) {
  if (existsSync(out)) return
  const { width: w, height: h } = await sharp(src).rotate().metadata()
  if (w / h > 1.2) return cut(src, out, width, height, 0.5)
  mkdirSync(path.dirname(out), { recursive: true })
  const blur = await sharp(src).rotate().flatten({ background: '#000' }).resize(width, height, { fit: 'cover' }).blur(40).modulate({ brightness: 0.55 }).toBuffer()
  const ph = Math.round(height * 0.92)
  const pw = Math.round((w / h) * ph)
  const poster = await sharp(src).rotate().flatten({ background: '#000' }).resize(pw, ph).toBuffer()
  await sharp(blur)
    .composite([{ input: poster, left: Math.round(width * 0.68 - pw / 2), top: Math.round((height - ph) / 2) }])
    .jpeg({ quality: 88 })
    .toFile(out)
}
