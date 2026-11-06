---
title: Organizing your media
description: How to name movie, TV, music and audiobook folders and files so Matinee recognizes them, plus artwork, subtitles, trailers and extras.
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

<!-- since: v0.2.4 -->
### Movies in several files

A movie split across files, the way rips that fit on CDs come, plays as one movie: its parts one after the other, with one seek bar and one place to resume from. Number the parts at the end of the name:

```
Movies/
└── Harbor Lights (2019)/
    ├── Harbor Lights (2019) CD1.avi
    └── Harbor Lights (2019) CD2.avi
```

`CD1`, `DVD1`, `Disc 1`, `Disk1`, `Part1` and `pt1` all work, with or without a space, a dot or a dash before the number. The parts must be in the same folder and named the same apart from the number. `Part` counts only after the year or in a folder named with the year, so a film called `Harbor Lights Part 2 (2021)` stays a title of its own.

The movie streams through your server, which joins the parts as it plays. Downloading it for offline gives one file, at 1080p. Seek bar previews and the end credits mark aren't made for a movie in parts yet.
<!-- /since -->

<!-- since: v0.2.4 -->
## Pin a match with an id

When a title keeps matching the wrong film or show, put its id in the folder or file name. Matinee then looks it up by the id instead of searching by name:

```
Movies/
└── Harbor Lights (2019) {tmdb-12345}/
    └── Harbor Lights (2019).mkv
TV/
└── Crumb Street (2021) [tvdbid-81189]/
    └── Season 1/
        └── Crumb Street - S01E01.mkv
```

| Tag | Id from |
| --- | --- |
| `{tmdb-12345}` or `[tmdbid-12345]` | The Movie Database |
| `{imdb-tt0111161}` or `[imdbid-tt0111161]` | IMDb |
| `{tvdb-81189}` or `[tvdbid-81189]` | TheTVDB, for shows |

Radarr and Sonarr can add these to folder names for you (the `{TmdbId}`, `{ImdbId}` and `{TvdbId}` naming tokens). Two show folders with different TMDB ids stay two shows even when their names match. The tags never show in titles. A match you fixed by hand on the title's page always wins over a tag.
<!-- /since -->

<!-- since: v0.2.1 -->
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

<!-- since: v0.2.4 -->
### Episodes without S01E01

Inside a season folder, a number is enough. These all work:

```
TV/
└── Crumb Street/
    ├── Season 2/
    │   ├── 04 - Dough Rises.mkv
    │   ├── 05.mkv
    │   └── Episode 6.mkv
    └── Specials/
        └── 01 - Holiday Bake.mkv
```

`Ep 06` and `E06` work too. Season folders can be called `Season 2`, `S02`, or the word for season in your language, such as `Staffel 2`, `Temporada 2` or `Saison 2`. A `Specials` folder is season 0.

### Daily shows

Talk shows and the news are usually named by the day they aired. Matinee files each under a season for its year, in date order, and finds the episode online by its date:

```
TV/
└── The Nightly Desk/
    ├── The Nightly Desk 2024-03-15.mkv
    └── The.Nightly.Desk.2024.03.18.Guest.Name.720p.mkv
```

The date can be written `2024-03-15`, `2024.03.15` or `2024 03 15`. A year in the show's folder name, like `Crumb Street (2021)`, is never read as a date.

### Two episodes in one file

A file that holds two or more episodes names the first and the last: `Crumb Street - S01E01E02.mkv`, `S01E01-E02`, `S01E01-02` or `1x01-1x02` all work. It shows as one item in the season, called "Episodes 1–2" unless the file name gives it a title.

### Shows with the same name

Put the year in the show's folder name, `Crumb Street (1998)` and `Crumb Street (2021)`, and Matinee keeps them as two shows and looks each up as the one from that year. A show folder without a year joins the show it names.
<!-- /since -->

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

<!-- since: v0.2.1 -->
Picture subtitles ripped from discs work as files too: a Blu-ray `.sup` file, or a DVD `.idx` with its `.sub` beside it. Name them the same way (`Movie (2024).en.sup`). Like picture subtitles inside a video, they're drawn into the video while it plays.

<!-- since: v0.2.1 -->
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

<!-- since: v0.2.1 -->
## Extras

Behind-the-scenes videos, deleted scenes, featurettes, interviews and other special features show in the **Extras** row on a movie's or show's page (see [Extras and commentaries](/docs/watching/extras/)). Put them in a folder for their kind inside the movie's or show's folder:

```
Movies/
└── Harbor Lights (2019)/
    ├── Harbor Lights (2019).mkv
    ├── Harbor Lights (2019)-featurette.mkv
    ├── Behind The Scenes/
    │   └── Building the lighthouse.mkv
    ├── Deleted Scenes/
    │   └── The storm.mkv
    └── Interviews/
        └── The director.mkv
```

| Folder | Or end the file name with | Shows as |
| --- | --- | --- |
| `Behind The Scenes` | `-behindthescenes` | Behind the scenes |
| `Deleted Scenes` | `-deleted` | Deleted scene |
| `Featurettes` | `-featurette` | Featurette |
| `Interviews` | `-interview` | Interview |
| `Scenes` | `-scene` | Scene |
| `Shorts` | `-short` | Short |
| `Trailers` | `-trailer` | Trailer |
| `Extras`, `Other` or `Special Features` | `-other` or `-extra` | Extra |

Folder names can be written with spaces, dots or underscores (`Deleted.Scenes`, `deleted_scenes`). A folder can sit inside `Extras` too, such as `Extras/Interviews/`. The file name, without the ending, is the extra's name on screen.

A movie sharing a folder with other movies only gets the files named after it, such as `Harbor Lights (2019)-featurette.mkv`. A show's extras go in the show's folder, and a season's in its season folder. These folders count only inside a movie's or show's folder: a `Shorts` folder right in your library folder is still a group of short films. Extras are never listed as titles of their own, or as versions of a movie.

Commentary tracks belong inside the movie's file. Mark the track as a commentary, or put "Commentary" in its name, when you make the file (MKVToolNix and most ripping tools do this), and it shows with the extras.
<!-- /since -->

## Extras that confuse the scan

Matinee skips hidden files and folders (starting with `.`). Keep samples and other stray videos out of library folders, or they'll show up as titles of their own.
