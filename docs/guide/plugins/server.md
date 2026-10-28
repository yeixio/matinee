---
title: The server part
description: Write a plugin's server part with the Go SDK, or in any language that builds to WebAssembly, to file library items, look up details and hear events.
since: v0.1.1
sidebar:
  order: 4
---

The server part is a WebAssembly module that runs inside Matinee. Matinee calls it to file the items of a library, to look up details, and to tell it about events. Write it in Go with the SDK, or in [another language](#other-languages).

```sh
go get github.com/yeixio/matinee/pluginsdk@latest
```

## The sandbox

Each call runs in a sandbox, [wazero](https://wazero.io), that sees only:

| What | Where the plugin sees it |
| --- | --- |
| The folders of the library being scanned, read only | At their own paths, such as `/media/games`. A Windows folder like `C:\Games` is `/C/Games`. |
| The plugin's own package, read only | `plugin.PackageDir` (`/_matinee/plugin`) |
| A folder of the plugin's own, to keep things in | `plugin.DataDir` (`/_matinee/data`) |
| The network | Only through `plugin.Fetch`, only over https, and only to the hosts in [`permissions.network`](/docs/plugins/manifest/#permissions) |

Each call can use 1 GB of memory and run for two minutes. A call that panics or runs out of time or memory fails on its own. Matinee carries on, and starts a fresh copy of the plugin for the next scan.

Use the `os`, `io/fs` and `path/filepath` packages to read files, as in any Go program. The paths Matinee hands over work as they are.

## Registering handlers

A Go plugin is a `main` package that registers its handlers in `init`, and has an empty `main`:

```go
package main

import plugin "github.com/yeixio/matinee/pluginsdk"

func init() {
	plugin.Register(plugin.Handlers{
		Index:  index,  // files a library's files, for libraryKinds
		Lookup: lookup, // looks up details, for metadata
		Event:  event,  // hears events, for events
	})
}

func main() {}
```

Leave out the handlers you don't need. Build it as a WASI reactor, or let `matinee-plugin build` do it:

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o server.wasm .
```

## Filing files: Index

During a scan of a library of one of your kinds, Matinee calls `Index` once for each file whose extension the kind lists.

```go
func index(r plugin.IndexRequest) (plugin.IndexResult, error)
```

`IndexRequest` has:

| Field | |
| --- | --- |
| `Library` | The library's `ID`, `Name` and `Kind`. |
| `Root` | The library folder the file was found in. A library can have several. |
| `Path` | The file. |
| `Size`, `Mtime` | Its size in bytes and when it last changed, in seconds since 1970. |
| `Config` | The plugin's [settings](/docs/plugins/manifest/#settings), as the admin filled them in. |

Return `plugin.IndexResult{Item: item}` to file the file, or an empty result to skip it, for instance when it's part of something another file stands for. An error is logged as a warning and the file is left out. Empty files and files and folders whose names start with `.` are never sent.

Once a file is filed, Matinee asks about it again only when it changes, so `Index` can take its time. Files that were skipped or failed are asked about again at the next scan. To read a large list once and use it for every file, load it with `sync.Once` from `PackageDir`. One copy of the plugin answers all of a scan's files in turn.

### Item

| Field | |
| --- | --- |
| `Kind` | Required. One of the library kind's `itemKinds`. |
| `Title` | Required. Up to 500 characters. |
| `SortTitle` | What to sort by, if not the title. |
| `Year` | |
| `Index` | A number to sort by within its parent, such as a track or chapter number. |
| `Overview` | A description, up to 10,000 characters. |
| `Genres` | Up to 20. |
| `Rating` | Out of 10. |
| `Facts` | Up to 20 labelled lines shown on the title's page, such as *Manufacturer: Namco*. A label is up to 100 characters, a value up to 500. Facts without both are left out. |
| `Poster`, `Backdrop` | A picture: a file the plugin can see, such as `snap/pacman.png` next to the file, or an `https://` URL. Anything else is left out. |
| `Parents` | Containers the item sits in, outermost first, such as a system and then a series. Each has a `Kind` (one of the plugin's item kinds), a `Title`, and optionally a `Year`, `Index`, `Poster` and `Backdrop`. Matinee makes each container once and shares it between every item that names the same one. |
| `Data` | Anything the player needs to know, such as which emulator to load. It's kept with the item as JSON, up to 64 KB, and handed to the player as `pluginData`. |

Matinee refuses an item whose kind isn't one of the library kind's `itemKinds`, or whose parents aren't the plugin's kinds. `matinee-plugin scan` shows these problems for each file.

## Looking up details: Lookup

If `plugin.json` lists item kinds under `metadata`, Matinee calls `Lookup` after a scan for each item of those kinds that hasn't been looked up yet.

```go
func lookup(r plugin.LookupRequest) (*plugin.Details, error)
```

`LookupRequest` has the item's `Kind`, `Title`, `Year`, the `Data` that `Index` kept, and `Config`. Return the `Details` you found: `Overview`, `Genres`, `Rating`, `Facts`, `Poster` and `Backdrop`, which must be `https://` URLs here. Return `nil, nil` for no match.

`Lookup` runs without the library's folders, and with the network. It's for what can only be found online. What a file says about itself belongs in `Index`.

```go
func lookup(r plugin.LookupRequest) (*plugin.Details, error) {
	body, err := plugin.Get("https://api.example.com/games?title=" + url.QueryEscape(r.Title))
	if err != nil {
		return nil, err
	}
	var found struct{ Summary, Cover string }
	if err := json.Unmarshal(body, &found); err != nil || found.Summary == "" {
		return nil, err
	}
	return &plugin.Details{Overview: found.Summary, Poster: found.Cover}, nil
}
```

## The network: Fetch

```go
res, err := plugin.Fetch(plugin.Request{
	Method:  "POST",
	URL:     "https://api.example.com/search",
	Headers: map[string]string{"Authorization": "Bearer " + r.Config["apiKey"]},
	Body:    []byte(`{"q":"pac-man"}`),
})
// res.Status, res.Headers (lower-case names), res.Body
```

`plugin.Get(url)` is the short form for a GET that wants a 200. Only `https://` URLs on hosts in `permissions.network` work. Answers are limited to 32 MB, and each request to 30 seconds. Matinee sends a `User-Agent` of `Matinee plugin <id>/<version>` unless you set one.

## Events

If `plugin.json` lists events, Matinee calls `Event` when one happens:

| Type | When | What's set |
| --- | --- | --- |
| `item.added` | A scan added an item, in any library | `Item` |
| `library.scanned` | A scan of a library finished | `Library` |
| `playback.started` | Someone started playing something | `Item`, `User`, `Played` |
| `playback.stopped` | They stopped, or finished it | `Item`, `User`, `Played` (with `Finished` when they got to the end) |

`Item` has the item's `ID`, `LibraryID`, `Kind`, `Title` and `Year`, plus `ShowTitle`, `Season` and `Episode` for an episode and `TMDBID` for movies and shows. `User` is the account or profile, with `ID` and `Name`. `Played` has `PositionMs`, `DurationMs` and `Finished`. Events cover the whole server, not just the plugin's own items, so a plugin can, for example, keep a watch diary of everything someone plays.

Events are delivered one at a time, in order. Matinee never waits for a plugin, so a plugin that falls far behind misses events. An event's error is logged.

## Keeping things: DataDir

`plugin.DataDir` is the plugin's own folder, kept across restarts and updates. It's removed when the plugin is. Use it for caches and for anything learned along the way. For what a person saves in the player, such as a game's progress, use the player's [storage](/docs/plugins/player/#storage) instead, which is kept per person.

## Logging

```go
plugin.Debug("checking %s", r.Path)
plugin.Info("found %d games", n)
plugin.Warn("no artwork for %s", name)
plugin.Error("bad list: %v", err)
```

Lines go to Matinee's log, marked `plugin=<id>`. Whatever the plugin prints to standard output or standard error goes there too.

## Other languages

Any language that builds a WASI preview 1 **reactor** module (one with `_initialize` and no `main` to run) can write a server part: Rust, C, Zig, AssemblyScript, TinyGo and others. The module talks to Matinee through four exports and three imports, with JSON messages in its memory.

The module exports:

| Export | Signature | |
| --- | --- | --- |
| `_initialize` | `() -> ()` | Run once when the module starts. |
| `matinee_malloc` | `(size i32) -> i32` | Memory for Matinee to write a request into. |
| `matinee_free` | `(ptr i32) -> ()` | Matinee is done with a buffer the plugin handed it. |
| `matinee_handle` | `(ptr i32, len i32) -> i64` | Answer the request at `ptr`, a buffer from `matinee_malloc` that's the plugin's to free. The result is the answer's address in the high 32 bits and its length in the low 32. Matinee reads the answer, then calls `matinee_free` on it. |

Requests are `{"method": "...", "params": {...}}`. Answers are `{"result": ...}` or `{"error": "..."}`:

| Method | Params | Result |
| --- | --- | --- |
| `scan.index` | `library {id, name, kind}`, `root`, `path`, `size`, `mtime`, `config` | `{"item": Item}`, or `{}` to skip the file |
| `metadata.lookup` | `kind`, `title`, `year`, `data`, `config` | `Details`, or `null` for no match |
| `event` | `type`, `time` (milliseconds since 1970), `item`, `library`, `user`, `played`, `config` | `null` |

`Item` is `kind`, `title`, `sortTitle`, `year`, `index`, `overview`, `genres`, `rating`, `facts` (each `label` and `value`), `poster`, `backdrop`, `parents` (each `kind`, `title`, `year`, `index`, `poster`, `backdrop`) and `data`. The Go SDK's [types](https://github.com/yeixio/matinee/blob/main/pluginsdk/plugin.go) are the reference for every field's JSON name.

The module imports, from the module named `matinee`:

| Import | Signature | |
| --- | --- | --- |
| `log` | `(level i32, ptr i32, len i32)` | Write a line to the log: 0 debug, 1 info, 2 warning, 3 error. |
| `fetch` | `(ptr i32, len i32) -> i32` | Make an HTTP request and return the length of the answer. |
| `take` | `(ptr i32)` | Copy the last `fetch`'s answer into a buffer of that length. |

A fetch request is `{"method", "url", "headers", "body"}` and its answer is `{"status", "headers", "body"}` or `{"error"}`, with bodies base64 encoded.

`matinee-plugin scan`, `lookup`, `event` and `try` run any module that follows these rules, so you can test a plugin in another language with them too.
