// Original artwork for the docs' demo library: posters, backdrops, stills
// and portraits drawn as SVG, so the screenshots show no one else's film
// art. The scenes are the ones the apps' store screenshots use.

import { readFileSync } from 'node:fs'
import nodePath from 'node:path'

// An SVG shown as an image can't reach the page's fonts, so the posters
// carry the app's own: Fraunces for titles, Inter for labels.
const fontsDir = nodePath.join(import.meta.dirname, '../node_modules/@fontsource-variable')
const font = (family, file) =>
  `@font-face{font-family:${family};src:url(data:font/woff2;base64,${readFileSync(nodePath.join(fontsDir, file)).toString('base64')})}`
const fonts = `<style>${font('Fraunces', 'fraunces/files/fraunces-latin-wght-normal.woff2')}${font('Inter', 'inter/files/inter-latin-wght-normal.woff2')}</style>`

/** A small seeded random, so every capture draws the same picture. */
function rand(seed) {
  let s = seed * 9301 + 49297
  return () => {
    s = (s * 9301 + 49297) % 233280
    return s / 233280
  }
}

const sky = (id, stops) =>
  `<linearGradient id="${id}" x1="0" y1="0" x2="0" y2="1">${stops.map((c, i) => `<stop offset="${i / (stops.length - 1)}" stop-color="${c}"/>`).join('')}</linearGradient>`

const glow = (id, color) =>
  `<radialGradient id="${id}"><stop offset="0" stop-color="${color}" stop-opacity="0.9"/><stop offset="0.35" stop-color="${color}" stop-opacity="0.35"/><stop offset="1" stop-color="${color}" stop-opacity="0"/></radialGradient>`

function stars(w, h, n, seed, maxY = 0.6) {
  const r = rand(seed)
  return Array.from({ length: n }, () => {
    const size = r() * r() * Math.min(w, h) * 0.004 + 0.6
    return `<circle cx="${(r() * w).toFixed(1)}" cy="${(r() * h * maxY).toFixed(1)}" r="${size.toFixed(2)}" fill="#fff" opacity="${(0.3 + r() * 0.7).toFixed(2)}"/>`
  }).join('')
}

/** A ridge line across the picture at `y`, rough by `amp`. */
function ridge(w, h, y, amp, seed, fill, steps = 9) {
  const r = rand(seed)
  let d = `M0 ${h} L0 ${y * h}`
  for (let i = 1; i <= steps; i++) d += ` L${((i / steps) * w).toFixed(1)} ${((y + (r() - 0.5) * amp) * h).toFixed(1)}`
  return `<path d="${d} L${w} ${h} Z" fill="${fill}"/>`
}

const water = (w, h, y, top, bottom) =>
  `<defs>${sky('water', [top, bottom])}</defs><rect y="${y * h}" width="${w}" height="${h * (1 - y)}" fill="url(#water)"/>`

function reflections(w, h, x, y, color, seed, n = 14) {
  const r = rand(seed)
  return Array.from({ length: n }, (_, i) => {
    const len = (0.02 + r() * 0.06) * w
    const yy = y * h + (i + 1) * h * 0.018
    return `<rect x="${(x * w - len / 2 + (r() - 0.5) * w * 0.03).toFixed(1)}" y="${yy.toFixed(1)}" width="${len.toFixed(1)}" height="${(h * 0.004).toFixed(1)}" rx="2" fill="${color}" opacity="${(0.8 - i / n / 1.4).toFixed(2)}"/>`
  }).join('')
}

const sun = (w, h, x, y, r, core, halo) =>
  `<defs>${glow('halo', halo)}</defs><circle cx="${x * w}" cy="${y * h}" r="${r * Math.min(w, h) * 3}" fill="url(#halo)"/><circle cx="${x * w}" cy="${y * h}" r="${r * Math.min(w, h)}" fill="${core}"/>`

