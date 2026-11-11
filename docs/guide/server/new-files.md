---
title: New files
description: How Matinee notices files you add, rename or delete, on local disks and network shares.
sidebar:
  order: 4
---

Copy a film into a library folder and it shows up in Matinee a few seconds after the copy finishes. Rename or delete files and the library follows. Only the changed file or folder is scanned, so this stays quick on big libraries.

![The New files settings, with Automatic picked](../images/new-files.webp)

**Settings → Libraries → New files** has three ways to watch:

| Mode | How it works |
| --- | --- |
| **Automatic** (default) | Folders on the server's own disks announce changes the moment they happen. Network shares (SMB, NFS) can't, so Matinee checks those every 30 seconds by looking at each folder's modification time, which is cheap even on a big library. |
| **Check on a timer** | Every folder is checked every 30 seconds. Pick this if new files on a disk don't show up by themselves. |
| **Off** | New files show up at the next full rescan, or when you press **Scan** next to a library. |

Each library under **Settings → Libraries** says how it's watched.

<!-- since: v0.2.0 -->
## Upgraded and renamed files

When a file is swapped for a better copy (say a download tool replaces a 1080p film with a 4K one), renamed, or moved into its own folder, Matinee sees it's the same title and moves the new file onto the existing one. Everyone keeps their watched marks, resume points, ratings, watch lists, collections, playlists, downloads and found subtitles.

Matinee knows it's the same title when it's:

- a movie with the same title and year,
- an episode with the same show, season and episode number,
- a song or audiobook part with the same album or book, number and title.

Download tools often delete the old copy first and move the new one in a few minutes later. So when a watched folder loses a file, Matinee keeps it listed for 10 minutes, waiting for the file that replaces it, before it removes it. A full rescan or the **Scan** button removes missing files straight away, after looking for what replaced them.

Intro and credits markers and seek bar pictures are made again in the background when the new file is different.
<!-- /since -->

## Full rescans

<!-- until: v0.2.4 -->
As well, Matinee rescans every library in full every 15 minutes to catch anything else. Change that with `MATINEE_SCAN_INTERVAL` (such as `1h`), or `0` to turn it off.
<!-- /until -->
<!-- since: v0.2.4 -->
As well, Matinee rescans every library in full every so often to catch anything watching can't, like a file replaced in place on a share. Pick how often under **Full rescans** in **Settings → Libraries → New files**: every 15 or 30 minutes, every 1, 6, 12 or 24 hours, or **Never**. The next rescan's time shows below it, and a new pick counts from when you make it. **Watch for new files** doesn't change this; turning it **Off** leaves full rescans on.

Each rescan reads every folder and file of every library, so on a big library on a network share it's real work for the share. If the share or the web player slows down every so often, or new files show up quickly anyway, pick a longer interval. The default, every 6 hours, is plenty for most homes.

Until you pick one, the server uses `MATINEE_SCAN_INTERVAL`, which is every 6 hours unless it's set (every 15 minutes before v0.2.4).
<!-- /since -->

<!-- since: v0.2.4 -->
## Stopping a scan

While a library is being scanned, whatever started it, **Settings → Libraries** shows **Stop scan** in place of **Scan** next to it, and so does its row under **Background jobs** in [Server health](/docs/server/server-health/#background-jobs). Hover over the button to see what started the scan.

Stopping keeps everything the scan already added or updated, and removes nothing: files it didn't get to are looked at by the next scan. Stopping a scheduled full rescan stops it for that library only; the rest are still rescanned, and the schedule carries on. To stop full rescans altogether, pick **Never** under **Full rescans**.

If a network share stopped answering, the scan stops as soon as the share answers or the computer gives up on it, which can take a minute or two.
<!-- /since -->

## Big libraries on slow shares

`MATINEE_WATCH_POLL_INTERVAL` sets how often network shares are checked (default `30s`). A big library on a slow share is checked less often by itself, so the share stays responsive.
