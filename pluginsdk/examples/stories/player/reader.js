// The Stories player: shows a story to read, remembers where the reader got
// to, and tells Matinee how far they read.
import { connect } from './matinee-player.js'

// A few languages, to show the idea: the web player says which one the
// person uses.
const strings = {
  en: { done: 'Done' },
  de: { done: 'Fertig' },
  es: { done: 'Terminado' },
  fr: { done: 'Terminé' },
}

const matinee = await connect()
const t = strings[matinee.language.split('-')[0]] ?? strings.en
const story = document.getElementById('story')
const done = document.getElementById('done')
done.textContent = t.done
document.documentElement.lang = matinee.language

const text = new TextDecoder().decode(await matinee.file())
render(text, matinee.item.pluginData?.markdown)

// Where the reader got to is kept per person, under a key for the story.
const key = `position/${matinee.item.id}`
const saved = await matinee.storage.get(key)
if (saved) {
  const { fraction } = JSON.parse(new TextDecoder().decode(saved))
  requestAnimationFrame(() => scrollTo(fraction))
}
story.focus()

const minutes = matinee.item.pluginData?.minutes ?? 1
let last = -1
async function save() {
  const fraction = readFraction()
  if (Math.abs(fraction - last) < 0.01) return
  last = fraction
  document.getElementById('read').style.width = `${fraction * 100}%`
  await matinee.storage.put(key, JSON.stringify({ fraction }))
  // Reading time stands in for a position in time.
  await matinee.progress(Math.round(fraction * minutes * 60_000), fraction > 0.98)
}

let timer
story.addEventListener('scroll', () => {
  clearTimeout(timer)
  timer = setTimeout(save, 500)
})
document.addEventListener('visibilitychange', () => document.hidden && save())

async function leave() {
  await save()
  await matinee.exit()
}

// Done marks the story read, wherever the reader stopped.
done.addEventListener('click', async () => {
  await matinee.progress(minutes * 60_000, true)
  await matinee.exit()
})
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' || e.key === 'Backspace') leave()
})

function readFraction() {
  const max = story.scrollHeight - story.clientHeight
  return max > 0 ? Math.min(1, story.scrollTop / max) : 1
}

function scrollTo(fraction) {
  story.scrollTop = fraction * (story.scrollHeight - story.clientHeight)
}

// render shows the story: paragraphs split on blank lines, with the first
// line as the title. Markdown headings become headings; the rest stays text.
function render(text, markdown) {
  const blocks = text.replace(/\r\n/g, '\n').split(/\n\s*\n/).map((b) => b.trim()).filter(Boolean)
  blocks.forEach((block, i) => {
    let el
    const heading = markdown && block.match(/^(#{1,6})\s+(.*)$/s)
    if (heading) {
      el = document.createElement(heading[1].length === 1 ? 'h1' : 'h2')
      el.textContent = heading[2]
    } else if (i === 0 && !markdown) {
      el = document.createElement('h1')
      el.textContent = block
    } else {
      el = document.createElement('p')
      el.textContent = block.replace(/\s*\n\s*/g, ' ')
    }
    story.append(el)
  })
}
