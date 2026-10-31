---
title: Welcome to Matinee
description: What Matinee is, what you need to run it, and where to start in this guide.
---

Matinee is a media server you run yourself. Point it at the folders where you keep movies, TV and music, and it turns them into a library that looks after itself: artwork and descriptions, cast and crew, what you've watched and where you stopped, on every screen in the house.

![Matinee's Home page in a browser](./images/home.webp)

Matinee is free and open source (AGPL-3.0). Your library, your watch history and your accounts stay on your server. There are no ads, and nobody needs an account anywhere else to use it.

## What you need

- **A computer that stays on**: a Linux box, a NAS, a Mac, a Raspberry Pi 4 or 5, or a Kubernetes cluster. Matinee is one small program and runs comfortably on modest hardware. A GPU helps when a lot of files need converting, but isn't required.
- **Your media in folders** the server can read, on its own disks or on a network share.
- **A browser.** The web player is built in. The [phone and TV apps](/docs/apps/phone-and-tv/) are optional.

## Where to start

1. [Install Matinee](/docs/getting-started/install/) with Docker, a Linux package, Homebrew or Helm.
2. [Create the admin account and add your libraries](/docs/getting-started/first-run/).
3. [Name your files](/docs/getting-started/organizing-media/) so Matinee recognizes them.
4. [Add a TMDB key](/docs/getting-started/posters-and-details/) for posters, descriptions, cast and crew.

Then [invite your household](/docs/household/people/), [set up profiles](/docs/household/profiles/) for the kids, and get the [apps](/docs/apps/phone-and-tv/) on your phone and TV.

## Finding your way around this guide

- **Watching** covers everything people do day to day: Home, browsing and filters, title pages, the player, the watch list, collections, music, audiobooks and podcasts.
- **Phone and TV apps** covers the Matinee apps, signing in a device with a code, and offline downloads.
- **Your household** covers accounts, profiles and kids mode, and themes.
- **Running the server** is for whoever looks after Matinee: the home network, remote access, hardware transcoding, how new files are found, media analysis, Radarr and Sonarr, Kubernetes and every setting.

The version picker at the top of the page switches between the guide for each release of Matinee. "Next" describes what's on `main` and not released yet.
