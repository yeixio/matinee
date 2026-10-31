---
title: How plugins work
description: What a Matinee plugin can add, the parts it's made of, and the tools for writing one.
since: v0.1.1
sidebar:
  order: 1
---

A plugin adds something to Matinee without changing Matinee itself. People install plugins in **Settings → Add-ons → Plugins** ([see the user guide](/docs/server/plugins/)). This part of the guide is for people who write them.

## What a plugin can do

A plugin can do any of these four things, or several:

- **Add a kind of library.** Someone adding a library picks your kind, such as *Arcade* or *Stories*, and points it at a folder. Matinee walks the folder and asks your plugin what each file is: a title, a year, a poster, facts, and the containers it sits in.
- **Look up details** for your own kinds of titles after a scan, from a website you name.
- **Play your titles in the web player**, with a web page of your own: an emulator, a reader, a viewer.
- **Hear about what happens on the server**: a title added, a scan finished, someone starting or stopping a title.

Matinee does the rest: your titles get a library page and title pages with posters and facts, and Matinee keeps track of who played what.

## The parts of a plugin

A plugin is a zip file named `<id>.matinee-plugin` with these files in it:

| File | What it is |
| --- | --- |
| `plugin.json` | The manifest: the plugin's name and version, and what it adds. See [plugin.json](/docs/plugins/manifest/). |
| `server.wasm` | The **server part**, a WebAssembly module. It decides what files are and looks up details. See [The server part](/docs/plugins/server/). |
| `player/` | The **player**, a web page and whatever it loads. See [The player](/docs/plugins/player/). |
| `icon.png` | The icon shown in Settings. |

A plugin has a server part, a player, or both.

### Walled off by design

Plugins come from anyone, so Matinee keeps each one in its own sandbox:

- The server part runs inside Matinee in a WebAssembly sandbox. It sees only the folders of the library it's scanning, read only, and a folder of its own. It reaches only the websites its manifest lists, and only after an admin agrees.
- The player runs in a frame with no origin of its own. It can't see who's signed in or call Matinee's API. It asks the web player for the title's file and for storage, and the web player answers as whoever is watching.

You don't need to do anything for this. It's how your plugin runs everywhere.

## The tools

- **The Go SDK**, [`github.com/yeixio/matinee/pluginsdk`](https://github.com/yeixio/matinee/tree/main/pluginsdk), for the server part. It comes with `plugintest` for testing. A server part can be written in [other languages](/docs/plugins/server/#other-languages) too.
- **`matinee-player.js`**, with TypeScript types, for the player.
- **The `matinee-plugin` tool.** It starts a new plugin, runs its server part on a folder of files, previews its player in a browser without a Matinee server, and builds the package.

The SDK and `matinee-player.js` are MIT licensed, so your plugin can use any license you like.

```sh
go install github.com/yeixio/matinee/cmd/matinee-plugin@latest
```

## Examples

- **[Stories](https://github.com/yeixio/matinee/tree/main/pluginsdk/examples/stories)** files short stories kept as text files and comes with a reader that remembers where you got to. It's small, and it's what [Your first plugin](/docs/plugins/first-plugin/) builds.
- **[Arcade](https://github.com/yeixio/matinee/tree/main/plugins/arcade)** plays arcade games with MAME built to WebAssembly. It uses every part of the API: library folders with artwork, details looked up online, data for the player, files from elsewhere in the library, and saved states in storage.

Next, build [your first plugin](/docs/plugins/first-plugin/).
