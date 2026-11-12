---
title: Settings reference
description: Every server setting you can give Matinee as an environment variable or a flag.
sidebar:
  order: 12
---

<!-- since: v0.1.9 -->
## Settings in the web player

**Settings** is split into groups. On a computer they're listed down the left side; on a phone, **Settings** in the tab bar opens the list and each group is its own page. Everyone sees **You**; admins see the rest.

| Group | What's in it |
| --- | --- |
| **You** | Language, Appearance, Playback, Your account and Your devices |
| **Libraries** | Libraries, Import from another media server, New files and Media analysis |
| **People and profiles** | People and Profiles |
| **Content** | Posters and details, Podcast search, Trailers and Home (holiday rows) |
| **Streaming** | Transcoding and streaming quality |
| **Network** | Home network and Remote access |
| **Add-ons** | Radarr and Sonarr, Overseerr, and Plugins |

The server's version is at the bottom of every group.

<!-- since: v0.1.11 -->
Admins also have **Server health**, a dashboard of the server's load, what's playing, background jobs, disk space, warnings and errors, and its log. See [Server health](/docs/server/server-health/).
<!-- /since -->

<!-- since: v0.1.12 -->
**Server health** also has **Alerts**, which email you or post to Discord, ntfy or Home Assistant when something needs you, and turns on Prometheus metrics. See [Alerts and metrics](/docs/server/alerts/).
<!-- /since -->

