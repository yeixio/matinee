// Makes the docs' demo library from catalog.mjs: video files (a still of each
// title's artwork, long enough to look real, tiny on disk), music, subtitles,
// a trailer, and the artwork demo/fake-tmdb.mjs serves. Needs ffmpeg and
// Playwright's Chromium. Files that already exist are kept, so a second run
// takes seconds.

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright'
import { backdrop, cover, portrait, poster } from './art.mjs'
import { albums, audiobooks, discover, movies, people, podcasts, shows } from './catalog.mjs'

const ff = (...args) => execFileSync('ffmpeg', ['-v', 'error', '-y', ...args])

/** Renders SVGs to JPEGs, keeping ones already there. */
async function render(jobs) {
  jobs = jobs.filter((j) => !existsSync(j.file))
  if (!jobs.length) return
  const browser = await chromium.launch()
  const page = await browser.newPage()
  for (const { file, svg, width, height } of jobs) {
    mkdirSync(path.dirname(file), { recursive: true })
    await page.setViewportSize({ width, height })
    await page.setContent(`<style>*{margin:0}img{display:block;width:${width}px;height:${height}px}</style><img src="data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}">`)
    await page.locator('img').evaluate((img) => img.decode())
    await page.screenshot({ path: file, type: 'jpeg', quality: 88 })
  }
  await browser.close()
}

/**
 * Ten minutes of silence in a codec and layout, encoded once and then looped
 * into every video by copying, since encoding hours of audio is the slow part.
 */
function silence(dir, layout) {
  const file = path.join(dir, `silence-${layout}.mka`)
  if (!existsSync(file)) {
    mkdirSync(dir, { recursive: true })
    ff('-f', 'lavfi', '-i', `anullsrc=r=48000:cl=${layout}`, '-t', '600', '-c:a', layout === '5.1' ? 'eac3' : 'aac', '-b:a', layout === '5.1' ? '96k' : '24k', file)
  }
  return file
}

/**
 * A video that holds one picture for its whole length, at one frame every ten
 * seconds. chapters, [title, start seconds, end seconds], are marked inside.
 */
function video(file, still, seconds, { uhd = false, audio = 'stereo', extraAudio = false, chapters = [] } = {}) {
  if (existsSync(file)) return
  mkdirSync(path.dirname(file), { recursive: true })
  const v = uhd
    ? ['-c:v', 'libx265', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p10le', '-x265-params',
       'log-level=error:hdr10=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc:master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400',
       '-tag:v', 'hvc1']
    : ['-c:v', 'libx264', '-preset', 'ultrafast', '-tune', 'stillimage', '-pix_fmt', 'yuv420p']
  const dir = path.join(path.dirname(path.dirname(still)), 'silence')
  const args = ['-loop', '1', '-framerate', '0.1', '-i', still, '-stream_loop', '-1', '-i', silence(dir, audio)]
  if (extraAudio) args.push('-stream_loop', '-1', '-i', silence(dir, 'stereo'))
  args.push('-t', String(seconds), '-vf', `scale=${uhd ? '3840:2160' : '1920:1080'}`, ...v, '-g', '30', '-map', '0:v', '-map', '1:a', '-c:a', 'copy', '-metadata:s:a:0', 'language=eng')
  if (extraAudio) args.push('-map', '2:a', '-metadata:s:a:1', 'language=eng', '-metadata:s:a:1', "title=Director's commentary")
  if (chapters.length) {
    const meta = `${file}.chapters.txt`
    writeFileSync(meta, ';FFMETADATA1\n' + chapters.map(([title, start, end]) => `[CHAPTER]\nTIMEBASE=1/1000\nSTART=${start * 1000}\nEND=${end * 1000}\ntitle=${title}\n`).join(''))
    // Inputs given after the options apply to the output only, so put this one first.
    args.unshift('-i', meta)
    for (let i = 0; i < args.length; i++) if (args[i] === '-map' && /^\d+:/.test(args[i + 1])) args[i + 1] = args[i + 1].replace(/^\d+/, (n) => String(Number(n) + 1))
    args.push('-map_chapters', '0')
  }
  ff(...args, file)
  if (chapters.length) rmSync(`${file}.chapters.txt`)
}

function subtitles(file, lines) {
  if (existsSync(file)) return
  let srt = ''
  lines.forEach((l, i) => (srt += `${i + 1}\n00:${String(i * 2).padStart(2, '0')}:01,000 --> 00:${String(i * 2).padStart(2, '0')}:04,000\n${l}\n\n`))
  writeFileSync(file, srt)
}

