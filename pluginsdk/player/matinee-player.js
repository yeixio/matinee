// matinee-player.js: the player side of a Matinee plugin (MIT licensed,
// like the rest of the SDK). Copy it into your plugin's player folder and
// load it from your page:
//
//   <script type="module">
//     import { connect } from './matinee-player.js'
//     const matinee = await connect()
//     const rom = await matinee.file()            // the item's file, an ArrayBuffer
//     const save = await matinee.storage.get('save/1')
//   </script>
//
// The page runs in a sandboxed frame without an origin of its own, so it
// can't call the server; each of these asks the web player around it, which
// answers as whoever is watching. docs/plugins.md lists the messages.

let next = 1
const waiting = new Map()

window.addEventListener('message', (e) => {
  if (e.source !== window.parent) return
  const msg = e.data
  if (!msg || msg.matinee !== 1 || msg.type !== 'response') return
  const w = waiting.get(msg.id)
  if (!w) return
  waiting.delete(msg.id)
  if (msg.error) w.reject(new Error(msg.error))
  else w.resolve(msg.result)
})

function call(method, params, transfer = []) {
  const id = next++
  return new Promise((resolve, reject) => {
    waiting.set(id, { resolve, reject })
    window.parent.postMessage({ matinee: 1, type: 'request', id, method, params }, '*', transfer)
  })
}

// connect tells the web player the page is ready and resolves with the item
// to play and what the page can ask for.
export function connect() {
  return new Promise((resolve) => {
    const onStart = (e) => {
      if (e.source !== window.parent || e.data?.matinee !== 1 || e.data.type !== 'start') return
      window.removeEventListener('message', onStart)
      resolve({
        // item: id, libraryId, kind, title, year, facts and pluginData
        // (what the plugin's server part kept with it).
        item: e.data.item,
        language: e.data.language,
        file: () => call('file'),
        // A file in one of the item's library's folders, by its path in the
        // folder, such as a parent ROM set: 'pacman.zip'.
        libraryFile: (path) => call('libraryFile', { path }),
        storage: {
          list: (prefix = '') => call('storage.list', { prefix }),
          get: (key) => call('storage.get', { key }),
          put: (key, value) => call('storage.put', { key, value }, value instanceof ArrayBuffer ? [value] : []),
          delete: (key) => call('storage.delete', { key }),
        },
        // Where the person is, for Continue Watching; finished marks it played.
        progress: (positionMs, finished = false) => call('progress', { positionMs, finished }),
        exit: () => call('exit'),
      })
    }
    window.addEventListener('message', onStart)
    window.parent.postMessage({ matinee: 1, type: 'ready' }, '*')
  })
}
