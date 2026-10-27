---
title: Music
description: Browse artists and albums, and play music that keeps going while you browse.
sidebar:
  order: 7
---

A music library lists artists. Open an artist for their albums, and an album for its tracks.

![The Music library with three artists](../images/music.webp)

Click **Play** to play an album from the top, **Shuffle** to mix it up, or click any track. The player sits at the bottom of the window and keeps playing while you browse the rest of Matinee, with buttons to skip back and forward.

![An album page with its tracks, and the music player along the bottom](../images/album.webp)

## Covers and artist photos

Matinee uses `cover.jpg` or `folder.jpg` in an album's folder, or a cover embedded in the music files, and `artist.jpg` in an artist's folder. Anything missing comes from MusicBrainz, the Cover Art Archive and Wikidata, which need no key. To use only your own files, set `MATINEE_MUSICBRAINZ=false`.

## Tags

Artist, album, track number and title come from the tags in your files, so tag them with your favorite tagger before adding them. Without tags, Matinee uses the [folder and file names](/docs/getting-started/organizing-media/#music).
