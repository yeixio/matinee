# Voice

"Hey Siri, play Abbey Road on Matinee", and the same through Google
Assistant or Android Auto. The phone apps hear the request and play it in
their audio player (matinee-apps `mobile/modules/matinee-car`): Siri.swift on
iPhones, Voice.kt and VoiceActivity.kt on Android. The guide page is
`docs/guide/apps/voice.md`.

## Finding what to play

`GET /search/play` picks the one thing to play (`internal/api/voice.go`):

- `q` is what was said, or the title when the phone split it up. The server
  reads "the album …", "the song …", "songs by …", "… audiobook" and
  "… by someone" out of it.
- `kind` (artist, album, track, book), `artist` and `album` are what the
  phone worked out (Siri's `INMediaSearch`, Assistant's `EXTRA_MEDIA_*`).
- Only music and audiobooks count. It keeps what's of the kind asked for and
  by whoever was named, then sorts by how close the title is (exact, starts
  with, has it, some words), then artist, album, book, song, then the
  shortest title. "… by …" is tried split and whole, for "Stand by Me".
- 404 when nothing fits.

Playlists are found by the apps through `GET /playlists?q=`: when the
request says "playlist", or when `/search/play` finds nothing. A server from
before `/search/play` gets the first music or book result of `/search`.

## Playing it

- **Siri** (iOS): the app handles `INPlayMediaIntent` itself, with no
  extension; `plugins/withSiri.js` adds the entitlement, the Info.plist keys
  and `application(_:handlerFor:)` in AppDelegate. Siri resolves the words
  first (the server's match becomes an `INMediaItem` with an id like
  `album/12`), then asks the app to play it, in the background.
- **Google Assistant** (Android): on the phone, Assistant sends
  `MEDIA_PLAY_FROM_SEARCH` to VoiceActivity, which passes it to AudioService
  as a media item with no id and the request; in the car, Android Auto calls
  the session's play-from-search the same way. `Library.planVoice` does the
  rest.
- With the app's player open (and on screen), a song, album, artist or book
  plays there, as a pick in the car does; playlists and "play Matinee" play
  in the native player.
- "Play Matinee" with nothing named resumes what played last, else shuffles
  the music.