const scenes = {
  // The Long Matinee: a lighthouse on an island at dusk, its beam across the sea.
  101: (w, h) => {
    const m = Math.min(w, h)
    const lx = w * 0.3
    const base = h * 0.62
    return `<defs>${sky('s', ['#1b2440', '#7a3e5c', '#f08a5d', '#f9c784'])}<linearGradient id="beam" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#fff3c4" stop-opacity="0.85"/><stop offset="1" stop-color="#fff3c4" stop-opacity="0"/></linearGradient></defs>
<rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 60, 1, 0.3)}
${sun(w, h, 0.72, 0.56, 0.11, '#ffe1a8', '#ffb36b')}
${water(w, h, 0.6, '#c7685a', '#1a1830')}${reflections(w, h, 0.72, 0.6, '#ffd59a', 3)}
<path d="M${lx} ${base - m * 0.29} L${w * 1.1} ${base - m * 0.4} L${w * 1.1} ${base - m * 0.18} Z" fill="url(#beam)" opacity="0.7"/>
<path d="M${w * 0.08} ${base + h * 0.02} Q${w * 0.3} ${base - h * 0.06} ${w * 0.52} ${base + h * 0.02} Z" fill="#141322"/>
<path d="M${lx - m * 0.035} ${base - h * 0.03} L${lx - m * 0.022} ${base - m * 0.27} L${lx + m * 0.022} ${base - m * 0.27} L${lx + m * 0.035} ${base - h * 0.03} Z" fill="#efe6dc"/>
<rect x="${lx - m * 0.03}" y="${base - m * 0.2}" width="${m * 0.06}" height="${m * 0.03}" fill="#c4473a"/>
<rect x="${lx - m * 0.034}" y="${base - m * 0.11}" width="${m * 0.068}" height="${m * 0.03}" fill="#c4473a"/>
<rect x="${lx - m * 0.03}" y="${base - m * 0.32}" width="${m * 0.06}" height="${m * 0.05}" rx="${m * 0.008}" fill="#fff3c4"/>
<path d="M${lx - m * 0.04} ${base - m * 0.32} L${lx} ${base - m * 0.36} L${lx + m * 0.04} ${base - m * 0.32} Z" fill="#2a2238"/>`
  },
  // Harbor Lights: boats and quay lights on a night harbor.
  102: (w, h) => {
    const r = rand(5)
    const lights = Array.from({ length: 9 }, (_, i) => {
      const x = (0.06 + i * 0.11 + r() * 0.03) * w
      const c = ['#ffcf6b', '#ff9f5a', '#9fd8ff'][i % 3]
      return `<circle cx="${x}" cy="${h * 0.555}" r="${Math.min(w, h) * 0.008}" fill="${c}"/>${reflections(w, h, x / w, 0.57, c, 10 + i, 8)}`
    }).join('')
    return `<defs>${sky('s', ['#050a1f', '#10224a', '#2b4a7a'])}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 120, 7, 0.45)}
${sun(w, h, 0.78, 0.2, 0.05, '#f4f1e1', '#a8c6ff')}
<rect y="${h * 0.48}" width="${w}" height="${h * 0.08}" fill="#0b1328"/>
${Array.from({ length: 7 }, (_, i) => `<rect x="${(0.04 + i * 0.14) * w}" y="${h * (0.4 + (i % 3) * 0.025)}" width="${w * 0.08}" height="${h * 0.16}" fill="#0d1a35"/>${Array.from({ length: 3 }, (_, j) => `<rect x="${(0.055 + i * 0.14 + j * 0.022) * w}" y="${h * (0.43 + (i % 3) * 0.025)}" width="${w * 0.01}" height="${h * 0.012}" fill="#ffd27a" opacity="${0.4 + ((i + j) % 3) * 0.25}"/>`).join('')}`).join('')}
${water(w, h, 0.56, '#16305e', '#03060f')}${lights}
<path d="M${w * 0.3} ${h * 0.72} L${w * 0.62} ${h * 0.72} L${w * 0.58} ${h * 0.77} L${w * 0.34} ${h * 0.77} Z" fill="#05070f"/>
<path d="M${w * 0.46} ${h * 0.72} L${w * 0.46} ${h * 0.5} L${w * 0.56} ${h * 0.7} Z" fill="#e9e3d0" opacity="0.92"/>
<rect x="${w * 0.455}" y="${h * 0.48}" width="${w * 0.006}" height="${h * 0.24}" fill="#05070f"/>`
  },
  // Paper Moons: three paper-cut moons over layered hills.
  103: (w, h) => `<defs>${sky('s', ['#2a0f45', '#7b2f7a', '#e0719b'])}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 70, 11, 0.5)}
<circle cx="${w * 0.28}" cy="${h * 0.28}" r="${Math.min(w, h) * 0.1}" fill="#ffe6f0" opacity="0.55"/>
<circle cx="${w * 0.62}" cy="${h * 0.22}" r="${Math.min(w, h) * 0.17}" fill="#fff4e8"/>
<circle cx="${w * 0.7}" cy="${h * 0.19}" r="${Math.min(w, h) * 0.15}" fill="url(#s)" opacity="0.92"/>
<circle cx="${w * 0.84}" cy="${h * 0.38}" r="${Math.min(w, h) * 0.06}" fill="#ffd1e3" opacity="0.7"/>
${ridge(w, h, 0.55, 0.12, 2, '#9a3d7d')}${ridge(w, h, 0.64, 0.1, 4, '#6b2464')}${ridge(w, h, 0.74, 0.08, 6, '#3e1544')}${ridge(w, h, 0.85, 0.05, 8, '#1d0a24')}`,
  // Northbound: rails running to the horizon under an aurora.
  104: (w, h) => {
    const hy = h * 0.55
    const ties = Array.from({ length: 12 }, (_, i) => {
      const t = Math.pow(i / 12, 2)
      const y = hy + t * (h - hy)
      const half = (0.01 + t * 0.4) * w
      return `<rect x="${w / 2 - half * 1.2}" y="${y}" width="${half * 2.4}" height="${Math.max(2, t * h * 0.02)}" fill="#2a2a2a"/>`
    }).join('')
    return `<defs>${sky('s', ['#020a14', '#06223a', '#0b3b4a'])}<linearGradient id="au" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#3dffb5" stop-opacity="0"/><stop offset="0.5" stop-color="#3dffb5" stop-opacity="0.7"/><stop offset="1" stop-color="#7a6bff" stop-opacity="0"/></linearGradient><filter id="blur"><feGaussianBlur stdDeviation="${Math.min(w, h) * 0.02}"/></filter></defs>
<rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 140, 13, 0.55)}
<path d="M${-w * 0.1} ${h * 0.35} C${w * 0.2} ${h * 0.1} ${w * 0.5} ${h * 0.45} ${w * 1.1} ${h * 0.12}" stroke="url(#au)" stroke-width="${h * 0.09}" fill="none" filter="url(#blur)"/>
<path d="M${-w * 0.1} ${h * 0.25} C${w * 0.3} ${h * 0.05} ${w * 0.6} ${h * 0.3} ${w * 1.1} ${h * 0.05}" stroke="url(#au)" stroke-width="${h * 0.05}" fill="none" filter="url(#blur)" opacity="0.6"/>
${ridge(w, h, 0.53, 0.06, 3, '#071a22', 14)}<rect y="${hy}" width="${w}" height="${h - hy}" fill="#0c1c1f"/>
${ties}<path d="M${w * 0.495} ${hy} L${w * 0.1} ${h} M${w * 0.505} ${hy} L${w * 0.9} ${h}" stroke="#b9c7c9" stroke-width="${Math.min(w, h) * 0.008}"/>
<circle cx="${w / 2}" cy="${hy - h * 0.005}" r="${Math.min(w, h) * 0.012}" fill="#ffe9a8"/>`
  },
  // Velvet Static: a neon horizon grid and a glowing set.
  105: (w, h) => {
    const hy = h * 0.58
    const lines = Array.from({ length: 10 }, (_, i) => {
      const y = hy + Math.pow((i + 1) / 10, 1.8) * (h - hy)
      return `<line x1="0" y1="${y}" x2="${w}" y2="${y}" stroke="#ff4fd8" stroke-width="2" opacity="0.6"/>`
    }).join('')
    const rays = Array.from({ length: 15 }, (_, i) => `<line x1="${w / 2}" y1="${hy}" x2="${(i / 14) * w * 3 - w}" y2="${h}" stroke="#ff4fd8" stroke-width="2" opacity="0.5"/>`).join('')
    const m = Math.min(w, h)
    return `<defs>${sky('s', ['#12001f', '#3b0a4f', '#ff4f8b'])}<linearGradient id="sun" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#ffe46b"/><stop offset="1" stop-color="#ff3d8b"/></linearGradient></defs>
<rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 60, 17, 0.4)}
<circle cx="${w / 2}" cy="${hy}" r="${m * 0.24}" fill="url(#sun)"/>
${Array.from({ length: 6 }, (_, i) => `<rect x="0" y="${hy - m * 0.02 - i * m * 0.035}" width="${w}" height="${m * (0.004 + i * 0.002)}" fill="#3b0a4f"/>`).join('')}
<rect y="${hy}" width="${w}" height="${h - hy}" fill="#14001f"/>${lines}${rays}
<rect x="${w / 2 - m * 0.13}" y="${hy - m * 0.14}" width="${m * 0.26}" height="${m * 0.2}" rx="${m * 0.03}" fill="#1a0526" stroke="#5ef2ff" stroke-width="${m * 0.008}"/>
<rect x="${w / 2 - m * 0.1}" y="${hy - m * 0.115}" width="${m * 0.2}" height="${m * 0.15}" rx="${m * 0.015}" fill="#5ef2ff" opacity="0.35"/>
${Array.from({ length: 7 }, (_, i) => `<rect x="${w / 2 - m * 0.1}" y="${hy - m * 0.11 + i * m * 0.021}" width="${m * 0.2}" height="${m * 0.006}" fill="#e8fdff" opacity="0.5"/>`).join('')}`
  },
  // A Quiet Orbit: a ringed planet and a small moon in deep space.
  106: (w, h) => {
    const m = Math.min(w, h)
    return `<defs><radialGradient id="p" cx="0.35" cy="0.35"><stop offset="0" stop-color="#ffd7a8"/><stop offset="0.6" stop-color="#d06a4a"/><stop offset="1" stop-color="#3b1424"/></radialGradient>${glow('neb', '#3b5bff')}</defs>
<rect width="${w}" height="${h}" fill="#03040c"/><circle cx="${w * 0.2}" cy="${h * 0.8}" r="${m * 0.7}" fill="url(#neb)" opacity="0.5"/>${stars(w, h, 220, 19, 1)}
<ellipse cx="${w * 0.58}" cy="${h * 0.45}" rx="${m * 0.42}" ry="${m * 0.09}" fill="none" stroke="#f0c9a0" stroke-width="${m * 0.02}" opacity="0.5" transform="rotate(-18 ${w * 0.58} ${h * 0.45})"/>
<circle cx="${w * 0.58}" cy="${h * 0.45}" r="${m * 0.24}" fill="url(#p)"/>
<path d="M${w * 0.58 - m * 0.4} ${h * 0.45 + m * 0.1} A${m * 0.42} ${m * 0.09} -18 0 0 ${w * 0.58 + m * 0.4} ${h * 0.45 - m * 0.13}" fill="none" stroke="#f6dcbc" stroke-width="${m * 0.02}" opacity="0.85"/>
<circle cx="${w * 0.22}" cy="${h * 0.25}" r="${m * 0.04}" fill="#cfd6ff"/>`
  },
  // The Projectionist: a beam of light across a dark cinema.
  107: (w, h) => {
    const m = Math.min(w, h)
    const seats = Array.from({ length: 5 }, (_, row) =>
      Array.from({ length: 14 }, (_, i) => `<rect x="${(i / 14) * w + ((row % 2) * w) / 28}" y="${h * (0.72 + row * 0.06)}" width="${w / 16}" height="${h * 0.07}" rx="${m * 0.015}" fill="#2a0e0e" opacity="${0.6 + row * 0.08}"/>`).join(''),
    ).join('')
    return `<defs><linearGradient id="beam" x1="1" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff2c7" stop-opacity="0.85"/><stop offset="1" stop-color="#ffcf73" stop-opacity="0.05"/></linearGradient><filter id="blur"><feGaussianBlur stdDeviation="${m * 0.01}"/></filter></defs>
<rect width="${w}" height="${h}" fill="#120606"/><rect x="${w * 0.06}" y="${h * 0.12}" width="${w * 0.6}" height="${h * 0.42}" fill="#3a1a12"/><rect x="${w * 0.08}" y="${h * 0.14}" width="${w * 0.56}" height="${h * 0.38}" fill="#f6d79a" opacity="0.9"/>
<path d="M${w * 0.9} ${h * 0.12} L${w * 0.08} ${h * 0.14} L${w * 0.64} ${h * 0.52} Z" fill="url(#beam)" filter="url(#blur)" opacity="0.6"/>
${stars(w * 0.3, h * 0.3, 30, 23).replace(/<circle cx="/g, `<circle transform="translate(${w * 0.6} ${h * 0.1})" cx="`)}
<circle cx="${w * 0.9}" cy="${h * 0.12}" r="${m * 0.03}" fill="#fff6d8"/>
<path d="M0 ${h * 0.12} Q${w * 0.04} ${h * 0.4} 0 ${h * 0.7} Z M${w} ${h * 0.2} Q${w * 0.94} ${h * 0.45} ${w} ${h * 0.7} Z" fill="#6b1414"/>${seats}`
  },
  // Salt & Cedar: cedars on a sea cliff at first light.
  108: (w, h) => {
    const r = rand(29)
    const trees = Array.from({ length: 16 }, (_, i) => {
      const x = (i / 15) * w * 1.1 - w * 0.05
      const th = (0.18 + r() * 0.2) * h
      const base = h * (0.68 + r() * 0.06)
      const tw = th * 0.32
      return `<path d="M${x} ${base - th} L${x - tw / 2} ${base} L${x + tw / 2} ${base} Z" fill="${['#0f2a22', '#123528', '#0b211b'][i % 3]}"/>`
    }).join('')
    return `<defs>${sky('s', ['#9ec9d6', '#e8e1cf', '#f2c9a0'])}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>
${sun(w, h, 0.25, 0.5, 0.06, '#fff4dc', '#ffd9a8')}${water(w, h, 0.55, '#8fb5bf', '#365d66')}${reflections(w, h, 0.25, 0.55, '#fff4dc', 31, 10)}
${ridge(w, h, 0.64, 0.05, 33, '#1f3d33', 6)}${trees}<rect y="${h * 0.76}" width="${w}" height="${h * 0.24}" fill="#0a1a15"/>`
  },
  // Last Reel: a desert road into a huge red sun.
  109: (w, h) => {
    const hy = h * 0.6
    return `<defs>${sky('s', ['#2b0b1a', '#a3243b', '#ff8a3d', '#ffd18a'])}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>
${sun(w, h, 0.5, 0.58, 0.2, '#ffe0a6', '#ff7b39')}${ridge(w, h, 0.56, 0.08, 37, '#5a1a2a', 7)}
<rect y="${hy}" width="${w}" height="${h - hy}" fill="#3a0f1c"/>
<path d="M${w * 0.48} ${hy} L${w * 0.1} ${h} L${w * 0.9} ${h} L${w * 0.52} ${hy} Z" fill="#1c070e"/>
${Array.from({ length: 6 }, (_, i) => { const t = Math.pow((i + 1) / 7, 2); const y = hy + t * (h - hy); return `<rect x="${w / 2 - w * 0.004 * (1 + t * 4)}" y="${y}" width="${w * 0.008 * (1 + t * 4)}" height="${h * 0.04 * t}" fill="#ffd18a"/>` }).join('')}
<path d="M${w * 0.82} ${h * 0.6} l0 -${h * 0.12} m0 ${h * 0.04} l-${w * 0.03} -${h * 0.02} m${w * 0.03} ${h * 0.05} l${w * 0.025} -${h * 0.03}" stroke="#1c070e" stroke-width="${Math.min(w, h) * 0.018}" stroke-linecap="round"/>`
  },
  // Crumb Street: a row of shopfronts with warm windows at night.
  201: (w, h) => {
    const colors = ['#e2725b', '#f2b45a', '#5e8c7f', '#c75b7a', '#7a6fb3']
    const shops = Array.from({ length: 5 }, (_, i) => {
      const x = (i / 5) * w
      const sw = w / 5
      const top = h * (0.28 + (i % 2) * 0.06)
      return `<rect x="${x}" y="${top}" width="${sw}" height="${h - top}" fill="${colors[i]}" opacity="0.85"/>
<rect x="${x + sw * 0.15}" y="${h * 0.62}" width="${sw * 0.7}" height="${h * 0.18}" fill="#ffe4a3"/>
<path d="M${x + sw * 0.08} ${h * 0.58} L${x + sw * 0.92} ${h * 0.58} L${x + sw * 0.86} ${h * 0.62} L${x + sw * 0.14} ${h * 0.62} Z" fill="#fff" opacity="0.85"/>
${[0, 1].map((j) => `<rect x="${x + sw * (0.2 + j * 0.35)}" y="${top + h * 0.06}" width="${sw * 0.25}" height="${h * 0.1}" fill="#ffe4a3" opacity="0.6"/>`).join('')}`
    }).join('')
    return `<defs>${sky('s', ['#141532', '#2c2d5c'])}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 50, 41, 0.25)}
