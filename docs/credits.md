# Skip credits

When an episode's or movie's end credits start, players show the
[Up next](autoplay.md) card there, or, when nothing comes next, a
**Skip credits** button. This page is the shared behaviour for the server,
the web player and the apps. The user guide is
[docs/guide/watching/skipping-credits.md](guide/watching/skipping-credits.md).

## Where the credits are

`GET /api/v1/items/{id}` gives episodes and movies `endCredits` once known
(`credits` is the cast and crew):

```json
"endCredits": { "startMs": 2541300, "source": "audio" }
"endCredits": { "startMs": 6120000, "endMs": 6480000, "source": "video" }
```

`endMs` is set when something plays after the credits (a scene, a
bumper); without it they run to the end. The server finds them in one of
three ways, the first that knows winning:

1. **Chapters** (`source: chapters`). The media analysis reads a chapter
   titled like *Credits*, *End Credits*, *Outro*, *End Titles* or
   *ED*/*Ending* in the second half of the file (`mediaInfo.creditsMs`).
2. **Audio** (`source: audio`, internal/credits). Episodes of a season
   usually end on the same music. The detector fingerprints the last 30%
   of each episode (3 to 10 minutes) with the intro detector's fingerprint
   (see [intros.md](intros.md)), lines two episodes up at every offset, and
   takes the longest stretch of at least 15 seconds they share as the
   credits in both. It must reach close enough to the end: what follows it
   is at most two minutes, or as long as the shared stretch. Each episode is
   compared with its nearest episodes, up to four.
3. **Video** (`source: video`). Movies, and episodes that share nothing,
   are looked at instead. FFmpeg's `blackframe` filter says, for the key
   frames of the last 20% of a movie (4 to 15 minutes) or 30% of an
   episode, what share of pixels is darker than 32 of 255; a picture with
   90% or more is dark. Files whose key frames are more than 12 seconds
   apart are decoded in full at one picture a second instead. Dark
   pictures make dark stretches, bridging lighter gaps of up to 8 seconds
   (a logo). The last stretch of 30 seconds or more is the credits, if what
   follows it is at most two minutes or as long as the stretch; dark
   stretches of a minute or more up to 90 seconds before it join it (a
   scene halfway through the credits).

Credits that stop within 20 seconds (video) or 3 seconds (audio) of the
end run to it.

Results go in the `end_credits` table (migration 78), one row per video,
start 0 when none were found, keyed by the file's size and mtime and
`credits.Version`. Detection runs after the intro detector, a season or a
movie at a time, at the analysis' pace and the lowest priority, and gives
way to newly scanned files. Admins turn it off with
`PUT /api/v1/analysis {"credits": false}` (the setting
`creditsDetection`); `GET /api/v1/analysis` reports `credits: {on, total,
done, found}` and the state `credits` while it works, with the season or
movie in `current`. **Analyze again** on a video clears its credits too.

For apps from before `endCredits`, the item's `mediaInfo.creditsMs` is
filled in from what the detector found when the credits run to the end.
Reaching `endCredits.startMs` marks the video watched.

## The players

- **Up next** shows at `startMs` when the credits run to the end, else at
  the end (see [autoplay.md](autoplay.md)).
- **Skip credits** shows from `startMs` until 2 seconds before where
  skipping lands, whenever no Up next card is showing: movies, a show's
  last episode with nothing similar, auto-play turned off, or a closed
  card. It never shows with Skip intro. Pressing it seeks to `endMs` when
  something plays after the credits, else ends the video the way reaching
  the end does.
- It sits where Skip intro does. On TVs it takes focus when it shows, so
  select skips.

Rules live in web/src/player/credits.ts and matinee-apps
mobile/src/credits.ts.
