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
- **Posters and descriptions** from [TMDB](https://www.themoviedb.org/) with a free API key ([how to get one](#posters-and-descriptions-tmdb-key)). Your own artwork files always win, and everything else works without a key.
- **Search** as you type.
- **People:** an admin plus as many accounts as you like, each with their own history.
- **Devices:** sign in a phone or TV by typing the code it shows at `/link`, and see or sign out every device you're signed in on under Settings.

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

### Posters and descriptions (TMDB key)

Matinee looks up posters, backdrops and descriptions on [TMDB](https://www.themoviedb.org/). For now, each server uses its own free TMDB key. It takes about two minutes:

1. Sign up at [themoviedb.org](https://www.themoviedb.org/signup) and confirm your email.
2. Open [Settings → API](https://www.themoviedb.org/settings/api), click **Create**, and answer **Yes** to "personal use". Your own server is personal use.
3. Fill in the short form. For example, use "Matinee" as the name and "Self-hosted media server for my household" as the summary.
4. Copy the **API Key** (or the API Read Access Token; either works).
5. Give it to Matinee:
   - Docker: add `-e MATINEE_TMDB_KEY=your-key` to `docker run`.
   - Kubernetes: run `kubectl create secret generic matinee-tmdb --from-literal=tmdb-api-key=your-key`, then set `tmdb.existingSecret: matinee-tmdb` in your values.

Restart Matinee. The next library scan fills in the artwork.

**Why isn't a key built in?** TMDB's free keys are for personal use, and a key shipped inside Matinee would be shared by every server. Once donations or client app sales bring in enough, we'll license a TMDB key for the project and build it into the official images, so new servers get artwork with no setup. Until then, you use your own key.

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
| `MATINEE_TMDB_KEY` | empty | TMDB API key or read access token, for posters and descriptions ([get one](#posters-and-descriptions-tmdb-key)) |
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

The API is plain JSON over HTTP under `/api/v1`, open to any client, and documented in [`openapi.yaml`](internal/api/openapi.yaml) (also served at `/api/v1/openapi.yaml`). Within v1 it only grows; breaking changes would go in a v2 served alongside it. Kubernetes probes are at `/healthz` (process up) and `/readyz` (database reachable).

## Development

```sh
make test                      # Go tests (SQLite) and web type checks
MATINEE_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/postgres?sslmode=disable make test-postgres
make dev                       # web player with hot reload, against a server from `make run`
```

The Go tests build real media files with FFmpeg and run a full scan, browse, play and transcode cycle, so FFmpeg needs to be installed.

## Releasing

Push a version tag, such as `git tag v0.2.0 && git push origin v0.2.0`. The release workflow tests, then publishes `ghcr.io/yeixio/matinee:0.2.0` (and `0.2`, `latest`) for amd64, arm64 and armv7, pushes the Helm chart to `oci://ghcr.io/yeixio/charts/matinee` with that version, and attaches single-binary archives for Linux, macOS and Windows to a GitHub release. A tag with a suffix (`v0.2.0-beta.1`) is a pre-release and leaves `latest` alone.

## License

The Matinee server and web player are licensed under the [GNU AGPL-3.0-or-later](LICENSE).