${sun(w, h, 0.82, 0.12, 0.05, '#fff6e0', '#ffe0a8')}${shops}<rect y="${h * 0.8}" width="${w}" height="${h * 0.2}" fill="#1b1a2e"/>
<rect y="${h * 0.86}" width="${w}" height="${h * 0.01}" fill="#ffe4a3" opacity="0.4"/>`
  },
  // Signal Fires: beacons along a mountain ridge under the stars.
  202: (w, h) => {
    const fires = [0.18, 0.46, 0.78].map((x, i) => {
      const y = h * [0.55, 0.42, 0.5][i]
      const s = Math.min(w, h) * 0.04
      return `<circle cx="${x * w}" cy="${y}" r="${s * 4}" fill="url(#fire)"/><path d="M${x * w - s / 2} ${y} Q${x * w} ${y - s * 2} ${x * w + s / 2} ${y} Z" fill="#ffe08a"/>`
    }).join('')
    return `<defs>${sky('s', ['#030716', '#0c1838', '#3a1c3a'])}${glow('fire', '#ff7a2f')}</defs><rect width="${w}" height="${h}" fill="url(#s)"/>${stars(w, h, 200, 43, 0.6)}
${ridge(w, h, 0.5, 0.18, 45, '#140d22', 8)}${fires}${ridge(w, h, 0.72, 0.1, 47, '#0b0714', 6)}`
  },
}

/** Which scene a title uses: its own, its show's, or a mix by id. */
function sceneFor(id, showId) {
  return scenes[id] ?? (showId && scenes[showId]) ?? scenes[101 + (id % 9)]
}

const escape = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;')

/** A poster: the scene, a fade at the foot, and the title set in a serif. */
export function poster(id, title, large, showId, label = '', hue = 0) {
  const [w, h] = large ? [780, 1170] : [500, 750]
  const words = title.split(' ')
  // Two lines when the title is long, so it never runs off the poster.
  const lines = title.length > 12 && words.length > 1 ? [words.slice(0, Math.ceil(words.length / 2)).join(' '), words.slice(Math.ceil(words.length / 2)).join(' ')] : [title]
  const longest = Math.max(...lines.map((l) => l.length))
  const size = Math.min(w / 6.5, (w * 1.55) / longest)
  const top = h * 0.9 - (lines.length - 1) * size * 1.05 - (label ? size * 0.5 : 0)
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">${fonts}
${hue ? `<filter id="hue"><feColorMatrix type="hueRotate" values="${hue}"/></filter><g filter="url(#hue)">` : '<g>'}${sceneFor(id, showId)(w, h)}</g>
<defs><linearGradient id="fade" x1="0" y1="0" x2="0" y2="1"><stop offset="0.55" stop-color="#000" stop-opacity="0"/><stop offset="1" stop-color="#000" stop-opacity="0.8"/></linearGradient></defs>
<rect width="${w}" height="${h}" fill="url(#fade)"/>
${lines.map((l, i) => `<text x="50%" y="${top + i * size * 1.05}" font-family="Fraunces, Georgia, serif" font-weight="700" font-size="${size}" fill="#fff" text-anchor="middle" letter-spacing="${size * 0.01}">${escape(l)}</text>`).join('')}
${label ? `<text x="50%" y="${top + (lines.length - 1) * size * 1.05 + size * 0.8}" font-family="Inter, Helvetica, sans-serif" font-weight="600" font-size="${size * 0.42}" fill="#fff" opacity="0.85" text-anchor="middle" letter-spacing="${size * 0.06}">${escape(label.toUpperCase())}</text>` : ''}
</svg>`
}

