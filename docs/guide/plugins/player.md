---
title: The player
description: Write the web page that plays a plugin's titles in Matinee's web player, with matinee-player.js for the title's file, storage and progress.
since: v0.1.1
sidebar:
  order: 5
---

A player is a web page in the plugin's package that plays its titles: an emulator, a reader, a viewer. When someone plays one of your titles in the web player, Matinee opens the page full screen and hands it the title. It can be plain HTML and JavaScript, or anything a bundler builds into a folder.

```json
"players": [{ "itemKinds": ["story"], "entry": "player/index.html" }]
```

Everything in the entry page's folder (`player/` here) is served to the web player, so put the page, its scripts, styles, WebAssembly and images there. Nothing else in the package is served.

## The sandbox

The page runs in a frame sandboxed with `allow-scripts allow-pointer-lock allow-downloads` and no origin of its own. Matinee also serves the player's files with a `Content-Security-Policy: sandbox` header, so the page never runs as the Matinee site, even when opened on its own. This means:

- The page can't see who's signed in or call Matinee's API. It asks the web player instead, through `matinee-player.js`, and the web player answers as whoever is watching.
- `localStorage`, `IndexedDB` and cookies aren't there. Use [storage](#storage), which follows the person from device to device.
- Loading the page's own files is a cross-origin request. Matinee sends `Access-Control-Allow-Origin: *` for them, so `fetch`, module scripts and `WebAssembly.instantiateStreaming` work with relative URLs.
- The frame may use the keyboard, gamepads, pointer lock, full screen and sound without a click first. It has the keyboard as soon as it opens.
- The page can load what any web page can from the internet. Your plugin's [network permissions](/docs/plugins/manifest/#permissions) are for the server part.

The web player puts a faded **Back** button in the top-left corner (top-right in right-to-left languages), so keep that corner free of controls.

Phone and TV apps don't play plugin titles yet, so people play them in the web player.

## matinee-player.js

Copy [`matinee-player.js`](https://github.com/yeixio/matinee/blob/main/pluginsdk/player/matinee-player.js) into your player's folder. For editors and TypeScript, copy [`matinee-player.d.ts`](https://github.com/yeixio/matinee/blob/main/pluginsdk/player/matinee-player.d.ts) next to it. `matinee-plugin new` does both.

```js
import { connect } from './matinee-player.js'

const matinee = await connect()
```

`connect()` tells the web player the page is ready, and resolves once it says what to play. Everything else hangs off what it returns:

| | |
| --- | --- |
| `matinee.item` | The title being played: `id`, `libraryId`, `kind`, `title`, `year`, `facts`, and `pluginData`, the `Data` the server part kept with it. |
| `matinee.language` | The language of the person's Matinee, such as `en` or `pt-BR`. Show your page in it if you can. |
| `matinee.file()` | The title's file, as an `ArrayBuffer`. |
| `matinee.libraryFile(path)` | Another file in a folder of the title's library, by its path in that folder, such as `neogeo.zip`: a BIOS, a parent set, a cover. Only files with the library kind's extensions are handed over. |
| `matinee.storage` | Small values kept for the person. [See below](#storage). |
| `matinee.progress(positionMs, finished)` | Saves how far the person got. `finished: true` marks the title played. |
| `matinee.exit()` | Closes the player and goes back to the title's page. |

Each call returns a promise, which rejects with an `Error` if the web player refuses.

Files come whole, in memory. For files of hundreds of megabytes, show that something is happening while they load.

## Storage

Storage keeps values for each person who plays (each profile has its own), separately for each plugin. A save made on one computer is there on the next. Use it for saved games, reading positions and preferences.

```js
await matinee.storage.put(`saves/${matinee.item.id}/slot1`, stateBuffer) // ArrayBuffer or string
const saved = await matinee.storage.get(`saves/${matinee.item.id}/slot1`) // ArrayBuffer, or null
const { keys, used, limit } = await matinee.storage.list(`saves/${matinee.item.id}/`)
await matinee.storage.delete(`saves/${matinee.item.id}/slot1`)
```

- Keys are letters, digits, `.`, `_`, `-` and `/`, up to 200 characters. They're yours to arrange: key by `item.id` for something per title, or by something in `pluginData` that stays the same if the library is scanned again, such as a game's name.
- A value can be up to 32 MB, and a person's storage for one plugin up to 512 MB. `list` says how much is `used` of the `limit`.
- `put` hands an `ArrayBuffer` over rather than copying it, so the page can't use that buffer afterwards.
- Values are deleted when the plugin is removed. Keep them when the plugin is updated: a new version should read what an older one wrote.

## Progress

Call `progress` as the person plays, every few seconds or at natural points, and when they leave. Matinee saves where they got to, and `finished: true` marks the title played. Plugins that listen for [`playback.started` and `playback.stopped`](/docs/plugins/server/#events) hear of it, as they do for movies and music. A title without a natural position in time, such as a game, can report the time played.

## The messages underneath

`matinee-player.js` is a thin layer over `postMessage`, so a player can do without it. Every message is an object with `matinee: 1`:

1. The page posts `{matinee: 1, type: "ready"}` to `window.parent`.
2. The web player answers `{matinee: 1, type: "start", item, language}`.
3. The page asks with `{matinee: 1, type: "request", id, method, params}`. The web player answers `{matinee: 1, type: "response", id, result}`, or `{..., error}` with a message.

| Method | Params | Result |
| --- | --- | --- |
| `file` | | An `ArrayBuffer` |
| `libraryFile` | `path` | An `ArrayBuffer` |
| `storage.list` | `prefix` | `{keys: [{key, size, updatedAt}], used, limit}` |
| `storage.get` | `key` | An `ArrayBuffer`, or `null` |
| `storage.put` | `key`, `value` | |
| `storage.delete` | `key` | |
| `progress` | `positionMs`, `finished` | |
| `exit` | | |

`ArrayBuffer`s are transferred, not copied, both ways.

## Trying it

`matinee-plugin try <folder>` runs your player in the same sandboxed frame without a Matinee server, and answers it the way the web player does. See [Testing and debugging](/docs/plugins/testing/#try-the-player).
