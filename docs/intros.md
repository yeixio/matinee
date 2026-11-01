# Skip intro

While a TV episode's intro plays, every Matinee player offers **Skip intro**,
which jumps to where the intro ends. This page is the shared behaviour for
the server, the web player and the apps. The user guide is
[docs/guide/watching/skipping-intros.md](guide/watching/skipping-intros.md).

## Where the intro is

`GET /api/v1/items/{id}` gives episodes an `intro` once one is known:

```json
"intro": { "startMs": 61200, "endMs": 104800, "source": "audio" }
```

The server finds it in one of two ways, the first that knows winning:

1. **Chapters** (`source: chapters`). The media analysis (internal/probe)
   reads a chapter titled like *Intro*, *Opening*, *Opening Credits*,
   *Title Sequence* or *OP* that starts in the first 40% of the file and
   lasts 5 seconds to 5 minutes. It is also in `mediaInfo.introStartMs` and
   `introEndMs`. MediaInfoVersion 3 added this, so libraries are analyzed
   again by themselves.
2. **Audio** (`source: audio`, internal/intro). Episodes of a season share
   their intro's sound. Once every file is analyzed, the analyzer goes
   season by season:
   - FFmpeg decodes the start of each episode to mono 5.5 kHz: 30% of the
     episode, at least 3 and at most 10 minutes.
   - That becomes an audio fingerprint, one 32-bit value every 46 ms in the
     style of Haitsma and Kalker: each bit says whether the energy step
     between two neighbouring frequency bands (300–2000 Hz) grew or shrank
     since the last frame. Quiet frames are marked, since silence matches
     silence everywhere.
   - Two episodes are lined up at every offset. The bit differences are
     averaged over two-second windows, and the longest run of windows that
     differ in at most 12 of 32 bits on average is what they share. The
     same sound through two codecs differs in about 7 bits, unrelated sound
     in about 16.
   - A shared run of 15 seconds to 200 seconds is the intro, in both
     episodes. Each episode is compared with its nearest episodes, up to
     four, until one shares an intro with it.
   - Episodes whose chapters already say where the intro is aren't
     listened to (only used to compare others with), and their row takes
     the chapter's times, so the counts below include them.
   - Results go in the `intros` table (migration 71), one row per episode,
     zeros when none was found, keyed by the file's size and mtime and
     `intro.Version`, so a replaced file is looked at again. An episode
     with no partner yet (the only one in its season) is looked at again
     when a new episode arrives.

Detection runs at the analysis' pace (Gentle pauses between seasons and
while anyone watches something transcoded) at the lowest CPU and disk
priority. Admins turn it off with `PUT /api/v1/analysis {"intros": false}`
(the setting `introDetection`); `GET /api/v1/analysis` reports `intros:
{on, total, done, found}` and the state `intros` while it works, with the
show and season in `current`. **Analyze again** on an episode
(`POST /items/{id}/analyze`) clears its intro too.

## The button

- It shows from `startMs` until 2 seconds before `endMs`, and pressing it
  seeks to `endMs`.
- It sits where the Up next card does (the two never meet) and moves up
  with the player's controls.
- On TVs it takes focus when it shows, so select skips. Pressing anything
  else, or the intro ending, gives focus back.

## Skipping by itself

`skipIntros` in `GET/PUT /api/v1/me/playback` (off by default) has players
seek past the intro without a press, once per episode, and show "Skipped
the intro" for a moment. Seeking back into the intro after that plays it.
`PUT /me/playback` keeps choices the body leaves out, so apps that predate
`skipIntros` don't turn it off.

Rules live in web/src/player/intro.ts and matinee-apps mobile/src/intro.ts.