<!-- since: v0.2.4 -->
**Server health** also has **Time zone**. See [Time zone](#time-zone) below.
<!-- /since -->

![Settings on a computer, with the groups down the left side and Libraries open](../images/settings-groups.webp)

![Settings on a phone, listing the groups (phone)](../images/phone-settings.webp)
<!-- /since -->

## Server settings

Most of Matinee is set up in the web player under **Settings**. A few things about the server itself are set when it starts, as environment variables or flags:

| Install | Where they go |
| --- | --- |
| Docker | `-e NAME=value` on `docker run`, or `environment:` in Compose |
| apt, dnf | `/etc/matinee/matinee.env`, then `sudo systemctl restart matinee` |
| Homebrew | `$(brew --prefix)/etc/matinee/matinee.env`, then `brew services restart matinee` |
| Mac app | **Settings → Edit advanced settings** in its menu (`~/Library/Application Support/Matinee Server/matinee.env`), then **Restart server** |
| Helm | `env:` in your values |

| Variable | Default | What it does |
| --- | --- | --- |
| `MATINEE_ADDR` | `:7171` | Listen address |
| `MATINEE_DATA_DIR` | `./data` | SQLite database (when used), image cache, transcode scratch space |
<!-- since: v0.2.4 -->
| `MATINEE_CACHE_LIMIT` | none | How much the data folder's volume may hold when that's less than its disk, such as `50Gi` or `45G`: a Kubernetes `emptyDir`'s `sizeLimit` or a quota. Transcodes keep within it ([more](/docs/server/transcoding/#when-the-transcode-folder-runs-short-of-room)). The Helm chart sets it |
<!-- /since -->
| `MATINEE_DATABASE_URL` | empty | `postgres://…` to use PostgreSQL instead of SQLite |
| `MATINEE_TMDB_KEY` | empty | TMDB API key or read access token, for [posters and details](/docs/getting-started/posters-and-details/) |
| `MATINEE_RATING_COUNTRY` | `US` | Whose age ratings [kids profiles](/docs/household/profiles/) use, such as `GB`, `CA`, `AU` or `DE` |
| `MATINEE_MEDIA_ROOT` | `/` | Folder the library picker opens in |
| `MATINEE_MUSICBRAINZ` | `true` | Look up artists and albums on MusicBrainz, with covers and artist photos. Set `false` to use only local artwork |
<!-- since: v0.1.3 -->
| `MATINEE_OPENLIBRARY` | `true` | Look up audiobook covers and author photos on Open Library. Set `false` to use only local artwork |
<!-- /since -->
<!-- since: v0.1.7 -->
| `MATINEE_PODCASTS_HOME_NETWORK` | `false` | Let [podcast](/docs/watching/podcasts/) feeds and episodes, and [radio stations](/docs/watching/music/#radio-stations), come from addresses on the home network too, for shows and streams you host yourself |
<!-- /since -->
| `MATINEE_WIKIDATA` | `true` | Look up awards on Wikidata. Set `false` to skip awards |
<!-- until: v0.2.4 -->
| `MATINEE_SCAN_INTERVAL` | `15m` | How often libraries are rescanned in full; `0` turns it off ([more](/docs/server/new-files/)) |
<!-- /until -->
<!-- since: v0.2.4 -->
| `MATINEE_SCAN_INTERVAL` | `6h` | How often libraries are rescanned in full until an admin picks an interval in Settings; `0` turns it off ([more](/docs/server/new-files/#full-rescans)) |
<!-- /since -->
<!-- until: v0.2.4 -->
| `MATINEE_WATCH_POLL_INTERVAL` | `30s` | How often folders on network shares are checked for new files |
<!-- /until -->
<!-- since: v0.2.4 -->
| `MATINEE_WATCH_POLL_INTERVAL` | `2m` | How often folders on network shares are checked for new files until an admin picks in Settings ([more](/docs/server/new-files/)) |
<!-- /since -->
| `MATINEE_DISCOVERY` | `true` | Announce the server on the local network so the apps find it |
| `MATINEE_SERVER_NAME` | `Matinee` | The name the apps show for this server |
| `MATINEE_PUBLIC_URL` | empty | The address the apps should use, such as `https://matinee.example.com` |
| `MATINEE_HOME_ADDR` | empty | A second listen address just for the [home network](/docs/server/home-network/), such as `:7172` |
| `MATINEE_GAMES_PORT` | `7173` | The UDP port [games](/docs/server/games/) stream to players through; `0` for any |
| `MATINEE_GAMES_PUBLIC_IP` | empty | The addresses players reach the games port at, comma separated, when the server can't see its own (behind NAT or in a container). The Helm chart sets it |
| `MATINEE_GAMES_DIR` | `/usr/lib/matinee/games` | Where the game runner and its emulators are; `off` turns games off |
| `MATINEE_REMOTE_ADDR` | `:7443` | HTTPS address for [remote access](/docs/server/remote-access/); `off` to not offer it |
| `MATINEE_CONNECT_URL` | `https://connect.matinee.yeix.io` | The lookup service that helps the apps find the server after its address changes. It only learns the address, never carries media; `off` to not use it |
| `MATINEE_PLUGINS` | `true` | Let admins install [plugins](/docs/server/plugins/); `false` turns plugins off |
| `MATINEE_PLUGIN_CATALOG` | `https://matinee.yeix.io/plugins.json` | The list of official plugins **Add plugin** offers; `off` for none |
<!-- since: v0.1.4 -->
| `MATINEE_DRAIN_DELAY` | `0s` | On shutdown, how long to keep serving after telling players the server is restarting, so a load balancer can move to another copy ([more](/docs/getting-started/updating/#updating-while-people-are-watching)) |
| `MATINEE_SHUTDOWN_TIMEOUT` | `10s` | On shutdown, how long requests in flight get to finish |
<!-- /since -->
| `MATINEE_FFMPEG`, `MATINEE_FFPROBE` | `ffmpeg`, `ffprobe` | Paths to FFmpeg |
| `MATINEE_VAAPI_DEVICE` | empty | The GPU to use for VAAPI on a machine with several, such as `/dev/dri/renderD129` |

Each variable also works as a flag: `MATINEE_SCAN_INTERVAL=1h` is `--scan-interval=1h`.

<!-- since: v0.2.4 -->
## Time zone

The server's clock goes by its time zone: the times in its log, when Home's [holiday rows](/docs/watching/home/) start, and anything else that goes by the time of day. Matinee starts in the computer's own time zone, or the one in the `TZ` environment variable (such as `TZ=America/Juneau`), which a container or Kubernetes install usually needs since it has none of its own.

To pick another, open **Settings → Server health → Time zone** and choose one from the list. The first choice, **This computer's**, goes back to the computer's or `TZ`'s. Matinee switches the next time it starts, and says so until then: restart it<!-- since: v0.2.4 --> with **Restart server** at the top of [Server health](/docs/server/server-health/#restart-the-server)<!-- /since -->, or wait for its next update.

![The Time zone setting](../images/time-zone.webp)
<!-- /since -->

## SQLite or PostgreSQL?

SQLite is the default and needs nothing else. It's right for a single machine or a NAS, as long as the data folder is on a local disk. Use PostgreSQL on Kubernetes, or whenever the data would otherwise live on network storage, where SQLite's file locking isn't reliable.
