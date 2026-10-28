---
title: plugin.json
description: Every field of a plugin's manifest, what Matinee checks, and how versions and permissions work.
since: v0.1.1
sidebar:
  order: 3
---

Every plugin has a `plugin.json` at the top of its package. Matinee reads it when the plugin is installed and refuses a package whose manifest breaks a rule below. Run `matinee-plugin check` to check yours the same way.

```json
{
  "id": "arcade",
  "name": "Arcade",
  "version": "1.0.0",
  "api": 1,
  "description": "Play arcade games in the web player with MAME.",
  "author": "The Matinee authors",
  "license": "AGPL-3.0-or-later",
  "homepage": "https://matinee.yeix.io/docs/server/arcade/",
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
  "settings": [{ "key": "apiKey", "label": "API key", "description": "From your account page.", "secret": true }],
  "permissions": { "network": ["raw.githubusercontent.com"] }
}
```

## About the plugin

| Field | | |
| --- | --- | --- |
| `id` | required | 2 to 40 lowercase letters, digits and dashes, starting with a letter or digit. It names the plugin's folder on the server and its place in the API. Installing a package with an id that's already installed replaces that plugin, so pick one that's yours: `stories`, not `plugin`. |
| `name` | required | What people see in Settings. |
| `version` | required | Shown in Settings. Use `major.minor.patch`, such as `1.2.0`, and raise it with every release. |
| `api` | required | The plugin API version the plugin is written for: `1`. Matinee lists a plugin written for an API it doesn't run, but won't turn it on. |
| `description` | | One or two sentences, shown under the name. |
| `author`, `license`, `homepage` | | Shown in Settings. `license` is an [SPDX expression](https://spdx.org/licenses/), such as `MIT` or `GPL-2.0-or-later`. `homepage` is where people go for help. |
| `icon` | | A square PNG or JPEG in the package, 256 × 256 pixels or more. |

## What the plugin adds

| Field | | |
| --- | --- | --- |
| `server` | | The server part, a WebAssembly module in the package, usually `server.wasm`. `libraryKinds`, `metadata` and `events` need one. |
| `libraryKinds` | | Kinds of library the plugin adds. [See below](#library-kinds). |
| `metadata` | | Item kinds the server part looks up details for after a scan. Only the plugin's own kinds, from its `libraryKinds`. |
| `events` | | What the server part wants to hear about: `item.added`, `library.scanned`, `playback.started`, `playback.stopped`. See [events](/docs/plugins/server/#events). |
| `players` | | Pages that play items. Each has `itemKinds` and an `entry`, the page in the package, such as `player/index.html`. Every file in the entry's folder is served to the web player, and nothing else in the package is. Players play only plugin item kinds, not movies, shows or music. |
| `settings` | | What an admin fills in. [See below](#settings). |
| `permissions` | | What the server part may reach beyond its own files. [See below](#permissions). |

A plugin needs a server part, a player, or both.

### Library kinds

| Field | | |
| --- | --- | --- |
| `name` | required | The kind's name, such as `arcade`: 2 to 31 lowercase letters, digits and dashes, starting with a letter. It must be unique on the server: two plugins can't add the same kind. |
| `label` | required | Shown when adding a library, such as *Arcade*. |
| `description` | | Shown under the label: what goes in such a library. |
| `extensions` | required | The file extensions the server part is asked about, such as `[".zip", ".7z"]`. Matinee skips other files without asking, and the player can only fetch files with these extensions. |
| `itemKinds` | required | The kinds of item the plugin files in such a library, playable ones first, followed by the containers it puts them in, such as `["story", "book"]`. Same rules as `name`. |

Kind names can't be Matinee's own: `movies`, `tv`, `music`, `movie`, `show`, `season`, `episode`, `artist`, `album`, `track`, `trailer`, `collection` and `person`.

### Settings

Each setting is a value an admin fills in on the plugin's page in Settings, such as an API key. The server part gets every setting's value with every call, in `Config`.

| Field | | |
| --- | --- | --- |
| `key` | required | A letter, then up to 39 letters, digits and underscores, such as `apiKey`. |
| `label` | required | Shown next to the field. |
| `description` | | Shown under the field. |
| `secret` | | `true` shows a password field. Matinee never sends a secret's value back to the browser. |

### Permissions

`permissions.network` lists the hosts the server part may fetch from, such as `["api.example.com"]`. `*.example.com` allows every subdomain of example.com, but not example.com itself. Fetches go over https only, and a redirect has to stay on the listed hosts.

An admin sees these hosts before turning the plugin on: *It will be able to reach…* A plugin that lists none never goes on the internet. If a new version lists more hosts than the one installed, Matinee turns it off until an admin turns it on again and agrees.

The player isn't limited this way. It's a web page in a sandbox, and it can load what any page can.

## Versions and updates

Installing a package whose `id` is already installed replaces the old version. The plugin keeps its settings, its data folder and what its players stored, and stays on unless it now asks for more hosts. Items already in libraries stay as they were until the next scan, so a new version should still understand the `Data` an older one kept with items.
