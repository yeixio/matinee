# Watch together

Several players follow one shared session, so friends and family on other
devices watch the same moment. The user guide's [watching together](guide/watching/watch-together.md)
page covers what people see; this is how it works.

## Sessions

`internal/together` keeps sessions in memory: a six character code (from an
alphabet without look-alike letters), the item, where playback was at
`updatedAt` on the server's clock, whether it's paused, the members, and a
version that goes up with every change. Nothing is written to the database.

A session's state is enough to know where playback is now:
`positionMs + (now - updatedAt)` while playing. Every answer carries
`serverTime`, so players work out the gap between their clock and the
server's and don't depend on the device's clock being right.

## API

| Request | What it does |
| --- | --- |
| `POST /together` | Start a session around what this player has open. |
| `GET /together/{code}` | Look before joining; `canWatch` says whether this person sees the item. |
| `POST /together/{code}/join` | Join. 403 when the item isn't in a library this person sees. |
| `POST /together/{code}/actions` | `play`, `pause`, `seek` or `item` (everyone moves to another title). |
| `GET /together/{code}/state?client=&since=&wait=` | Wait for the next change, up to 25 seconds. |
| `DELETE /together/{code}/members/{client}` | Leave. |
| `POST /together/{code}/end` | End it for everyone. |

Players identify themselves with the same client id as remote control
(`docs/remote-control.md`), and a client id only counts for the account that
used it first, so nobody can act for another person's player.

## Players

Each player keeps asking for state with the last version it saw. On a new
state it:

1. moves to the session's item if it's another one,
2. plays or pauses to match,
3. seeks to where the session is when it's more than two seconds off, but
   not while loading and not again within five seconds, so a transcode that
   restarts at each seek can catch up.

What the person does (play, pause, seek, the next episode) goes to
`/actions`, and the player changes its own copy of the state at once so it
doesn't snap back while the server answers. Subtitles, audio, quality and
volume stay per player.

A member who stops asking for 70 seconds is dropped, and an empty session
goes after 15 minutes. When the server shuts down, waiting requests answer
at once; afterwards the session is gone, so the first player back starts it
again with `resume` (the old code) and its own state, and the others join it.
A session someone ended can't be resumed.
