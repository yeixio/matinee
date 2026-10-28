---
title: Publishing
description: Package a plugin, put it online for people to install from a link, and get it into Matinee's official list.
since: v0.1.1
sidebar:
  order: 7
---

## Build the package

```sh
matinee-plugin build
```

This builds `server.wasm`, checks the plugin the way Matinee does when installing it, and writes `<id>.matinee-plugin`. The package holds `plugin.json`, the server module, the icon, each player's folder, and any `LICENSE`, `COPYING`, `NOTICE` and `README` files at the top of the plugin's folder. Add anything else the server part reads from its package, such as a data file, with `-include`:

```sh
matinee-plugin build -include games.json.gz -include licenses
```

A package can be up to 1 GB, with up to 20,000 files and 2 GB unpacked.

## Share it

Put the `.matinee-plugin` file anywhere people can download it over https. A release on GitHub, GitLab or Codeberg works well, since each version gets its own address. Admins install it with **Settings → Plugins → Add plugin → From a link**, and the link they paste is the file's address. They can also download it and use **From a file**.

Say on your plugin's page:

- what it adds, with a screenshot or two;
- what files it expects, and how to lay them out;
- which websites it reaches and why, since admins see the list before turning it on;
- which version of Matinee it needs.

For a new version, raise `version` in `plugin.json`, build, and publish the new file. People install it the same way, and Matinee replaces the old version.

To build releases in CI, run `go test ./...` and `matinee-plugin build`, then attach the package to a release. The Arcade plugin's [workflow](https://github.com/yeixio/matinee/blob/main/.github/workflows/arcade.yml) does this, and also plays a game in a headless browser before releasing.

## The official list

**Add plugin** in Settings opens with a list of official plugins, which install in one click. Settings also offers an update when the list has a newer version than the one installed. The list is [`site/public/plugins.json`](https://github.com/yeixio/matinee/blob/main/site/public/plugins.json) in Matinee's repository, published at `https://matinee.yeix.io/plugins.json`:

```json
{
  "plugins": [
    {
      "id": "stories",
      "name": "Stories",
      "description": "Read short stories kept as text and Markdown files. Adds a Stories library.",
      "version": "1.0.0",
      "url": "https://github.com/you/matinee-stories/releases/download/v1.0.0/stories.matinee-plugin",
      "homepage": "https://github.com/you/matinee-stories"
    }
  ]
}
```

To propose a plugin for the list, open a pull request that adds its entry. To release a new version, open one that changes `version` and `url`. A plugin is added when:

- **It installs and works on the current Matinee release.** Say in the pull request how to try it, with files anyone can get legally.
- **Its source is public**, under an open source license, and the package is built from it.
- **It reaches only the websites it needs**, each for a reason the plugin's page explains. It doesn't track the people using it.
- **It doesn't come with content people need a license for,** such as games, films or books. The plugin plays what people already have.
- **Its name and description say plainly what it does,** and its `id` doesn't take a name another plugin is known by.
- **Someone will look after it.** A plugin whose versions stop installing on new Matinee releases may be taken off the list.

A server can use a list of its own with `MATINEE_PLUGIN_CATALOG=<url>`, or show none with `MATINEE_PLUGIN_CATALOG=off`. That's useful for testing a list, or for a set of plugins your household uses.

## Licenses

The Go SDK, `plugintest` and `matinee-player.js` are MIT licensed, so a plugin can use any license. If your plugin includes other people's code, such as an emulator, follow that code's license and put its license file in the package. The Arcade plugin, for example, carries MAME's GPL license and says where to get MAME's source.
