# Matinee

A fast, self-hosted media server for your movies, TV and music. The basics Plex got right, without the fluff: no ads, no rentals, no discover feed, no account on someone else's server.

![Home](docs/screenshots/desktop-home.png)

<p>
  <img src="docs/screenshots/phone-home.png" width="195" alt="Home on a phone">
  <img src="docs/screenshots/phone-season.png" width="195" alt="A season on a phone">
  <img src="docs/screenshots/desktop-album-playing.png" width="560" alt="An album playing">
</p>

**Status:** early. The server and web player work end to end; native apps come later.

## What it does

- **Libraries** for movies, TV and music. Point it at folders; it names things from the file and folder names (`Movie (2021)`, `Show/Season 1/Show - S01E02.mkv`, `Artist/Album/01 Track.flac`), reads music tags, and picks up `poster.jpg`, `fanart.jpg` and `cover.jpg` next to your files.
- **Plays anywhere.** Files the browser can play stream as-is, with instant seeking. Anything else is converted on the fly with FFmpeg to HLS. H.264 video is repackaged, not re-encoded, so it costs almost no CPU.
- **Subtitles** from the file itself or from files next to it (`Movie.en.srt`, `Movie.en.forced.srt`, `.vtt`, `.ass`). Text subtitles are drawn by the player; Blu-ray and DVD picture subtitles are burned into the video while converting. The player remembers your language from one episode to the next.
- **Picks up where you left off.** Resume points, watched marks, Continue Watching and Recently Added, per person.
- **Music** with a player that keeps going while you browse.
- **Posters and descriptions** from [TMDB](https://www.themoviedb.org/) built in to the official images, no account needed. Your own artwork files always win.
- **Search** as you type.
- **People:** an admin plus as many accounts as you like, each with their own history.

## Run it

### Docker

```sh
docker run -d --name matinee -p 7171:7171 \
  -v ./data:/data \
  -v /path/to/movies:/media/movies:ro \
  -v /path/to/tv:/media/tv:ro \
  -v /path/to/music:/media/music:ro \
  ghcr.io/yeixio/matinee:latest
```

Open http://localhost:7171, create the admin account, and add your folders under Settings. A [`docker-compose.yml`](docker-compose.yml) is included too.

### Kubernetes

```sh
helm install matinee deploy/helm/matinee -f my-values.yaml
```

The chart runs Matinee with a bundled PostgreSQL, so nothing depends on file locking over network storage. Its credentials are generated into a Secret on first install, so there's nothing to set up, and this works the same under Argo CD or Flux. Mount your media read-only through `media:` in the values (NFS, a PVC, anything); transcodes and the image cache use a local `emptyDir`. To use your own PostgreSQL, set `postgresql.enabled=false` and `database.url` (or `database.existingSecret`). See [`values.yaml`](deploy/helm/matinee/values.yaml).

Run one replica for now: an active transcode lives in the pod that started it. More replicas work with `service.sessionAffinity: ClientIP`.

### From source

Needs Go 1.26+, Node 22 with pnpm, and FFmpeg.

```sh
make run          # builds the web player and server, then serves on :7171
make demo         # optional: a small fake library in ./demo-media to try it with
```

## Configuration

Every setting is a flag or an environment variable.

| Variable | Default | What it does |
| --- | --- | --- |
| `MATINEE_ADDR` | `:7171` | Listen address |
| `MATINEE_DATA_DIR` | `./data` | SQLite database (when used), image cache, transcode scratch space |
| `MATINEE_DATABASE_URL` | empty | `postgres://…` to use PostgreSQL instead of SQLite |
| `MATINEE_TMDB_KEY` | built-in key in official images | Your own TMDB API key or read access token, to use instead of the built-in one |
| `MATINEE_MEDIA_ROOT` | `/` | Folder the library picker opens in |
| `MATINEE_SCAN_INTERVAL` | `15m` | How often libraries are rescanned; `0` turns it off |
| `MATINEE_FFMPEG`, `MATINEE_FFPROBE` | `ffmpeg`, `ffprobe` | Paths to FFmpeg |

**SQLite or PostgreSQL?** SQLite is the default and needs nothing else: right for a single machine or a NAS, as long as `MATINEE_DATA_DIR` is on a local disk. Use PostgreSQL on Kubernetes, with several servers, or whenever the data would otherwise live on network storage, where SQLite's locking isn't reliable.

## How it's built

One Go binary with the web player embedded. It talks to SQLite or PostgreSQL and runs FFmpeg for probing and transcoding.

```
cmd/matinee          entry point
internal/api         HTTP API, auth, serves the web player
internal/scan        library scanning; library kinds (movies, tv, music) register here
internal/metadata    online metadata providers (TMDB)
internal/artwork     poster and backdrop serving and caching
internal/stream      direct play and HLS transcoding
internal/store       SQLite / PostgreSQL storage
web/                 React + Vite web player
deploy/helm/matinee  Helm chart
```

Library kinds and metadata providers are interfaces so plugins can add more later, such as an arcade (MAME) library.

The API is plain JSON over HTTP and open to any client. Kubernetes probes are at `/healthz` (process up) and `/readyz` (database reachable).

## Development

```sh
make test                      # Go tests (SQLite) and web type checks
MATINEE_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/postgres?sslmode=disable make test-postgres
make dev                       # web player with hot reload, against a server from `make run`
```

The Go tests build real media files with FFmpeg and run a full scan, browse, play and transcode cycle, so FFmpeg needs to be installed.

## License

The Matinee server and web player are licensed under the [GNU AGPL-3.0-or-later](LICENSE).
