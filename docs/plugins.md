# Writing plugins

A plugin adds to Matinee without changing it: a new kind of library with
its own way of reading files, details for that library's titles, a player
for them in the web player, and reactions to what happens on the server.
The [arcade plugin](../plugins/arcade), which plays games with MAME, is the
reference: it uses every part described here.

People running Matinee install plugins in **Settings → Plugins** (see the
[user guide](guide/server/plugins.md)). This page is for people writing them.

## What's in a plugin

A plugin is a zip file named `<id>.matinee-plugin`:

```text
plugin.json          the manifest (below)
server.wasm          the server part: WebAssembly, optional
player/index.html    the player: a web page, optional
player/...           anything else the player loads
icon.png             shown in Settings, optional
```

A plugin has a server part, a player, or both.

### plugin.json

```json
{
  "id": "arcade",
  "name": "Arcade",
  "version": "1.0.0",
  "api": 1,
  "description": "Play arcade games with MAME.",
  "author": "The Matinee authors",
  "license": "GPL-2.0-or-later",
  "homepage": "https://github.com/yeixio/matinee",
  "icon": "icon.png",
  "server": "server.wasm",
  "libraryKinds": [
    {
      "name": "arcade",
      "label": "Arcade",
      "description": "MAME ROM sets, one zip per game.",
      "extensions": [".zip", ".7z"],
      "itemKinds": ["game"]
    }
  ],
  "metadata": ["game"],
  "events": ["playback.started"],
  "players": [{ "itemKinds": ["game"], "entry": "player/index.html" }],
  "settings": [{ "key": "apiKey", "label": "API key", "secret": true }],
  "permissions": { "network": ["api.example.com", "*.images.example.com"] }
}
```

- `id`: 2 to 40 lowercase letters, digits and dashes. It's the folder the
  plugin is installed in and how the API names it. Installing a plugin with
  the same id replaces it.
- `api`: the plugin API version, 1 for now. Matinee lists a plugin made for
  a version it doesn't run, but won't turn it on.
- `libraryKinds`: each becomes a choice when adding a library. Matinee walks
  the library's folders and asks the server part about every file whose
  extension is listed. `itemKinds` are the kinds of item the plugin files,
  such as `game`, plus any containers it puts them in, such as `system`.
  Names can't be Matinee's own (`movies`, `movie`, `show`, `track`...).
- `metadata`: item kinds the server part looks up details for after a scan,
  the way TMDB does for movies. Only the plugin's own kinds.
- `events`: `item.added`, `library.scanned`, `playback.started`,
  `playback.stopped`.
- `players`: pages that play items of some kinds. Everything in the entry
  page's folder is served to the web player; nothing else in the package is.
- `settings`: what an admin fills in. The server part gets the values with
  every call. Secret ones show as a password field and are never sent back.
- `permissions.network`: the hosts the server part may fetch from, over
  https. `*.example.com` allows every subdomain, not `example.com` itself.
  The admin sees the list before turning the plugin on, and an update that
  asks for more hosts turns the plugin off until they agree again.

## The server part

The server part is a WebAssembly module that Matinee runs with
[wazero](https://wazero.io) in a sandbox. It gets:

- the folders of the library it's scanning, read only, at their own paths
  (a Windows folder like `C:\Games` is `/C/Games`);
- its own package, read only, at `/_matinee/plugin`;
- a folder of its own to keep things in, at `/_matinee/data`;
- the network only through Matinee's `fetch`, and only to the hosts it lists;
- 1 GB of memory and two minutes per call.

A plugin that crashes or runs out of time fails that one call; Matinee
carries on, and starts a fresh copy of the plugin for the next scan.

### In Go

The SDK is in [`pluginsdk`](../pluginsdk), under the MIT license so plugins
can use any license. Build with Go 1.24 or later:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"

	plugin "github.com/yeixio/matinee/pluginsdk"
)

func init() {
	plugin.Register(plugin.Handlers{Index: index})
}

func main() {}

func index(r plugin.IndexRequest) (plugin.IndexResult, error) {
	name := strings.TrimSuffix(filepath.Base(r.Path), filepath.Ext(r.Path))
	it := &plugin.Item{Kind: "game", Title: name}
	if _, err := os.Stat(filepath.Join(r.Root, "snap", name+".png")); err == nil {
		it.Poster = filepath.Join(r.Root, "snap", name+".png")
	}
	return plugin.IndexResult{Item: it}, nil
}
```

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o server.wasm .
```

Returning no item skips the file. An item can name `Parents`, outermost
first, which Matinee creates once and shares between the items in them.
`Data` is kept with the item and given to the player. `Poster` and
`Backdrop` are a file the plugin can see or an https URL.

