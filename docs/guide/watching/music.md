---
title: Music
description: Browse artists, albums and songs, line up a queue, and keep music playing while you browse.
sidebar:
  order: 7
---

A music library opens on an overview: albums you played lately, albums added lately, and your artists. The tabs along the top list every **Artist**, **Album** or **Song**, and **Shuffle all** plays the whole library in a random order.

![The Music library's overview, with recently added albums and the artists](../images/music.webp)

## Albums and artists

An album's page lists its songs. Click the big **Play** button to play it from the top, **Shuffle** to mix it up, or any song to play from there. The song that's playing has moving bars beside it. Below the songs are the artist's other albums.

![An album page with its songs, and the player along the bottom](../images/album.webp)

An artist's page has their albums, newest first, and plays or shuffles every song they have.

![An artist page with their albums](../images/artist.webp)

<!-- since: v0.1.1 -->
## Music keeps playing while you browse

The player sits along the bottom of every page, so you can look around Matinee, or pick the next album, without stopping the song. It has shuffle, back, play, next, repeat, the volume, and a bar to move around the song. Repeat goes from off, to the whole queue, to just this song.

On a phone, the player sits above the tabs with the song, play and next. Tap it for the full player.

The media keys on your keyboard, headphones and lock screen work too, and show the song and its cover. Matinee remembers what you were listening to: come back later and it's ready where you stopped.

## The full player and the queue

Click the cover in the player (or the queue button beside the volume) for the full player: the cover large, the controls, and the queue of what's playing next. Drag songs in the queue to change the order, click one to jump to it, or remove it with **×**. **Clear** empties the rest of the queue. Press **Esc** or the arrow at the top to go back.

![The full player, with the queue beside the cover](../images/now-playing.webp)

On a phone, the button at the top right switches between the cover and the queue.

## Play next and Add to queue

The **⋯** button on any song, album or artist has:

- **Play next**, which puts it right after the song that's playing.
- **Add to queue**, which puts it at the end.
- **Go to album** and **Go to artist**.

Shuffle mixes what's left in the queue and keeps the song that's playing; turning it off puts the queue back in order.

## On the phone apps

The [phone apps](/docs/apps/phone-and-tv/) have the same library, album and artist pages, a player above the tabs, and a full player with the queue. Music keeps playing when you lock the phone or switch apps, with the song on the lock screen, and downloaded albums play offline.

<!-- /since -->

## Covers and artist photos

Matinee uses `cover.jpg` or `folder.jpg` in an album's folder, or a cover embedded in the music files, and `artist.jpg` in an artist's folder. Anything missing comes from MusicBrainz, the Cover Art Archive and Wikidata, which need no key. To use only your own files, set `MATINEE_MUSICBRAINZ=false`.

## Tags

Artist, album, track number and title come from the tags in your files, so tag them with your favorite tagger before adding them. Without tags, Matinee uses the [folder and file names](/docs/getting-started/organizing-media/#music).
