---
title: When the internet is down
description: What keeps working on your Matinee server when the internet goes down, what waits for it, and how it catches up.
since: v0.2.0
sidebar:
  order: 2
---

Your server plays what's on its own disks, so an internet outage doesn't take your movies, shows and music away. Everything that doesn't need the internet keeps working at full speed.

## What keeps working

- Signing in, profiles and PINs. Accounts live on your server.
- Browsing, Home, search, collections, watch lists and ratings.
- Playing, including converting video for devices that need it, and hardware transcoding.
- Downloads to the phone and tablet apps on your home network.
- New files: they show up as usual, named from their file names.
- Every admin setting, and **Settings → Server health**.
- Live TV from a tuner on your home network, and Radarr, Sonarr or Home Assistant at home.

## What waits for the internet

These wait quietly and catch up once the connection is back. You don't need to do anything:

- Posters, descriptions, cast and crew for titles added during the outage. Titles look them up again within a minute or so of the internet coming back, without a rescan.
- Posters that were never downloaded show a placeholder until then.
- Subtitle search, online lyrics, trailers from TMDB and person biographies.
- Podcast feeds and episodes, internet radio, and Live TV channels from the internet.
- [Remote access](/docs/server/remote-access/) from outside your home, and [alerts](/docs/server/alerts/) by email, Discord or ntfy.

Ask for one of these meanwhile, such as searching for subtitles, and Matinee says there's no internet connection right now and that your server is fine, rather than showing an error.

## How Matinee stays quick

When a site such as TMDB stops answering, Matinee remembers it and stops asking for a while instead of making every page or scan wait for an answer that won't come. It tries again after 30 seconds, then waits longer each time, up to five minutes. As soon as one site answers, Matinee knows the internet is back and looks up what waited.

**Settings → Server health** says **No internet connection**, for how long, and which sites aren't answering, instead of listing every lookup that couldn't be made. A service on your home network that's off, such as Radarr, is listed there too.