const tint = (hue, body) => (hue ? `<filter id="hue"><feColorMatrix type="hueRotate" values="${hue}"/></filter><g filter="url(#hue)">${body}</g>` : body)

/** A backdrop, or an episode's still: the scene alone, wide. */
export function backdrop(id, large, showId, hue = 0) {
  const [w, h] = large ? [3840, 2160] : [1280, 720]
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">${tint(hue, sceneFor(id, showId)(w, h))}</svg>`
}

/** An album cover: a square scene with the album and artist names. */
export function cover(id, album, artist, hue = 0) {
  const s = 600
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${s}" height="${s}" viewBox="0 0 ${s} ${s}">${fonts}${tint(hue, sceneFor(id)(s, s))}
<text x="40" y="${s - 84}" font-family="Fraunces, Georgia, serif" font-weight="700" font-size="58" fill="#fff">${escape(album)}</text>
<text x="42" y="${s - 40}" font-family="Inter, Helvetica, sans-serif" font-weight="600" font-size="22" fill="#fff" opacity="0.85" letter-spacing="3">${escape(artist.toUpperCase())}</text>
</svg>`
}

/** A portrait: a soft-lit head and shoulders on a tinted backdrop. */
export function portrait(id) {
  const hues = [18, 200, 280, 140, 330, 40, 220, 100]
  const h = hues[id % hues.length]
  return `<svg xmlns="http://www.w3.org/2000/svg" width="632" height="632" viewBox="0 0 632 632">
<defs><radialGradient id="bg" cx="0.3" cy="0.2"><stop offset="0" stop-color="hsl(${h},45%,55%)"/><stop offset="1" stop-color="hsl(${h},40%,18%)"/></radialGradient>
<linearGradient id="skin" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${(h + 25) % 360},35%,78%)"/><stop offset="1" stop-color="hsl(${(h + 25) % 360},25%,45%)"/></linearGradient></defs>
<rect width="632" height="632" fill="url(#bg)"/>
<ellipse cx="316" cy="640" rx="250" ry="210" fill="hsl(${h},25%,25%)"/>
<rect x="276" y="330" width="80" height="90" fill="url(#skin)"/>
<ellipse cx="316" cy="250" rx="112" ry="135" fill="url(#skin)"/>
<path d="M200 240 Q210 100 316 105 Q430 100 432 250 Q410 160 316 165 Q230 165 200 240 Z" fill="hsl(${(h + 180) % 360},20%,${12 + (id % 3) * 12}%)"/>
</svg>`
}
