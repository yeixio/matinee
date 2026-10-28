// The player: Matinee shows this page when someone plays an item, in a
// sandboxed frame. It gets the item and its file through matinee-player.js.
import { connect } from './matinee-player.js'

const matinee = await connect()
document.documentElement.lang = matinee.language
document.getElementById('title').textContent = matinee.item.title

const file = await matinee.file()
document.getElementById('about').textContent = `${file.byteLength} bytes, ${matinee.item.pluginData.size} when scanned`

// Tell Matinee the item was started.
await matinee.progress(0)

document.getElementById('done').onclick = async () => {
  await matinee.progress(0, true) // played to the end
  await matinee.exit()
}
