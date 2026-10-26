# Auto-play

When an episode's closing credits start, every Matinee player shows an
**Up next** card for the next episode with a 10-second countdown, then plays
it. This page is the shared behaviour for the web player and the apps.

## When the card shows

1. Where the closing credits start, from `mediaInfo.creditsMs` on the item.
   The background analysis reads it from the file's chapters: the first
   chapter in the second half of the file titled like *Credits*,
   *End Credits*, *Outro*, *End Titles*, or *ED*/*Ending* (anime).
   Files analyzed before this existed are analyzed again by themselves.
2. Otherwise 30 seconds before the end.
3. Videos under two minutes only get it at the end.

Seeking back to before that point takes the card away again.

## What it offers

`GET /api/v1/items/{id}/next` answers with:

- `next`: the next episode in the season, else the first episode of the
  following season. Specials (season 0) lead only to other specials, and
  regular seasons never lead into specials.
- `similar`: after a show's last episode, a related show the person hasn't
  finished (else any related title). Players offer to open its page; it
  never plays by itself.

## The card

- **Countdown:** "Up next", the episode's still, show, `S1 · E2 · Title`,
  "Playing in N". **Play now** starts it at once. **Cancel** leaves for the
  episode's details page. The countdown pauses while the video is paused.
  If the episode ends first, the next one starts straight away.
- **Still watching?** After 3 episodes in a row played by the countdown with
  nobody pressing, clicking or tapping anything, the card asks instead of
  counting down: **Keep watching** or **Stop** (back to the details page).
  The video keeps going to the end and then waits.
- **You might like:** the `similar` title with **Have a look** and **Close**.

Moving on marks the episode watched. Reaching `creditsMs` also counts as
watched on the server, like passing 92% does.

## Settings

`GET`/`PUT /api/v1/me/playback` holds each person's choices, on every device:

- `autoPlay` (default on): offer the next episode with the countdown. Off
  means no card; the player ends as it always did.
- `binge` (default off): skip the countdown and go straight into the next
  episode when the credits start. "Still watching?" still applies.
