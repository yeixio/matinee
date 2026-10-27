---
title: Organizing your media
description: How to name movie, TV and music folders and files so Matinee recognizes them, plus artwork, subtitles and trailer files.
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

## Artwork files

Images next to your media always win over anything looked up online:

| File | Where | Used for |
| --- | --- | --- |
| `poster.jpg` | movie, show or season folder | the poster |
| `fanart.jpg` | movie or show folder | the backdrop behind the title |
| `cover.jpg` or `folder.jpg` | album folder | the album cover |
| `artist.jpg` | artist folder | the artist's photo |

## Subtitles

Put subtitle files next to the video, named like the video plus a language code:

- `Movie (2024).en.srt`: English
- `Movie (2024).en.forced.srt`: forced English, for foreign-language parts only
- `.srt`, `.vtt` and `.ass` all work

Subtitles inside the video file work too. Text subtitles are drawn by the player; picture subtitles from Blu-rays and DVDs are burned into the video while it plays. See [Playing](/docs/watching/playing/).

## Trailers

A trailer next to a movie or show shows up as a **Trailer** button on its page. Name it like the video with `-trailer` on the end (`Movie (2024)-trailer.mp4`), call it `trailer.mp4`, or put trailers in a `Trailers` folder. Trailer files are never listed as movies of their own.

## Extras that confuse the scan

Matinee skips hidden files and folders (starting with `.`). Keep samples and other stray videos out of library folders, or they'll show up as titles of their own.
