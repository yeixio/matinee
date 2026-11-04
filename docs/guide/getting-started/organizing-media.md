---
title: Organizing your media
description: How to name movie, TV, music and audiobook folders and files so Matinee recognizes them, plus artwork, subtitles and trailer files.
sidebar:
  order: 3
---

Matinee names everything from your folder and file names, then looks the titles up online. A few simple conventions get you a tidy library with the right artwork.

## Movies

Give each movie its own folder named `Title (Year)`, with the video inside:

```
Movies/
├── The Long Matinee (2024)/
│   ├── The Long Matinee (2024).mkv
│   ├── The Long Matinee (2024).en.srt
│   ├── The Long Matinee (2024)-trailer.mp4
│   └── poster.jpg
└── Harbor Lights (2019)/
    └── Harbor Lights (2019).mp4
```

The year matters: it tells two films with the same title apart.

<!-- since: v0.3.0 -->
### Several versions of a movie

Keep as many copies of a movie as you like in its folder, such as a 4K copy for the TV and a 1080p one for phones, or the theatrical cut next to the extended one. Matinee shows them as one movie with a version for each file. Name the cut in the file name, after the year, and Matinee shows it on the version:

```
Movies/
└── Harbor Lights (2019)/
    ├── Harbor Lights (2019) 2160p.mkv
    ├── Harbor Lights (2019) 1080p.mp4
    └── Harbor Lights (2019) {edition-Director's Cut}.mkv
```

`{edition-…}` takes any name. Matinee also knows the usual ones written plainly, such as `Harbor Lights (2019) - Extended Edition.mkv`, `Harbor Lights (2019) [Theatrical].mkv` or `Harbor.Lights.2019.Directors.Cut.1080p.mkv`.

Two copies of an episode in its season folder, such as `Crumb Street - S01E01 720p.mkv` and `Crumb Street - S01E01 1080p.mkv`, are versions of that episode in the same way.

Watched marks, places to resume from, ratings, watch lists and collections belong to the movie or episode, whichever version you play. Deleting one copy keeps all of that with the copies left, and copies matched to the same movie online count as one even when their names differ. When a fixed match tells two files apart, they become two movies again. See [Versions](/docs/watching/title-pages/#versions) for picking one.
<!-- /since -->

## TV shows

One folder per show, a `Season N` folder per season, and the season and episode number in each file name:

```
TV/
└── Crumb Street/
    ├── poster.jpg
    ├── Season 1/
    │   ├── Crumb Street - S01E01.mkv
    │   └── Crumb Street - S01E02.mkv
    └── Season 2/
        └── Crumb Street - S02E01.mkv
```

`S01E02` can be anywhere in the name, so `Crumb Street - S01E02 - Dough Rises.mkv` works too. Specials go in `Season 0`.

## Music

One folder per artist, one per album, with the tracks inside:

```
Music/
└── Aurora Static/
    ├── artist.jpg
    └── Signal Fires/
        ├── cover.jpg
        ├── 01 Opening Credits.flac
        └── 02 Velvet Seats.flac
```

Matinee reads the tags in your music files (artist, album, track number, title) and falls back to the folder and file names. Covers can also be embedded in the files.

<!-- since: v0.1.2 -->
## Audiobooks

One folder per author, then either a file per book or a folder per book. A series gets a folder of its own, with each book's number at the start of its folder:

```
Audiobooks/
└── Mara Ellison/
    ├── author.jpg
    └── The Lantern Coast/
        ├── Book 1 - The Tide Keeper/
        │   ├── cover.jpg
        │   └── The Tide Keeper.m4b
        └── Book 2 - Salt and Signal/
            ├── cover.jpg
            ├── 01 - The Tower.mp3
            └── 02 - Static.mp3
```

An `.m4b` (or any single file) keeps its chapters inside; a folder of files is a chapter per file, in track-number order. Discs can go in `CD 1`, `CD 2` folders inside the book's folder. Tags in the files win over the folder names: see [Audiobooks](/docs/watching/audiobooks/#series-and-narrators) for which ones.

<!-- /since -->
## Artwork files

Images next to your media always win over anything looked up online:

| File | Where | Used for |
| --- | --- | --- |
| `poster.jpg` | movie, show or season folder | the poster |
| `fanart.jpg` | movie or show folder | the backdrop behind the title |
| `cover.jpg` or `folder.jpg` | album folder | the album cover |
| `artist.jpg` | artist folder | the artist's photo |
| `cover.jpg` or `folder.jpg` | audiobook folder | the book's cover |
| `author.jpg` | author folder | the author's photo |

<!-- since: v0.1.3 -->

Cover names for albums and books can be in any case and can end in `.jpg`, `.jpeg`, `.png` or `.webp`. `front.jpg`, an image with "cover" or "front" in its name, or the only image in the folder work too. An album's cover can sit above its `CD 1` and `CD 2` folders. See [Music](/docs/watching/music/#covers-and-artist-photos) and [Audiobooks](/docs/watching/audiobooks/#covers-and-author-photos).

<!-- /since -->

## Subtitles

Put subtitle files next to the video, named like the video plus a language code:

- `Movie (2024).en.srt`: English
- `Movie (2024).en.forced.srt`: forced English, for foreign-language parts only
- `.srt`, `.vtt`, `.ass` and `.ssa` all work

<!-- since: v0.3.0 -->
Picture subtitles ripped from discs work as files too: a Blu-ray `.sup` file, or a DVD `.idx` with its `.sub` beside it. Name them the same way (`Movie (2024).en.sup`). Like picture subtitles inside a video, they're drawn into the video while it plays.

<!-- since: v0.3.0 -->
Subtitles can also live in a `Subs` (or `Subtitles`) folder next to the video, the way many releases come:

```
Movie (2024)/
├── Movie (2024).mkv
└── Subs/
    ├── Movie (2024).en.srt
    └── 2_English.srt
```

For a TV season, give each episode its own folder inside `Subs`, named like the episode file (`Subs/Show S01E01/2_English.srt`). Files loose in `Subs` are only matched when the folder above holds a single video, so one episode never gets another's subtitles. A name like `2_English.srt` is read as English.

Subtitles inside the video file work too. Text subtitles are drawn by the player; picture subtitles from Blu-rays and DVDs are burned into the video while it plays. See [Playing](/docs/watching/playing/).

## Trailers

A trailer next to a movie or show shows up as a **Trailer** button on its page. Name it like the video with `-trailer` on the end (`Movie (2024)-trailer.mp4`), call it `trailer.mp4`, or put trailers in a `Trailers` folder. Trailer files are never listed as movies of their own.

## Extras that confuse the scan

Matinee skips hidden files and folders (starting with `.`). Keep samples and other stray videos out of library folders, or they'll show up as titles of their own.
