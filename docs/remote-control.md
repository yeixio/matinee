# Remote control

Every Matinee player tells the server what it's playing and takes commands,
so Home Assistant (see the guide's Home Assistant page) and, later, people's
other devices can see what's on and control it. This page is the shared
behaviour for the web player (`web/src/remote.ts`) and the apps. The API is
`/clients` in `internal/api/openapi.yaml`; the server side is
`internal/live` and `internal/api/live.go`.

## Saying hello

- A player makes up an id once (a UUID) and keeps it, so it stays the same
  player in Home Assistant across restarts. The web player keeps one per
  browser; only one tab talks to the server at a time, and a tab that opens
  the player takes over.
- It sends `PUT /clients/{id}` when it opens, whenever what it plays changes
  (start, play, pause, seek, subtitles, sleep timer, volume) and every 10
  seconds while playing. `playing` is null when nothing plays. The apps send
  their device's name (the web player leaves it empty and the server names
  it after the browser).
- `commands` lists what it can do right now. With nothing open, that's at
  least `playItem`; with the player open, everything it supports. `next`
  only while there's a next episode, `subtitles` only when the item has any.
- When it closes (the app goes to the background for good, the tab closes)
  it sends `DELETE /clients/{id}`.

## Taking commands

- It asks `GET /clients/{id}/commands?wait=25` in a loop. A 404 means the
  server restarted: say hello again, then carry on. On errors, wait a little
  longer each time (up to 30 seconds).
- Commands run as if the person did them on the device:
  - `play`, `pause`, `seek` (`positionMs`), `stop` (close the player, which
    saves the position as usual), `previous` (back to the start), `next`
    (the next episode, as auto-play would).
  - `playItem` opens `itemId` and plays it, from `positionMs` when given,
    else where the person left off. The server already turned a show or
    season into the right episode.
  - `volume` (0 to 1) and `mute` where the player has its own volume. TV
    apps usually don't, and leave them out of `commands`.
  - `sleep` sets the sleep timer (see sleep-timer.md): `minutes` from now,
    `endOfItem`, or `minutes: 0` to turn it off.
  - `subtitles` shows `track`, or none for "".
- When a command has `from`, show "Controlled from {name}" for a few
  seconds, so nobody wonders why the film paused by itself.
- After a command, report the new state right away.

## Who can do what

The home guest (devices at home that didn't sign in) can report and take
commands but not send them. Grown-ups who signed in can see and control
their own account's players. Admins see and control everything, and only
they can follow `GET /events`, which is what Home Assistant signs in as.
