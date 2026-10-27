---
title: Troubleshooting
description: Fixes for the things that most often go wrong, from missing posters to apps that can't find the server.
sidebar:
  order: 1
---

## Posters and descriptions are missing

- Check that the server has a TMDB key (`MATINEE_TMDB_KEY`). See [Posters and details](/docs/getting-started/posters-and-details/).
- After adding the key, restart Matinee. The next scan fills in the artwork, or click **Scan** on a library to start one now.
- A title that was matched wrongly or not at all usually has an unusual file or folder name. Rename it to `Title (Year)`, as in [Organizing your media](/docs/getting-started/organizing-media/).

## A new file doesn't show up

Matinee notices new files by itself within a minute or so; on network shares it checks every 30 seconds by default. If something still hasn't appeared, click **Scan** on the library under **Settings → Libraries**. See [New files](/docs/server/new-files/) for how this works and how to tune it.

## The library folder is empty in the picker

The folder picker shows what the *server* can see. In Docker, your media has to be mounted into the container (`-v /path/to/movies:/media/movies:ro`); on Kubernetes, under `media:` in the values. With a package install, check that the `matinee` user can read the folder.

## The apps can't find the server

- The phone or TV has to be on the same network as the server.
- In Docker, the server can only announce itself with `--network host`.
- You can always type the server's address instead, such as `http://192.168.1.20:7171`.

See [Home network](/docs/server/home-network/).

## Playback stutters or keeps buffering

- Look at the title's [File details](/docs/watching/title-pages/#file-details). A 4K HEVC or HDR file usually has to be converted for a browser, which is heavy work without a GPU.
- **Settings → Transcoding** shows whether a GPU is in use. See [Hardware transcoding](/docs/server/transcoding/).
- Away from home, your home connection's upload speed is the limit. For a trip, [download](/docs/apps/downloads/) in the apps before you go.

## Remote access doesn't work

**Settings → Remote access** says what's in the way. The usual causes are UPnP turned off on the router (turn it on, or [forward a port yourself](/docs/server/remote-access/#forwarding-a-port-yourself)), a second router in front of yours, or an internet provider that shares one public address between customers. See [Remote access](/docs/server/remote-access/).

## I forgot my password

Another admin can reset it under **Settings → People**. That's why it's worth making a second person in the house an admin.

## Something else

Search the [issues on GitHub](https://github.com/yeixio/matinee/issues) or open a new one. Include the server's version (at the bottom of **Settings**), how you installed it, and what you see in the server's log.