export async function makeLibrary(root) {
  const media = path.join(root, 'media')
  const images = path.join(root, 'images')
  const jobs = []
  const art = (name, svg, width, height) => {
    const file = path.join(images, name)
    jobs.push({ file, svg, width, height })
    return file
  }

  for (const m of movies) {
    art(`poster-${m.id}.jpg`, poster(m.scene ?? m.id, m.title, true, undefined, '', m.hue ?? 0), 780, 1170)
    art(`backdrop-${m.id}.jpg`, backdrop(m.scene ?? m.id, true, undefined, m.hue ?? 0), 1920, 1080)
  }
  for (const s of shows) {
    art(`poster-${s.id}.jpg`, poster(s.scene ?? s.id, s.title, true, undefined, '', s.hue ?? 0), 780, 1170)
    art(`backdrop-${s.id}.jpg`, backdrop(s.scene ?? s.id, true, undefined, s.hue ?? 0), 1920, 1080)
    s.seasons.forEach((eps, si) => {
      art(`poster-${s.id}-s${si + 1}.jpg`, poster(s.scene ?? s.id, s.title, true, undefined, `Season ${si + 1}`, (s.hue ?? 0) + si * 40), 780, 1170)
      eps.forEach((_, ei) => art(`still-${s.id}-${si + 1}-${ei + 1}.jpg`, backdrop(s.scene ?? s.id, false, undefined, (s.hue ?? 0) + si * 40 + ei * 25), 1280, 720))
    })
  }
  for (const t of discover) {
    art(`poster-${t.id}.jpg`, poster(t.scene, t.title, true, undefined, '', t.hue), 780, 1170)
    art(`backdrop-${t.id}.jpg`, backdrop(t.scene, true, undefined, t.hue), 1920, 1080)
  }
  for (const p of people) art(`person-${p.id}.jpg`, portrait(p.id), 632, 632)
  albums.forEach((a, i) => {
    art(`cover-${i}.jpg`, cover(a.scene, a.album, a.artist, a.hue ?? 0), 600, 600)
    art(`artist-${i}.jpg`, portrait(40 + i * 3), 632, 632)
  })
  audiobooks.forEach((b, i) => art(`book-${i}.jpg`, cover(b.scene, b.title, b.author, b.hue ?? 0), 600, 600))
  podcasts.forEach((p, i) => art(`podcast-${i}.jpg`, cover(p.scene, p.title, p.author, p.hue ?? 0), 600, 600))
  await render(jobs)

  for (const m of movies) {
    const name = `${m.title} (${m.year})`
    const dir = path.join(media, 'Movies', name)
    video(path.join(dir, `${name}.${m.uhd ? 'mkv' : 'mp4'}`), path.join(images, `backdrop-${m.id}.jpg`), m.minutes * 60, m.uhd ? { uhd: true, audio: '5.1', extraAudio: true } : {})
    subtitles(path.join(dir, `${name}.en.srt`), ['The lights go down.', '<i>Somewhere, a projector hums.</i>', 'Popcorn, anyone?'])
    if (m.id === 101 || m.id === 107) {
      subtitles(path.join(dir, `${name}.es.srt`), ['Se apagan las luces.', '<i>En algún lugar, zumba un proyector.</i>', '¿Palomitas?'])
      video(path.join(dir, `${name}-trailer.mp4`), path.join(images, `backdrop-${m.id}.jpg`), 140)
    }
  }
  for (const s of shows) {
    s.seasons.forEach((eps, si) => {
      eps.forEach((_, ei) => {
        const se = `S${String(si + 1).padStart(2, '0')}E${String(ei + 1).padStart(2, '0')}`
        const minutes = s.kids ? 11 : 42 + ((ei * 7) % 9)
        // A cold open, then the intro, which players offer to skip.
        const open = 60 + ei * 15
        const chapters = s.kids ? [] : [['Cold open', 0, open], ['Intro', open, open + 45], ['Episode', open + 45, minutes * 60]]
        video(path.join(media, 'TV', s.title, `Season ${si + 1}`, `${s.title} - ${se}.mkv`), path.join(images, `still-${s.id}-${si + 1}-${ei + 1}.jpg`), minutes * 60, { chapters })
      })
    })
  }
  albums.forEach((a, i) => {
    const dir = path.join(media, 'Music', a.artist, a.album)
    mkdirSync(dir, { recursive: true })
    execFileSync('cp', [path.join(images, `cover-${i}.jpg`), path.join(dir, 'cover.jpg')])
    execFileSync('cp', [path.join(images, `artist-${i}.jpg`), path.join(media, 'Music', a.artist, 'artist.jpg')])
    a.tracks.forEach((t, n) => {
      const file = path.join(dir, `${String(n + 1).padStart(2, '0')} ${t}.flac`)
      if (existsSync(file)) return
      // A little tune, so the visualizer has a beat, a bass line and some hiss to show.
      ff('-f', 'lavfi', '-i', `aevalsrc='${tune(110 + n * 20)}':s=22050:d=${150 + n * 23}`, '-af', 'volume=0.05', '-sample_fmt', 's16',
        '-metadata', `artist=${a.artist}`, '-metadata', `album_artist=${a.artist}`, '-metadata', `album=${a.album}`,
        '-metadata', `title=${t}`, '-metadata', `track=${n + 1}`, '-metadata', `date=${a.year}`, file)
    })
  })
  audiobooks.forEach((b, i) => book(path.join(media, 'Audiobooks'), b, path.join(images, `book-${i}.jpg`), path.join(root, 'silence')))
  // Every podcast episode plays the same hour of quiet; fake-podcasts.mjs serves it.
  const episode = path.join(root, 'silence', 'podcast-episode.m4a')
  if (!existsSync(episode)) ff('-stream_loop', '-1', '-i', silenceFile(path.join(root, 'silence')), '-t', '3600', '-map', '0:a', '-c:a', 'copy', episode)
  return { media, images, episode }
}