### In other languages

Any language that builds a WASI (preview 1) reactor module works. The
module exports:

| Export | Signature | |
| --- | --- | --- |
| `_initialize` | `() -> ()` | run once when the module starts |
| `matinee_malloc` | `(size i32) -> i32` | memory for Matinee to write a request into |
| `matinee_free` | `(ptr i32) -> ()` | Matinee is done with a buffer the plugin handed it |
| `matinee_handle` | `(ptr i32, len i32) -> i64` | answer a request; the result is the answer's address in the high 32 bits and its length in the low 32 |

Requests are JSON: `{"method": "...", "params": {...}}`. Answers are
`{"result": ...}` or `{"error": "..."}`.

| Method | Params | Result |
| --- | --- | --- |
| `scan.index` | `library {id, name, kind}`, `root`, `path`, `size`, `mtime`, `config` | `{"item": Item}`, or `{}` to skip the file |
| `metadata.lookup` | `kind`, `title`, `year`, `path`, `data`, `config` | `Details`, or `null` for no match |
| `event` | `type`, `time` (ms), `item`, `library`, `user {id, name}`, `played {positionMs, durationMs, finished}`, `config` | `null` |

`Item` is `kind`, `title`, `sortTitle`, `year`, `index`, `parents` (each
`kind`, `title`, `year`, `index`, `poster`, `backdrop`), `data`, and the
`Details` fields: `overview`, `genres`, `rating` (out of 10), `facts` (each
`label` and `value`), `poster`, `backdrop`. The Go SDK's types
(`pluginsdk/plugin.go`) are the reference.

The module imports, from the `matinee` module:

| Import | Signature | |
| --- | --- | --- |
| `log` | `(level i32, ptr i32, len i32)` | write to the server's log: 0 debug, 1 info, 2 warning, 3 error |
| `fetch` | `(ptr i32, len i32) -> i32` | make an HTTP request; returns the answer's length |
| `take` | `(ptr i32)` | copy the answer to the last `fetch` into a buffer of that length |

A fetch request is `{"method", "url", "headers", "body"}` and its answer
`{"status", "headers", "body"}` or `{"error"}`, with bodies base64 encoded.
Answers are limited to 32 MB.

## The player

A player is a web page. The web player shows it full screen in a frame
sandboxed without an origin of its own (`sandbox="allow-scripts
allow-pointer-lock allow-downloads"`), and the server serves its files with
a `Content-Security-Policy: sandbox` header, so even opened on its own it
never runs as the Matinee site. It can't read anyone's session or call the
API. Instead it asks the web player, which answers as whoever is watching.

[`pluginsdk/player/matinee-player.js`](../pluginsdk/player/matinee-player.js)
wraps the messages:

```js
import { connect } from './matinee-player.js'

const matinee = await connect()
const rom = await matinee.file() // ArrayBuffer
await matinee.storage.put(`save/${matinee.item.id}`, state)
```

Underneath, every message is an object with `matinee: 1`:

1. The page posts `{type: "ready"}` to its parent.
2. The web player answers `{type: "start", item, language}`. `item` has
   `id`, `libraryId`, `kind`, `title`, `year`, `facts` and `pluginData`.
3. The page asks with `{type: "request", id, method, params}`, and the web
   player answers `{type: "response", id, result}` or `{..., error}`:

| Method | Params | Result |
| --- | --- | --- |
| `file` | | the item's file, an ArrayBuffer |
| `libraryFile` | `path`, relative to a library folder | another file of the item's library that the library kind would index, such as a parent set |
| `storage.list` | `prefix` | `{keys: [{key, size, updatedAt}], used, limit}` |
| `storage.get` | `key` | an ArrayBuffer, or `null` |
| `storage.put` | `key`, `value` (ArrayBuffer or string) | |
| `storage.delete` | `key` | |
| `progress` | `positionMs`, `finished` | |
| `exit` | | the web player goes back |

Storage is per person (each profile has its own) and per plugin: up to
32 MB a key and 512 MB in all. Keys are letters, digits, `.`, `_`, `-` and
`/`. Removing the plugin deletes it.

## Testing a plugin

`internal/plugin/testdata/testkit` is a small plugin that Matinee's own
tests install, scan with and send events to; it's a good starting point.
To try yours, zip it, then in Settings → Plugins choose **Add plugin → From
a file**.

## The official list

**Add plugin** lists the official plugins from
`https://matinee.yeix.io/plugins.json` (`site/public/plugins.json` in this
repository). A server can point elsewhere with `MATINEE_PLUGIN_CATALOG`, or
turn the list off with `off`.
