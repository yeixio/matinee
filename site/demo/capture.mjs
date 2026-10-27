// What both screenshot scripts share: browser contexts, waiting for a page
// to settle, and saving shots as WebP into docs/guide/images.

import { mkdirSync } from 'node:fs'
import path from 'node:path'
import sharp from 'sharp'

export const out = path.resolve(import.meta.dirname, '../../docs/guide/images')
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
export const desktop = { viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 }
export const phone = { viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true }

/**
 * Helpers for one browser and server. only: the shot names to save (all
 * when empty). paths: on-disk media folder to show as /media instead.
 */
export function capture(browser, { origin, only = new Set(), paths }) {
  mkdirSync(out, { recursive: true })

  async function open(options, token) {
    const ctx = await browser.newContext({ ...options, reducedMotion: 'reduce' })
    if (token) await ctx.addCookies([{ name: 'matinee_session', value: token, url: origin }])
    return ctx.newPage()
  }

  /** Waits for the page's requests and images to settle. */
  async function settle(page, extra = 700) {
    await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {})
    // Images in view; lazy ones further down never load, so don't wait forever.
    await page.evaluate(() =>
      Promise.race([
        Promise.all([...document.images].filter((i) => i.getBoundingClientRect().top < innerHeight).map((i) => (i.complete ? null : i.decode().catch(() => null)))),
        new Promise((r) => setTimeout(r, 4000)),
      ]),
    )
    await sleep(extra)
  }

  /** Swaps text on the page, for the few numbers the demo files can't make look real. */
  async function dress(page, swaps) {
    await settle(page, 200)
    await page.evaluate((swaps) => {
      const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT)
      for (let n = walk.nextNode(); n; n = walk.nextNode()) for (const [a, b] of Object.entries(swaps)) if (n.nodeValue.includes(a)) n.nodeValue = n.nodeValue.replaceAll(a, b)
    }, swaps)
  }

  const wanted = (name) => !only.size || only.has(name)

  /**
   * Saves a screenshot as WebP: the viewport, the whole page, or the area
   * around an element (with `pad` pixels of the page around it).
   */
  async function shot(page, name, { target, pad = 24, fullPage = false, maxWidth = 2000 } = {}) {
    if (!wanted(name)) return
    await settle(page)
    if (paths) await dress(page, { [paths]: '/media' })
    let png
    if (target) {
      // The sticky top bar would cover part of a long element in a full-page capture.
      await page.addStyleTag({ content: 'header, [role=banner] { position: static !important }' })
      await target.scrollIntoViewIfNeeded()
      await sleep(300)
      const box = await target.boundingBox()
      const scroll = await page.evaluate(() => ({ x: scrollX, y: scrollY }))
      png = await page.screenshot({
        fullPage: true,
        animations: 'disabled',
        clip: { x: Math.max(0, box.x + scroll.x - pad), y: Math.max(0, box.y + scroll.y - pad), width: box.width + pad * 2, height: box.height + pad * 2 },
      })
    } else {
      png = await page.screenshot({ fullPage, animations: 'disabled' })
    }
    const img = sharp(png)
    const { width } = await img.metadata()
    await (width > maxWidth ? img.resize({ width: maxWidth }) : img).webp({ quality: 86 }).toFile(path.join(out, `${name}.webp`))
    console.log('shot', name)
  }

  return { open, settle, dress, shot, wanted }
}