/** A kick on every beat, a bass note that swells, a chord over it and a hi-hat between beats, at f Hz. */
function tune(f) {
  return [
    `0.6*sin(2*PI*55*t)*exp(-9*mod(t,0.5))`,
    `0.35*sin(2*PI*${f}*t)*(0.55+0.45*sin(2*PI*0.25*t))`,
    `0.12*sin(2*PI*${f * 2.5}*t)+0.08*sin(2*PI*${f * 3}*t)*abs(sin(PI*0.5*t))`,
    `0.08*sin(2*PI*3520*t)*exp(-30*mod(t+0.25,0.5))`,
  ].join('+')
}

/**
 * An audiobook: one .m4b with its chapters marked inside, or a folder with a
 * file per chapter. Chapters run 20 to 50 minutes of silence, copied, so
 * hours of book take seconds to make.
 */
function book(root, b, coverFile, silenceDir) {
  const name = b.series ? `Book ${b.index} - ${b.title}` : b.title
  const dir = path.join(root, b.author, ...(b.series ? [b.series] : []), name)
  mkdirSync(dir, { recursive: true })
  execFileSync('cp', [coverFile, path.join(dir, 'cover.jpg')])
  writeFileSync(path.join(dir, 'desc.txt'), b.overview + '\n')
  const minutes = b.chapters.map((_, i) => (i === 0 || i === b.chapters.length - 1 ? 2 : 20 + ((i * 13) % 31)))
  const silence = silenceFile(silenceDir)
  const tags = ['-metadata', `artist=${b.author}`, '-metadata', `album_artist=${b.author}`, '-metadata', `album=${b.title}`,
    '-metadata', `composer=${b.narrator}`, '-metadata', `date=${b.year}`, '-metadata', 'genre=Audiobook']
  if (b.whole) {
    const file = path.join(dir, `${b.title}.m4b`)
    if (existsSync(file)) return
    let meta = ';FFMETADATA1\n'
    let at = 0
    b.chapters.forEach((c, i) => {
      meta += `[CHAPTER]\nTIMEBASE=1/1000\nSTART=${at}\nEND=${at + minutes[i] * 60000}\ntitle=${c}\n`
      at += minutes[i] * 60000
    })
    const metaFile = path.join(silenceDir, `chapters-${b.title}.txt`)
    writeFileSync(metaFile, meta)
    ff('-stream_loop', '-1', '-i', silence, '-i', metaFile, '-t', String(at / 1000), '-map', '0:a', '-map_metadata', '1', '-map_chapters', '1',
      ...tags, '-metadata', `title=${b.title}`, '-c:a', 'copy', '-f', 'mp4', file)
    return
  }
  b.chapters.forEach((c, i) => {
    const file = path.join(dir, `${String(i + 1).padStart(2, '0')} - ${c}.m4a`)
    if (existsSync(file)) return
    ff('-stream_loop', '-1', '-i', silence, '-t', String(minutes[i] * 60), '-map', '0:a', ...tags,
      '-metadata', `title=${c}`, '-metadata', `track=${i + 1}/${b.chapters.length}`, '-c:a', 'copy', file)
  })
}

/** Ten minutes of quiet stereo AAC, for audiobooks. */
function silenceFile(dir) {
  const file = path.join(dir, 'silence-book.m4a')
  if (!existsSync(file)) {
    mkdirSync(dir, { recursive: true })
    ff('-f', 'lavfi', '-i', 'anullsrc=r=22050:cl=mono', '-t', '600', '-c:a', 'aac', '-b:a', '16k', file)
  }
  return file
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const root = process.argv[2] ?? path.join(import.meta.dirname, '.cache')
  console.log(await makeLibrary(root))
}
