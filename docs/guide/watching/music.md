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

<!-- since: v0.1.3 -->
## The visualizer

The visualizer fills the screen with the song that's playing: bars that jump with it, a halo round the cover, or rings that pulse with the beat. Open it with the visualizer button at the top of the full player. The left and right arrow keys, or the button at the top right, change the look, and Matinee remembers the one you pick. **Space** pauses, and **Esc** closes it. The buttons and the song's name fade away after a few seconds; move the mouse to bring them back.

![The visualizer's bars](../images/visualizer.webp)

![The halo round the cover](../images/visualizer-halo.webp)

On a phone, tap the visualizer button at the top of the full player. Tap the look's name to change it, and tap anywhere to show or hide the buttons.

The visualizer moves in time with the music on every device, even a TV, because the server listens to each song and works out how loud its bass, middle and treble are, moment by moment. It does that the first time anyone opens the visualizer on a song (and for the next song in the queue while it's open), which takes a moment, then keeps the result. Until it's ready, and for songs a phone plays offline, a slow wave fills in.

## On a TV

Apple TV and Android TV have the same music library as the browser, with **Overview**, **Artists**, **Albums** and **Songs** along the top and **Shuffle all**. An album or artist page has **Play** and **Shuffle**, and selecting a song plays the album from there. Hold select on a song for **Play next**, **Add to queue**, **Go to album** and **Go to artist**.

The music keeps playing while you browse. What's playing shows at the top of the screen, next to the server's name; select it for the player. The player has the cover, how far along the song is, shuffle, back, play, next and repeat, and the queue in a row below it, where selecting a song plays it now. The play/pause button on the remote plays and pauses the music from any screen.

![The music player on a TV](../images/app-tv-music.webp)

**Visualizer** in the player opens the visualizer; left and right on the remote change its look, and play/pause or select pauses.

![The visualizer on a TV](../images/app-tv-visualizer.webp)

<!-- /since -->

## Covers and artist photos

Matinee uses `cover.jpg` or `folder.jpg` in an album's folder, or a cover embedded in the music files, and `artist.jpg` in an artist's folder. Anything missing comes from MusicBrainz, the Cover Art Archive and Wikidata, which need no key. To use only your own files, set `MATINEE_MUSICBRAINZ=false`.

<!-- since: v0.1.3 -->

Cover names can be in any case and can be `.jpg`, `.jpeg`, `.png` or `.webp`: `Folder.JPG`, `front.png`, `AlbumArt_…_Large.jpg` and an image with "cover" or "front" in its name all count, and so does the only image in a folder. Back covers, disc scans and booklet pages don't. For an album split into `CD 1` and `CD 2` folders, put the cover in the album's folder above them.

Albums tagged as a compilation without an album artist are filed under **Various Artists**, so their songs stay on one album with one cover.

### Finding missing covers

When covers are missing, check the album's folder first: a cover added after the album was scanned is picked up on the next scan. To look again for everything that's missing, an admin opens **Settings**, finds the music library under **Libraries**, and clicks **Find artwork**. Matinee scans the library's folders and looks up every artist and album without a picture of its own on MusicBrainz again, even ones it couldn't match before. Covers show up as they're found; with a big library that takes a while, since MusicBrainz answers about one question a second.

![Find artwork on a music library in Settings](../images/find-artwork.webp)

Online lookups go by the album and artist names in your tags. An edition note like "(Deluxe Edition)" or "[2011 Remaster]" is tried with and without, and an album without a cover of its own in the Cover Art Archive takes the cover of one of its releases. If an album still has none, fix its tags, or add a `cover.jpg`.

<!-- /since -->

## Tags

Artist, album, track number and title come from the tags in your files, so tag them with your favorite tagger before adding them. Without tags, Matinee uses the [folder and file names](/docs/getting-started/organizing-media/#music).
